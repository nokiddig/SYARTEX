package agent

import (
	"log"
	"path/filepath"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/noaadapter"
)

// noaWarn returns a diagnostics sink tagging non-fatal noa messages with the
// session, routed through the package logger (agents have no per-instance one).
func noaWarn(session string) func(string) {
	return func(msg string) { log.Printf("[noa] %s: %s", session, msg) }
}

// noa is the "model-driven context compression" mechanism introduced in norma v0.4.0,
// exposed as an experimental platform feature that users toggle in system settings. It is
// mutually exclusive with the built-in compaction: noaadapter.Enable is the single entry
// point and attaches the context takeover (Compactor), the Compress tool and three
// resident prompt sections in one go; not calling Enable means it is off (built-in
// compaction works as usual). The switch is resolved by the noaEnabledFn injected into
// each agent and read once per run, so toggling only affects runs started afterwards and
// no agent rebuild is needed.

// enableNoa wires noa into opts when the resolver reports it enabled. archiveRoot is the
// base directory that persists the original compressed text (the global workDir; all
// agents use <workDir>/noa uniformly instead of scattering it across task/intent
// directories), and sessionID names the archive subdirectory under it (globally unique,
// so no collisions within the same base directory).
//
// noa is experimental: a failure to attach it must not interrupt a real task. On error it
// is reported via onWarn and the built-in compression is used as a fallback.
// On success opts.Compaction is cleared so agentcore does not warn about "two context
// managers set at the same time".
func enableNoa(opts *agentcore.Options, enabled func() bool, archiveRoot, sessionID string, onWarn func(string)) {
	if enabled == nil || !enabled() {
		return
	}
	if opts.OnWarn == nil {
		opts.OnWarn = onWarn
	}
	if err := noaadapter.Enable(opts, noaadapter.Options{
		ArchiveBaseDir: filepath.Join(archiveRoot, "noa"),
		SessionID:      sessionID,
		OnWarn:         onWarn,
	}); err != nil {
		if onWarn != nil {
			onWarn("failed to enable noa compression, falling back to built-in compression: " + err.Error())
		}
		return
	}
	// Compactor overrides Compaction, but agentcore warns every time both coexist; clear it explicitly.
	opts.Compaction = nil
}
