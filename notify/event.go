package notify

// Snapshot is the contract for the notification_events.snapshot JSONB column. The writer is the db
// layer's finding-persist transaction; the readers are the server layer's delivery engine and filter
// matching. It is defined in this package because it is a payload of the "notification domain": db only
// serializes it and does not understand what the fields mean.
//
// Why the finding fields are stored redundantly instead of looked up at render time: a finding can be
// renamed, re-leveled or have its status changed later, while the push content should reflect the
// conclusion **as of when the event happened**. A lookup would yield the dangerously misleading
// "later changed to low". It also means fan-out and rendering do not need to JOIN the findings, tasks
// and assets tables.
type Snapshot struct {
	// Event type: finding_created / finding_status_changed
	Kind      string  `json:"kind"`
	FindingID int64   `json:"finding_id"`
	TaskID    int64   `json:"task_id"`
	VulnClass string  `json:"vulnclass"`
	Name      string  `json:"name"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
	AssetIDs  []int64 `json:"asset_ids"`
	// Non-empty only when kind=finding_status_changed.
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Item is one finding to be pushed, for channels to render.
type Item struct {
	FindingID int64
	Name      string
	VulnClass string
	Severity  string
	Summary   string
	// Assets are the resolved asset display names (such as domain/IP). They are filled in by the server
	// layer: this package does not touch the database and cannot look up names.
	Assets []string
	// DetailURL is the link back to the finding detail; empty means public_base_url is not configured and it is omitted when rendering.
	DetailURL string
	// Used only by status-change events; when both are non-empty it renders as "Pending → Fixed".
	FromStatus string
	ToStatus   string
}

// IsStatusChange reports whether the item is a status-change event.
func (i Item) IsStatusChange() bool { return i.FromStatus != "" || i.ToStatus != "" }

// Title returns the item's display title: the human-assigned name first, falling back to the finding
// type vulnclass, and to a placeholder when both are empty, so an empty title is never output.
func (i Item) Title() string {
	if i.Name != "" {
		return i.Name
	}
	if i.VulnClass != "" {
		return i.VulnClass
	}
	return "(unnamed vulnerability)"
}

// Message is the complete content of one channel send.
type Message struct {
	// Length is 1 for a single push and a whole batch for a digest push.
	// An empty slice is invalid; callers must guarantee at least one item.
	Items []Item
	// When Batch=true it is rendered as a digest message (different title, with the time window and item count).
	Batch bool
	// WindowMinutes is the digest period in minutes, used only when Batch=true for the text "Last N minutes".
	// It is deliberately passed in explicitly by the configuration rather than computed with time.Since at
	// render time: keeping rendering deterministic is what makes it testable.
	WindowMinutes int
	// HomeURL is the platform dashboard address (the global public_base_url); when empty, no dashboard entry is included.
	HomeURL string
}
