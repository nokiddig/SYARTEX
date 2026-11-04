package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter is the contract for the notification_channels.filter JSONB column: the filter conditions of a
// channel instance. All fields are optional and the default means "no filtering", which is exactly the
// fallback semantics for malformed configuration, see ParseFilter.
type Filter struct {
	// MinSeverity is the minimum severity threshold (low/medium/high/critical); empty means no threshold.
	MinSeverity string `json:"min_severity"`
	// An empty TaskIDs / AssetIDs array means no restriction; when non-empty the event must intersect it.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// An empty VulnClassInclude means accept everything; when non-empty the vulnclass must match any one keyword.
	// VulnClassExclude excludes on a match of any keyword (exclude takes precedence over include).
	// Matching is a case-insensitive substring match, which is safer than regex: a mistyped regex
	// cannot silently disable the channel.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange decides whether the channel receives finding status-change events (meaningful only in realtime mode).
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter parses the channel filter configuration.
//
// **It never returns an error.** This is a deliberate design choice: a malformed filter configuration
// always degrades to the zero-value Filter (= no filtering = everything matches), because for a
// vulnerability notification system **pushing one message too many is far better than silently
// missing a high-severity one**. Making a parse failure mean "do not push" would hand the user a channel
// that looks configured but pushes nothing, which is the worst failure mode.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// When parsing fails f keeps its zero value, i.e. no filtering.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity reports whether s is a valid severity threshold (an empty string means no threshold).
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks the fields of the filter configuration whose **values are restricted**, for use when saving a channel.
//
// Why it must be enforced at write time: Match judges an unknown threshold as `rank >= 0`, which is
// always true. That is, a single typo in min_severity ("hgih") makes the filter **silently stop
// working** and turn into "push everything". That is consistent with this package's trade-off of
// "better to push too much than to miss one" (nothing is missed), but the result is that the user
// thinks they are doing tiered pushing while actually flooding the group with every finding, and
// nothing tells them the configuration is wrong. This kind of "silent degradation" is exactly what
// should be blocked at the entry point.
//
// Note that Validate is used only on the **write** path. The read path still uses ParseFilter's
// lenient semantics, so bad values already present in historical data do not make the channel
// unreadable as a whole.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("invalid minimum severity %q; valid values: low / medium / high / critical, or leave empty for no limit", f.MinSeverity)
	}
	return nil
}

// Match decides whether an event should be delivered to a channel with this filter.
//
// **It never returns an error**, for the same reason as ParseFilter: any internal anomaly is treated as
// a "match".
// Order of evaluation: event type -> severity threshold -> task/asset scope -> vulnerability type keywords.
func Match(f Filter, s Snapshot) bool {
	// Status-change events are received only by channels that explicitly enable them. It is off by default,
	// because most users expect "push" to mean "a new finding was found", not to follow every status
	// transition like a running ledger.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclusion comes first: matching any exclude keyword drops the event, even if it also matches the include list.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// A linear scan of small sets is enough; both sides are on the order of "a few dozen hand-picked
	// items", and building a map costs more than it saves.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold reports whether s contains any of the keywords (case-insensitive).
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
