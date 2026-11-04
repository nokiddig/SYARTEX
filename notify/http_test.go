package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// This file covers the HTTP-layer error grading of doJSON.
//
// Why it is tested separately: the channel adapters only handle the platform's own business error codes
// (DingTalk errcode, Feishu code, the Telegram ok field), while the **HTTP-layer** grading is done by
// doJSON uniformly, and the two are independent lines of defense. Without this one, a relay gateway
// that returns 503 would be treated as a permanent failure and retrying would be given up, while a
// 403 would be treated as retryable and back off pointlessly for three rounds.

func replyServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoJSONClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		permanent bool
	}{
		{"200 success is not an error", 200, false},
		{"429 rate limited, retryable", 429, false},
		{"408 request timeout, retryable", 408, false},
		{"500 server error, retryable", 500, false},
		{"502 gateway error, retryable", 502, false},
		{"503 service unavailable, retryable", 503, false},
		{"400 bad request, permanent failure", 400, true},
		{"401 authentication failure, permanent failure", 401, true},
		{"403 forbidden, permanent failure", 403, true},
		{"404 not found, permanent failure", 404, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := replyServer(t, tc.status, `{"detail":"upstream says no"}`)
			_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
			if tc.status < 300 {
				if err != nil {
					t.Fatalf("2xx should not produce an error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("a non-2xx response should produce an error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("wrong permanent classification for HTTP %d: expected %v, got %v (%v)",
					tc.status, tc.permanent, got, err)
			}
			// The status code must appear in the error, otherwise the user cannot tell whether they misconfigured it or the peer is down.
			// Assert the number rather than Go's StatusText: the number is the language-independent part
			// that can be asserted stably.
			if !strings.Contains(err.Error(), strconv.Itoa(tc.status)) {
				t.Errorf("the error message should carry the HTTP status code %d, got %v", tc.status, err)
			}
		})
	}
}

// TestDoJSONIncludesResponseSnippet covers snippet: the error description returned by the peer must be
// brought back, otherwise the user only knows "it failed" and not why the peer rejected it.
func TestDoJSONIncludesResponseSnippet(t *testing.T) {
	srv := replyServer(t, 400, `{"error":"invalid webhook token"}`)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("an error is expected")
	}
	if !strings.Contains(err.Error(), "invalid webhook token") {
		t.Errorf("the error message should bring back the peer's description, got %v", err)
	}
}

// TestDoJSONSnippetIsSingleLineAndBounded constrains the shape of snippet:
// the peer's response goes as is into the last_error column and the frontend table, and multiple lines or excessive length would break layout and payload size.
func TestDoJSONSnippetIsSingleLineAndBounded(t *testing.T) {
	// A response containing newlines, tabs and 5000 characters of excess content.
	long := strings.Repeat("x", 5000)
	srv := replyServer(t, 500, "line1\nline2\r\n\tline3 "+long)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("an error is expected")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\r\n\t") {
		t.Errorf("the error message should be squeezed into a single line, got %q", msg)
	}
	// The snippet limit is 200 characters + a fixed prefix, so the total must be far smaller than the original response.
	if len(msg) > 400 {
		t.Errorf("error message too long (%d bytes), it should have been truncated by snippet: %q", len(msg), msg)
	}
}

// TestDoJSONRejectsOversizedResponse confirms reading is capped: when the peer returns huge content abnormally
// the whole response must not be read into memory (every entry in the delivery history stores a copy of last_error).
func TestDoJSONRejectsOversizedResponse(t *testing.T) {
	huge := strings.Repeat("A", 1<<20) // 1 MiB
	srv := replyServer(t, 400, huge)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("an error is expected")
	}
	if len(err.Error()) > 400 {
		t.Errorf("an oversized response should be read with a limit and truncated, error message length %d", len(err.Error()))
	}
}
