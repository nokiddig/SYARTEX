package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// Global setting keys (stored in the settings key-value table, no table needed).
const (
	// settingNotifyEnabled is the master switch for notifications. On by default: it is
	// a one-click kill switch for maintenance windows, not a precondition for the
	// feature - the real precondition is whether any channel is configured.
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the external base URL used to build finding-detail
	// backlinks (e.g. https://artex.example.com). When empty, messages carry no
	// backlink button. The project has no reusable external-address setting, so one
	// is added here.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes is the digest-mode interval (minutes).
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick is the delivery engine's polling interval. 3 seconds is the upper
	// bound of this engine's real-time responsiveness, and the main source of delay
	// between a finding being stored and the message reaching the IM.
	notifyTick = 3 * time.Second
	// notifyLease is the lease duration taken when claiming deliveries. It must be
	// significantly longer than the worst-case time of a single delivery (the notify
	// package's HTTP client timeout is 15 seconds); otherwise the same row could be
	// delivered by two dispatchers at once.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick caps the number of events dispatched per tick, so that
	// enabling a channel for the first time does not expand the whole historical
	// backlog into delivery tasks at once.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest interval.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick is the per-tick delivery cap for channels without a
	// rate limit. It exists to keep "one channel configured as unlimited + a scan
	// that finds thousands of findings" from blocking a single loop iteration for a
	// long time.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick is the maximum number of messages delivered per
	// channel per tick.
	//
	// This cap is derived backwards from the **lease duration**: rows are leased when
	// claimed (notifyLease = 3 minutes), and if so many messages are delivered
	// serially in one tick that the worst-case time exceeds the lease, the lease on
	// the last few expires before they are sent. Within a single process this does
	// not matter (Run is a single goroutine running serially, so ticks never
	// re-enter), but when **two processes share one database**, the other process
	// re-claims rows whose lease has expired and sends them again, also
	// double-incrementing attempts and judging them failed while the original
	// process is still delivering.
	//
	// Value: 3 minute lease / 30 second per-send timeout = 6 would use the lease
	// **exactly**, with zero margin, so it cannot be used; 5 leaves a 30 second
	// margin on the 150 second worst case. This relationship is pinned by
	// TestNotifyTickBudgetFitsWithinLease - changing any one of notifyLease,
	// notifySendTimeout or this value makes that assertion fail.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout is the timeout of a single delivery. It also determines the
	// value of the previous constant; their product must not exceed notifyLease, see
	// TestNotifyTickBudgetFitsWithinLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is the retry backoff sequence, indexed by the number of attempts made.
// The 3 chances (including the first) correspond to db.MaxNotifyAttempts; the two
// must be changed together.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Notifier is the delivery engine for finding notifications.
//
// It runs as its own goroutine alongside Scheduler (see server.New). It deliberately
// does not reuse Scheduler's tick: the real-time requirement of notifications
// (3 seconds) differs from the business cadence of triggers, and the failures of
// the two must not affect each other - a stuck notification must not hold up agent
// triggers.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu protects buckets. There are few channels and little contention, so one mutex
	// is enough; a finer-grained structure is not worth introducing.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is the token bucket of a single channel.
//
// A token bucket is used instead of a sliding window that "counts per minute and
// then resets" because the latter has bad boundary effects: sending a full 20
// messages at the end of a window and another 20 right after is 40 messages within
// one second to the platform, which gets rate limited; a token bucket refills at a
// constant rate and naturally avoids such bursts.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until ctx ends. Started once by server.New.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step runs one round: first dispatch new events, then deliver the due tasks.
//
// A failure in any step is only logged and never breaks the loop - a fault in the
// notification system must never escalate into a process-level problem. Every tick
// is independent, so the next round naturally retries.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] failed to dispatch events: %v", err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf("[notify] failed to read channels: %v", err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// The token bucket is metered in **messages** (equivalent to HTTP requests), not
		// in findings. In real-time mode the two are equal (one finding, one message);
		// in digest mode a whole batch of findings is merged into one message, so it
		// consumes only one token.
		//
		// Both modes consult the token bucket first and then claim according to the
		// quota - the order must not be reversed, otherwise a delivery blocked by rate
		// limiting has already used up a retry attempt.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan returns the token cost and the batch size upper bound of a digest
// channel for this tick.
//
// The two return values are **two different units**, which is exactly why this is
// its own function:
//
//   - tokens is a message count. A batch of findings is merged into one message and
//     sent with one HTTP request, so it is always 1. rate_per_min therefore still
//     applies to digest (at most this many digest messages per minute).
//   - claimLimit is the maximum number of findings in this batch. It is bounded only
//     by memory and is unrelated to the request budget.
//
// Previously, to make rate_per_min apply to digest, the per-tick request budget
// (notifyMaxSendsPerChannelPerTick, derived backwards from the lease) was passed
// straight down as the batch size. As a result, a channel with rate_per_min=20 only
// refilled 1 token in a 3-second tick, so every digest message carried just 1
// finding - digest degraded into "real-time push with digest wording", readers
// received a string of "1 new finding in the last 30 minutes", and
// db.MaxDigestBatchSize was never reachable.
//
// This symptom is hard to spot in end-to-end tests (the existing cases all pass a
// large enough limit to stepDigest by hand, bypassing the quota computation in
// step), so the decision is kept here and pinned directly by
// TestDigestTickPlanDecouplesBatchSizeFromSendBudget.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and delivers the real-time tasks of a channel, one message per finding.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim real-time deliveries channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("channel type %q is not registered", ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// A render failure is a local data problem; retrying will not help.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest aggregates the pending deliveries of a channel into one message and sends it when a batch is due.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] failed to check whether a digest batch is due channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] failed to claim digest batch channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("channel type %q is not registered", ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Deliveries whose snapshot is broken and that did not make it into the message
	// must be explicitly failed. Otherwise they stay outside included, in neither the
	// message nor the failure list - when the send succeeds, their state is missed by
	// the later bulk marking and stays in sending until the lease expires, after
	// which they are claimed over and over.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := "event snapshot cannot be parsed; this finding cannot be rendered into a message"
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark bad-snapshot deliveries channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with unparsable snapshots channel=%d", len(skipped), ch.ID)
	}
	// Hand only the deliveries that made it into the message to send: included[i]
	// corresponds strictly to msg.Items[i], and send relies on this correspondence to
	// apply "the channel reported that the first K items fit" to the right delivery rows.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers and advances state according to the result.
//
// The deliveries of one batch (possibly dozens in digest mode) share a single send
// result: either delivered, or the whole batch is retried. There are no per-item
// retries - a digest message is one message, and resending part of it would confuse
// the batch semantics.
//
// The only exception is **splitting caused by the channel length limit**: the
// channel reports that only the first K items actually fit, so item K+1 onwards
// must be left for the next batch instead of being marked successful along with
// the rest. Otherwise the truncated findings are neither in the message nor in the
// failure list, and vanish completely.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Cap a single delivery, so a stuck channel cannot hold up all the remaining channels of this round.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// The delivered count reported by a channel cannot exceed the number of
			// deliveries; if it ever does, the rendering layer miscounted - treat it as
			// all delivered and log the problem, which beats corrupting records.
			log.Printf("[notify] channel reported delivered count %d exceeding delivery count %d channel=%s; treating as all delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] failed to mark deliveries as sent channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// This message reached the channel length limit: requeue the rest right away
			// so the next tick continues sending. Use DeferDeliveries rather than
			// RescheduleDeliveries - this is not a failure and must not consume retry
			// budget (claiming already optimistically did +1, and that call subtracts it back).
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf("message reached the channel length limit; only the first %d items were delivered, the rest are left for the next batch", delivered)); err != nil {
				log.Printf("[notify] failed to queue segmented continuation channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// The channel neither reported an error nor how many items were delivered.
		// Treat it as a failure (with backoff), so the delivery is not claimed again
		// and again yet never marked.
		err = fmt.Errorf("channel did not report the delivered count (delivered=%d)", delivered)
	}

	// The failure disposition is decided **per delivery**, not from the maximum
	// attempt count of the whole batch.
	//
	// It used to be `if maxAttempts(deliveries) >= MaxNotifyAttempts`, declaring the
	// whole batch dead, but the attempt counts within a batch differ: an old delivery
	// that has already been retried twice (attempts=2) would drag brand-new
	// deliveries (attempts=1) of the same batch into failed together - a new finding
	// would be lost permanently without using a single retry, the exact opposite of
	// the intent of "do not let old rows pull new rows down".
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, err.Error()); fErr != nil {
			log.Printf("[notify] failed to mark failed state channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf("still failing after %d attempts: %s", db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] failed to mark failed state channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Reschedule grouped by delay: there are only 3 backoff tiers, so the number of
	// groups is naturally small and there is no need to issue a separate UPDATE per
	// item (that would make a 500-item batch produce 500 round trips).
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, err.Error()); rErr != nil {
			log.Printf("[notify] failed to reschedule deliveries channel=%s ids=%v: %v", channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf("[notify] delivery failed channel=%d kind=%s permanent=%d retries_exhausted=%d pending_retry=%d: %s",
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries returns those in all that are not in keep (compared by pointer identity).
// Used to find the deliveries that "did not make it into the message" - they must be
// explicitly handled and cannot be left in a gray area.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt fetches the channel implementation and parses its config.
// ok=false means the type is not registered; the delivery should fail directly
// instead of being retried forever.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// On a config parse failure, use an empty map: the channel's own Validate will
		// report which field is missing, and that error guides the user's fix better
		// than a JSON parse error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders a single-finding message.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL}, nil
}

// renderBatch renders a digest message. Snapshots are parsed one by one - a single
// broken one is skipped on its own and does not take down the whole digest.
//
// The return value included corresponds **strictly one to one** with msg.Items
// (the i-th delivery <-> the i-th item). This correspondence is a hard requirement:
// the caller decides from "the channel reported that the first K items fit" which of
// the first K deliveries to mark as delivered. If a bad snapshot were skipped here
// without removing the skipped delivery from included, the indexes would be
// misaligned - a bad item that should fail would be marked delivered, and a good
// item wrongly judged as not delivered. The bad ones are explicitly marked failed by
// the caller, see stepDigest.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// A bad snapshot enters neither the message nor included - its disposition is
			// the caller's job (explicitly marked failed, rather than being mixed into
			// "delivered" to slip through).
			log.Printf("[notify] skipping unparsable snapshot in digest batch delivery=%d: %v", dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, fmt.Errorf("all %d deliveries in the digest batch are unparsable", len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders an event snapshot into an item to push, also resolving asset names and the detail backlink.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// A failure to resolve asset names should not block the push: not reading the
		// names is far lighter than not receiving the notification - the message just
		// lacks an asset line.
		log.Printf("[notify] failed to resolve asset names finding=%d: %v", snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// For the detail page route see web/src/app/(main)/function/findings/detail/page.tsx,
		// which reads the finding id from the query parameter id.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens takes **at most want** tokens from the channel's token bucket and
// returns the number actually taken.
//
// One token = one message (one HTTP request). In real-time mode the caller passes as
// many messages as it wants; in digest mode a whole batch of findings sends only one
// message, so it passes 1.
//
// The bucket capacity is the channel's per-minute limit, refilled at a constant rate.
// ratePerMin<=0 means no rate limit and returns a finite but large enough value, so a
// single loop iteration is not held up by an unbounded backlog.
//
// The want cap is required: without it the only option is to drain the whole bucket,
// while the caller has its own per-tick cap, so the extra tokens would be useless and
// would vanish out of thin air before the next refill - the accumulated burst
// capacity would never be reachable, and even "no deliveries pending this round"
// would still be charged.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Refill by the real elapsed time; the rate is ratePerMin/60 per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before truncating: the token count is accumulated in floating
	// point, so when refilling to full in two steps, 0.5 + 0.5 may give 0.9999999999,
	// and int() would truncate it to 0 - a bucket that is mathematically full would
	// yield no token. 1e-9 is far smaller than one token and does not let a real
	// shortfall through.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the master switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL returns the external address used for backlinks, with the trailing slash removed.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval returns the digest interval, falling back to the default when invalid or not configured.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot parses the event snapshot belonging to a delivery.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, fmt.Errorf("event snapshot of delivery %d is empty", dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf("failed to parse event snapshot of delivery %d: %w", dl.ID, err)
	}
	if snap.Kind == "" {
		// The event kind follows the event row; the copy in the snapshot may have been written by an older version.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
