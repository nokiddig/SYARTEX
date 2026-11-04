package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// dingTalkChannel implements the DingTalk custom robot.
//
// Platform characteristics (which drive the implementation trade-offs here):
//   - A single robot is rate limited to 20 messages/minute; excess messages are silently dropped (HTTP
//     may still return 200), so rate limiting must be done on the client side, see DefaultRatePerMin.
//   - Security settings are a choice of one: signing / custom keywords / IP allowlist. Signing is the only
//     scheme that does not depend on message content, so only signing is supported (a bare webhook with
//     none of the three enabled is also supported).
//   - Both success and failure return HTTP 200 and are told apart by errcode in the body; without
//     checking errcode a failed delivery would be recorded as a success.
type dingTalkChannel struct{}

func (dingTalkChannel) Kind() string { return KindDingTalk }

func (dingTalkChannel) DefaultRatePerMin() int { return 20 }

// The DingTalk webhook URL carries access_token and is itself a credential, so it is masked as a whole.
func (dingTalkChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// The destination is the DingTalk webhook URL itself; changing it requires restating the signing secret for the new URL.
func (dingTalkChannel) DestinationKeys() []string { return []string{"webhook"} }

func (dingTalkChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("missing Webhook URL")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("invalid Webhook URL: %w", err)
	}
	return nil
}

// Send delivers one message. With a detail link and a single item it uses an ActionCard (with a button); otherwise markdown.
func (c dingTalkChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	hook := cfgString(cfg, "webhook")
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := dingTalkSignedURL(hook, cfgString(cfg, "secret"), time.Now())
	if err != nil {
		return 0, Permanent(err)
	}

	title := markdownTitle(m)
	// DingTalk markdown text has no documented byte limit, but a cap is still applied so an abnormally
	// large evidence field cannot blow up the message.
	text, kept := markdownBody(m, 20000)

	var payload any
	if !m.Batch && len(m.Items) == 1 && m.Items[0].DetailURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          title,
				"text":           text,
				"btnOrientation": "0",
				"singleTitle":    "View details",
				"singleURL":      m.Items[0].DetailURL,
			},
		}
	} else {
		payload = map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": title, "text": text},
		}
	}

	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	// DingTalk hides business errors inside a 200 response.
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("failed to parse DingTalk response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 301000 is a signature verification failure and 310000 is a keyword mismatch; both are configuration
		// errors that retrying will not fix.
		return 0, Permanent(fmt.Errorf("DingTalk returned error %d: %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

// dingTalkSignedURL appends the timestamp and sign parameters to the webhook following the official signing rules.
//
// Rule: string to sign = timestamp + "\n" + secret, and the HMAC-SHA256 **key is also the secret**;
// the result is base64-encoded and then URL-encoded. The timestamp is in milliseconds. When secret is
// empty the webhook is returned unchanged, to support robots with signing disabled.
func dingTalkSignedURL(hook, secret string, now time.Time) (string, error) {
	if secret == "" {
		return hook, nil
	}
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(hook)
	if err != nil {
		// Do not pass err through: the error text of url.Parse includes the full URL (with access_token).
		return "", fmt.Errorf("failed to parse Webhook URL: %s", redactRequestTarget(hook))
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// validateHTTPURL checks that the URL is usable and the scheme is supported, and judges literal-IP targets for internal addresses.
//
// Two points worth noting:
//
//  1. **Error messages must be redacted**. url.Parse returns a *url.Error whose Error() includes the
//     **full original URL**, and the URLs of these platforms embed credentials (DingTalk access_token,
//     WeCom key, Telegram bot token, Feishu hook id). This used to `return err` directly, so the
//     "malformed URL" error carried the credentials out to the 400 response of the test endpoint, the
//     last_error persisted on every delivery, the server logs and the delivery history API.
//
//  2. **Literal IPs are judged as internal immediately**, while domain names are left to the dial stage
//     (blockInternalDial is the point where the rule finally takes effect and also covers DNS
//     rebinding). It is done once here so the user gets a hint when saving the config rather than
//     after the first delivery fails.
//
// Restricting the scheme is defensive: file:/// or gopher:// and the like would make http.Client behave
// unexpectedly (they are already blocked by the scheme check, but there is no reason to open up this surface).
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL cannot be parsed (%s)", redactRequestTarget(raw))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http/https is supported, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("missing host name")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedDialIP(ip) && !allowLocalTargets() {
		return fmt.Errorf("refusing to deliver to a loopback/link-local address %s (to deliver to a local service, set %s=1)", ip, AllowLocalTargetsEnv)
	}
	return nil
}
