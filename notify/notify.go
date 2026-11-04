// Package notify implements the adapter layer that delivers finding notifications to IM and email channels.
//
// Layering: this package is a **leaf package** and depends only on the standard library. It knows nothing
// about the database or the server. Channel configuration is passed in as a map[string]any (matching the
// notification_channels.config JSONB column) and the content to deliver is passed in as a Message. The
// benefit of this split is that the error-prone parts (signature computation, UTF-8 truncation, filter
// matching) can be unit-tested without PostgreSQL, and the host only has to do the orchestration on the
// server side.
//
// Concurrency contract: Channel implementations must be **stateless**. The same Channel instance is reused
// concurrently by many channel configurations (even by several bot instances of one channel), so all
// credentials are passed in through the cfg parameter, and implementations must not cache things such as
// webhook URLs in their own fields.
package notify

// Channel kind identifiers. The values are also the legal set of notification_channels.kind and are checked
// against an allowlist on the server side (like findings.status, no DB CHECK is used so channels are easy to add later).
const (
	KindDingTalk = "dingtalk" // DingTalk custom bot
	KindFeishu   = "feishu"   // Feishu (Lark) custom bot
	KindWeCom    = "wecom"    // WeCom group bot
	KindWebhook  = "webhook"  // generic webhook: custom method/headers/JSON template
	KindTelegram = "telegram" // Telegram Bot API
	KindEmail    = "email"    // SMTP email
)

// Event kinds, matching notification_events.kind.
const (
	EventFindingCreated       = "finding_created"
	EventFindingStatusChanged = "finding_status_changed"
)

// InitKind is the fallback value for an empty kind in config.
const InitKind = KindDingTalk

// severityRank maps a finding severity to a comparable ordinal. An unknown severity returns 0, so any
// min_severity setting keeps unknown severities out: when in doubt, do not notify, to avoid false-alarm spam.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// SeverityRank returns the ordinal of a severity; an unknown severity returns 0.
func SeverityRank(severity string) int { return severityRank[severity] }

// SeverityLabel returns the display name of a severity with an emoji, used in message titles and card colouring.
// An unknown severity is echoed back as is, never invented.
func SeverityLabel(severity string) string {
	switch severity {
	case "critical":
		return "🔴 Critical"
	case "high":
		return "🟠 High"
	case "medium":
		return "🟡 Medium"
	case "low":
		return "🔵 Low"
	default:
		return severity
	}
}

// StatusLabel translates a disposition status into a display label, used in status-change messages.
func StatusLabel(status string) string {
	switch status {
	case "pending":
		return "Pending"
	case "in_progress":
		return "In progress"
	case "confirmed":
		return "Confirmed"
	case "resolved":
		return "Resolved"
	case "fixed":
		return "Fixed"
	case "false_positive":
		return "False positive"
	case "ignored":
		return "Ignored"
	case "duplicate":
		return "Duplicate"
	case "risk_accepted":
		return "Risk accepted"
	default:
		return status
	}
}

// AtLeast reports whether severity reaches the min threshold. An empty min means no threshold, so everything passes.
// Note that an unknown severity has ordinal 0 and is rejected by any non-empty min (see the severityRank comment).
func AtLeast(severity, min string) bool {
	if min == "" {
		return true
	}
	return SeverityRank(severity) >= SeverityRank(min)
}
