package notify

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// This file covers two related hardening measures:
//   (1) delivery addresses must not use the server as a springboard to hit the internal network / cloud metadata (SSRF)
//   (2) error messages from address validation must not carry credentials from the address
//
// About the test environment: many cases in this package use httptest fake receivers on 127.0.0.1, which the
// guard blocks by default. So TestMain switches AllowLocalTargetsEnv on globally, and every SSRF case below
// explicitly clears it, in order to assert the **deny by default** behaviour.

func TestMain(m *testing.M) {
	// Let the regular cases connect to the local fake receiver; the SSRF cases temporarily clear it themselves.
	_ = os.Setenv(AllowLocalTargetsEnv, "1")
	os.Exit(m.Run())
}

// TestDialGuardRejectsLoopbackByDefault is the core assertion of the SSRF protection:
// with the default configuration, delivery to a loopback address must be rejected at the **connection layer**.
func TestDialGuardRejectsLoopbackByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "") // turn off the escape hatch = default behaviour
	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("delivery to a loopback address should not be allowed by default")
	}
	if hit {
		t.Fatal("the request already reached the local service: the guard did not take effect")
	}
	// The error message must tell the user how to lift the restriction (a local SMTP relay is a legitimate configuration).
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("the rejection message should explain how to opt in explicitly: %v", err)
	}
}

// TestDialGuardAllowsLoopbackWhenOptedIn is the reverse case: once explicitly switched on it must work,
// otherwise legitimate deployments such as a local postfix / an internal relay would be wiped out wholesale.
func TestDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("delivery should work after explicitly opting in: %v", err)
	}
}

func TestIsBlockedDialIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"169.254.169.254", // cloud metadata endpoint: the main reason this function exists
		"169.254.1.1", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", // an IPv4-mapped form must be unmapped before judging, otherwise it is a bypass
		"",
	}
	for _, s := range blocked {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be rejected", s)
		}
	}
	// RFC1918 private networks are **deliberately allowed**: a self-hosted Mattermost / SMTP relay on the internal
	// network is a common legitimate use. This assertion pins that trade-off down: if someone later adds a
	// private-network check in passing, this fails, which forces a conscious decision (instead of silently breaking
	// a batch of deployments).
	allowed := []string{"10.0.0.5", "172.16.3.4", "192.168.1.10", "8.8.8.8", "2606:4700::1111"}
	for _, s := range allowed {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed (private networks are common legitimate delivery targets)", s)
		}
	}
}

// TestValidateHTTPURLRejectsLiteralPrivateTargets covers the early hint at configuration time:
// a literal IP should be rejected at save time, rather than waiting for the first delivery to fail.
func TestValidateHTTPURLRejectsLiteralPrivateTargets(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "")
	for _, raw := range []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:8080/hook",
	} {
		if err := validateHTTPURL(raw); err == nil {
			t.Errorf("%s should be rejected at configuration time", raw)
		}
	}
	// Public addresses and private addresses pass as usual (private ones are left to the dial stage, which does not block them).
	for _, raw := range []string{"https://oapi.dingtalk.com/robot/send", "http://10.0.0.9/hook"} {
		if err := validateHTTPURL(raw); err != nil {
			t.Errorf("%s should pass validation: %v", raw, err)
		}
	}
}

// TestValidateHTTPURLErrorNeverLeaksCredentials is a branch the audit pointed out that I missed in the previous round.
//
// When url.Parse **fails** it returns a *url.Error whose Error() contains the full original address. Last round I
// only redacted the error returned by http.Client.Do and missed this one; and the "permanent failure path" cases
// added back then (file://, gopher://, ftp://) can in fact all be parsed successfully by url.Parse and go through
// the scheme branch, so being all green proves nothing about this path being safe: it was a false assurance.
func TestValidateHTTPURLErrorNeverLeaksCredentials(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/%zz?access_token=" + leakProbeToken,         // invalid percent escape
		"https://a.example.com:port/x?access_token=" + leakProbeToken, // non-numeric port
		"http://[::1?access_token=" + leakProbeToken,                  // unbalanced brackets
	}
	for _, raw := range cases {
		// First confirm that this input **really** makes url.Parse fail. Without this step the case might silently end up
		// in another branch (which is how last round's false assurance came about).
		if _, err := url.Parse(raw); err == nil {
			t.Errorf("%q should fail to parse, otherwise this case does not cover the target branch", raw)
			continue
		}
		err := validateHTTPURL(raw)
		if err == nil {
			t.Errorf("%q should fail validation", raw)
			continue
		}
		assertNoSecret(t, err.Error(), leakProbeToken)
	}
	// Confirm that the channel-layer wrapper does not carry the address out either.
	t.Setenv(AllowLocalTargetsEnv, "")
	err := (dingTalkChannel{}).Validate(map[string]any{"webhook": cases[0]})
	if err == nil {
		t.Fatal("an invalid address should fail validation")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// TestEmailDialGuardRejectsLoopbackByDefault covers the dial guard of the SMTP channel.
//
// The email channel used to use a bare net.Dialer, the only gap in the whole SSRF protection: with host set to
// 169.254.169.254 or 127.0.0.1 it could connect directly, and when the smtp.NewClient handshake fails it wraps the
// line returned by the peer into the error, which the delivery-history API echoes through last_error: exactly the
// half-blind read primitive the other channels have already closed; the timing difference between "connection
// refused vs timeout" could also be used to probe ports.
//
// This package's TestMain switches AllowLocalTargetsEnv on globally (many cases use fake receivers on 127.0.0.1),
// so this case must clear it itself: otherwise it would pass whether or not the guard exists, which is also why
// nothing caught the gap in the first place.
func TestEmailDialGuardRejectsLoopbackByDefault(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "") // turn off the escape hatch = default behaviour
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Fatal("delivering email to a loopback address should not be allowed by default")
	}
	// The connection must not be established at all: the guard stops it in the Control hook, so EHLO can never be sent.
	if f.sawCommand("EHLO") || f.sawCommand("HELO") {
		t.Fatal("the SMTP session was already established: the guard did not take effect")
	}
	// The error message must tell the user how to lift the restriction (a local postfix relay is a legitimate configuration).
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("the rejection message should explain how to opt in explicitly: %v", err)
	}
}

// TestEmailDialGuardAllowsLoopbackWhenOptedIn is the paired reverse case: once explicitly switched on,
// delivery must work normally. A self-hosted internal SMTP / local relay is a very common deployment, and the guard must not be a blanket ban.
func TestEmailDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("a local SMTP server should accept delivery after explicitly opting in: %v", err)
	}
	if !f.sawCommand("EHLO") {
		t.Fatal("EHLO was not seen: the session was not really established")
	}
}
