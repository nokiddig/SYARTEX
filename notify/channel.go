package notify

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Channel is the adapter for one notification channel. Implementations must be **stateless**: the same
// instance is reused concurrently by many channel configs, and credentials are always passed in via cfg.
type Channel interface {
	// Kind returns the channel type identifier; it must match the registry key.
	Kind() string
	// Validate is called when a config is saved and checks required fields and formats. The returned error
	// is shown directly to the person configuring the channel, so the message should say which field is
	// missing rather than a vague "invalid configuration".
	Validate(cfg map[string]any) error
	// Send delivers one message and returns the **number of items actually delivered** plus an error.
	//
	// Why the count is returned: every platform caps message length, so a digest that does not fit is
	// truncated. If the caller unconditionally marked the whole batch as delivered, the truncated items
	// would vanish: they are absent from the message, the delivery history shows success, and nothing
	// reveals that a finding was never sent. With kept returned, the caller marks only the first kept
	// items and leaves the rest for the next batch.
	//
	// A returned error means delivery failed; *PermanentError means it must not be retried.
	// On failure kept is meaningless and the caller must ignore it.
	Send(ctx context.Context, cfg map[string]any, m Message) (int, error)
	// DefaultRatePerMin returns the platform's officially recommended per-minute delivery cap, used as the
	// default rate limit when a new channel instance is created. 0 means no known limit.
	DefaultRatePerMin() int
	// SecretKeys returns the keys in this channel's config that are credentials. When the API echoes a
	// config, the values of these keys are masked; on update, a masked value means "keep the stored value".
	// Only the implementation knows which fields count as credentials (for WeCom the whole webhook URL is
	// the credential, while for DingTalk it is only the secret), so this knowledge must come from the
	// channel and cannot be guessed by the layer above.
	SecretKeys() []string
	// DestinationKeys returns the keys in this channel's config that decide "where the message goes".
	//
	// Like SecretKeys, this is security-relevant: the destination address and the credentials are two
	// independent sets of fields. If "change only the address, keep the credentials as is" were allowed,
	// anyone who can edit a channel config could send the real stored credentials to a server they
	// control, and masking the channel config would be pointless.
	// See PrepareConfigUpdate.
	DestinationKeys() []string
}

// registry is the channel registry. It is deliberately an explicit literal rather than init()
// self-registration: that way "which channels exist" can be seen in one place, and a newly added
// channel that is missed shows up at compile time instead of depending on runtime side effects.
var registry = map[string]Channel{
	KindDingTalk: dingTalkChannel{},
	KindFeishu:   feishuChannel{},
	KindWeCom:    weComChannel{},
	KindWebhook:  webhookChannel{},
	KindTelegram: telegramChannel{},
	KindEmail:    emailChannel{},
}

// Get returns the channel implementation for the given kind.
func Get(kind string) (Channel, bool) {
	c, ok := registry[kind]
	return c, ok
}

// ValidKind reports whether kind is a supported channel type.
func ValidKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns all supported channel types in lexical order (so the UI dropdown stays stable).
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PermanentError marks a delivery failure that must not be retried: bad credentials, rejection by the
// target, an invalid request body, and so on. Retrying only makes sense for transient faults (network
// blips, rate limiting, remote 5xx); repeatedly backing off and retrying a permanent failure never
// succeeds and buries the real error in the retry log.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks err as a permanent failure. It returns nil when err is nil, which makes it
// convenient to write `return Permanent(someCheck())`.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether the err chain carries the permanent-failure marker.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// ---- config reading helpers ----
//
// Channel configs come from a JSONB column in the database. After decoding with encoding/json they
// are map[string]any, where numbers are always float64 and arrays are []any. The helpers below unify
// that conversion and tolerate type mismatches caused by users leaving fields blank in the UI (such
// as entering the port as a string).

// cfgString reads a string config item and always trims surrounding whitespace, which is easy to
// pick up when copy-pasting from a web form.
func cfgString(cfg map[string]any, key string) string {
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// cfgInt reads an integer config item, accepting both float64 (the JSON default) and string sources.
func cfgInt(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// cfgBool reads a boolean config item, accepting the strings "true"/"1".
func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// cfgStrings reads a string-array config item, trimming whitespace and dropping empty strings.
func cfgStrings(cfg map[string]any, key string) []string {
	raw, ok := cfg[key].([]any)
	if !ok {
		// A single string is also accepted, which is convenient for form submissions with only one value.
		if s := cfgString(cfg, key); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cfgMap reads a string-map config item (such as custom HTTP headers), trimming whitespace
// from keys and values and dropping empty keys.
func cfgMap(cfg map[string]any, key string) map[string]string {
	raw, ok := cfg[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}
