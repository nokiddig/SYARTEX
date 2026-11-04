package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets decides whether messages may be delivered to loopback / link-local addresses.
//
// Denied by default. These address ranges are not where IM robots or public mail servers live, yet what
// they can reach is sensitive: the admin port of another service on the same machine, and the metadata
// endpoint of cloud environments (169.254.169.254, which can expose instance credentials). The delivery
// address is configured by an administrator, but an admin session borrowed through XSS/CSRF, or a second
// person sharing the same JWT, could read response content back by editing the config: doJSON writes the
// first 200 bytes of a 4xx/5xx response body into last_error, and the delivery history API echoes it,
// which makes this a half-blind read primitive.
//
// But a "local SMTP relay" (postfix on 127.0.0.1:25) is a common self-hosted mail setup, and a blanket ban
// would leave people stuck. So an explicit escape hatch is provided instead of hard-coding an exemption:
// setting ARTEX_NOTIFY_ALLOW_LOCAL=1 allows it.
//
// It is exported as AllowLocalTargetsEnv so that tests can switch it on explicitly: the test cases of this
// package and the server package make heavy use of httptest fake receivers on 127.0.0.1, and without
// it they would all be blocked by the guard.
const AllowLocalTargetsEnv = "ARTEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP reports whether the target IP belongs to an address range that is "not allowed for delivery by default".
//
// It rejects only loopback, link-local (including the cloud metadata address 169.254.169.254), unspecified and multicast.
// It **does not reject** RFC1918 private networks: a self-hosted Mattermost / SMTP relay on the internal
// network is a very common legitimate use, and blocking them as well would make the feature simply
// unusable in real environments. This trade-off is deliberate: the protection has to block the truly
// sensitive targets without wrecking normal deployments along with them.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// IPv4-mapped IPv6 (::ffff:127.0.0.1) must be restored to IPv4 before judging, otherwise the check is bypassed.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the Control hook of the http.Transport dialer; it checks the target address
// **at connection time**.
//
// Why it is placed in the dial stage rather than only validating when the config is saved: this is where
// the rule finally takes effect. It covers both ways of bypassing config validation: DNS rebinding
// (resolving to a public IP at validation time and to an internal one at connection time) and
// redirects (although cross-host redirects are already refused, a same-host redirect may still point
// the path elsewhere).
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("cannot parse target address %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("refusing to deliver to a loopback/link-local address %s (to deliver to a local service, set %s=1)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport adds only a dial guard on top of the default Transport.
// Clone keeps all of the default tuning (connection pool, HTTP/2, timeouts, proxy, etc.), to avoid
// changing other behavior just to add one check.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is the client shared by all channel deliveries.
//
// It deliberately does **not** reuse the project's global egress proxy (GlobalProxy on the server side): that proxy is
// for penetration-target traffic and is often an unstable tunnel, and the availability of notifications
// should not be held hostage by jitter on the target network. IM pushes can simply connect directly. The
// timeout is set to 15 seconds; a peer slower than that is effectively already in a failed state.
//
// Cross-host redirects are refused: the delivery URLs of this feature all have the "one fixed endpoint"
// shape and normally never redirect to another host, while the credentials of these platforms (DingTalk
// access_token, WeCom key, Telegram bot token) are **in the URL**, so following a cross-host redirect
// would hand the credentials to the redirect target. Same-host redirects (such as adding a trailing
// slash) are still allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("refusing cross-host redirect (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit limits how much of the response body is read. A misbehaving peer may send back huge
// content, while all we need is the error code and a short error description to show in the delivery history.
const respBodyLimit = 8 << 10

// doJSON sends one request and returns the response body (length-limited).
//
// When payload is nil an empty body is sent (for GET or platforms that do not require a body).
// The keys and values in headers are attached as is, for the custom headers of the generic Webhook.
//
// Error classification is the core responsibility of this function: network-level failures and 5xx/408/429
// are classified as "retryable", and the other 4xx as "permanent"; retrying a 403 only prints the same
// error to the log 3 times.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// A serialization failure is a local bug (a config field has the wrong type) and retrying will not make it better.
			return nil, Permanent(fmt.Errorf("failed to build request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// An invalid URL is most likely a mistyped address from the user and is a permanent failure.
		// err must not be passed through here either: the error text of url.Parse contains the full URL.
		return nil, Permanent(fmt.Errorf("invalid request URL: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refused, DNS failure, timeout: mostly transient faults, left to backoff retry.
		//
		// The error text must be redacted before it is passed outward. Reason: http.Client.Do returns a *url.Error,
		// whose Error() is `Op "full URL": underlying error`, and the credentials of these platforms are **in
		// the URL** (DingTalk access_token, WeCom key, Feishu hook id, Telegram /bot<token>/). Without
		// redaction, the credentials would flow along this error string into four places: last_error of
		// notification_deliveries (persisted in plaintext), the response of the delivery history API
		// (**bypassing the masking of the channel config**), the server logs, and the 502 text that the test-send
		// endpoint returns to the frontend.
		return nil, fmt.Errorf("request failed: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// 429 (rate limited) and 408 (timeout) are worth retrying; the other 4xx are configuration or permission problems and retrying is pointless.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("peer rate limited or timed out (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("peer service error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("peer rejected the request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet squeezes a response body into one short line of text for use in error messages. A response may
// contain newlines and lots of whitespace, and putting it into last_error as is would wreck the layout of
// the delivery history page.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget squeezes a delivery URL into "scheme://host/…" for use in error messages.
//
// This is the only URL redaction policy in this package, and it is deliberately made **crude enough**:
// everything except scheme and host is dropped. The reason is that there is no "generic and safe" way to
// tell which part of a URL is the credential:
//
//	DingTalk    credential in query              /robot/send?access_token=xxx
//	WeCom       credential in query              /cgi-bin/webhook/send?key=xxx
//	Feishu      credential in **last segment**   /open-apis/bot/v2/hook/<hook_id>
//	Telegram    credential in **middle**         /bot<token>/sendMessage
//
// "Keeping only the useful part" would require per-channel patches, and missing any one platform is a
// credential leak. Keeping the host is already enough for troubleshooting (DNS failures, connection
// failures and certificate problems can all be located), and which robot it is can be recognized by the
// masked tail hint in the channel config.
//
// On a parse failure a fixed placeholder is returned; the original string is never echoed back.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unparseable address)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError strips the address from a transport-layer error and keeps only the underlying cause.
//
// The structure of *url.Error is {Op, URL, Err}, and Error() prints the URL along with it.
// Here the Err field is taken explicitly to bypass its Error(), which is more reliable than string
// replacement after the fact, because replacement has to correctly handle all the variants after URL
// encoding/escaping and is easy to miss.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: unknown error", uerr.Op, host)
	}
	// Errors that are not *url.Error (such as the one returned by the redirect policy) may also carry an address, so they all go through redaction.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces http(s) addresses appearing in a piece of text with their redacted form.
//
// It is a backstop for errors whose structured fields are not available (redirect policy errors, custom
// errors from third-party libraries). Only the http/https prefix is recognized, and the address is cut at
// whitespace and quotes, since addresses never contain those two kinds of characters.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
