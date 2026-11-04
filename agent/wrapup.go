package agent

import (
	"strings"

	"github.com/Autumn-27/norma/harness"
)

// Wrap-up prompt (wrap-up / settlement prompt): when an agent is terminated because its [turn budget is exhausted (MaxTurns)] or
// [it timed out (run_seconds/MaxDuration)], the SDK's settlement phase injects this prompt,
// so that the agent first persists what it has identified but not yet written back, and then outputs a one-sentence summary, avoiding a half-finished run.
//
// Each agent's wrap-up prompt can be overridden in the admin UI as needed (stored in agents.wrapup_prompt); when left empty, the
// built-in default here is used. Only the [prompt body] is editable; which tools are disabled and how many turns the wrap-up itself gets are code-fixed policy.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in default wrap-up prompts, indexed by agent key. The worker reuses settleWrapUpPrompt, which was historically hard-coded
// (defined in worker.go); planner/mainagent each have their own; keys not found (custom agents) fall back to the generic one.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults: the built-in default turn budget for each agent's wrap-up phase [itself] (can be overridden by an admin value > 0).
// All get 10 turns, to guarantee the wrap-up phase has enough steps to persist. Keys not found use genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "Your planning turns for this round are about to run out -- note that only [this round] is ending; the system will wake you again later as the situation changes so you can keep planning. The task is not terminating, and you do not need to wrap up the entire plan here. Land the conclusions you have already worked out this round so it is not wasted, but also [do not pad out intents just to finish] (0 intents this round is still a perfectly normal result): (1) if you have already judged exploration directions that [should be dispatched right now], submit them in one batch with a single add_intent (do not hold back what you have thought through); (2) for goals already proven achieved by some finding/fact, call prove_goal to mark them met (do not miss any); (3) if you have identified a serial exploitation chain that needs to be done step by step, record it with TodoWrite so that the next wake-up can continue dispatching it. When done, simply end this round; no summary text is needed."

const mainAgentWrapUpDefault = "Your turns are about to run out and this interaction is about to end. Do not start any new exploration/operations. **In a single sentence of plain text**, summarize the current progress, the key conclusions and the suggested next step for the user."

const genericWrapUpDefault = "You are about to be terminated because your budget is exhausted. First write back the results that are complete but not yet persisted, then **in a single sentence of plain text** summarize what you did and which key conclusions you reached (this sentence will be displayed as the result of this run)."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        resolveWrapup(agentKey),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// ---------- Task-level timeout wrap-up prompts (see docs/任务级超时与收尾设计.md; intentionally Chinese: existing file name) ----------
//
// These are a [separate set] from the per-run wrap-up prompts: per-run means "the budget of this one run is used up"; a task timeout means
// "the whole task is due and about to end". The semantics are often opposite (especially for the planner: per-run says "do not stop, keep planning",
// while a task timeout says "time is up, stop planning and make the final judgment"). Configured for worker/planner only.

// WrapupTaskTimeoutOverride / ...TurnsOverride: DB overrides for the task-timeout wrap-up prompt and turn count
// (wired to agents.task_timeout_wrapup_prompt / _max_turns, worker/planner only).
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "**The whole task has reached its timeout limit and is about to end** (this is not the budget of this run of yours; the whole exploration is due). This is the last chance: (1) persist [all] of what you have identified but not yet written back -- new assets with insert_assets, exploration conclusions/facts with record_fact, confirmed vulnerabilities with report_finding; (2) do not start any new command/probe; (3) **finally, in a single sentence of plain text**, summarize your key conclusions on this intent."

const plannerTaskTimeoutDefault = "**The whole task has reached its timeout limit and is about to end** (not just this round; the entire task is terminating). Based on [all] of the current facts and findings, make one last goal judgment: for goals proven achieved by evidence, call prove_goal to mark them met (do not miss any). **Do not generate any new intents** (an intent dispatched now will not be executed any more). Wrap up as soon as the judgment is done; no summary text is needed."

// TaskTimeoutWrapupDefault returns an agent's built-in default task-timeout wrap-up prompt (for the admin UI placeholder / restore default).
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // not configured (mainagent/chat) returns an empty string
}

// resolveTaskTimeoutWrapup: DB override (non-empty) > built-in default. An empty string means this agent has no task-timeout prompt
// (not worker/planner), in which case the caller should fall back to the per-run prompt.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // by default reuse the per-run turn count
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true  -> this run was squeezed by the task deadline: wrap-up due to Timeout = task is due -> task-timeout prompt;
//     wrap-up due to MaxTurns = the turns ran out first inside the clamped window while the task still has a few minutes -> fall back to the per-run prompt.
//   - clamped=false -> the task is still early: both reasons use the per-run prompt (i.e. it degrades to wrapupSettlement).
//
// The PromptByReason handed to the harness picks on the spot by the [actual] reason at wrap-up time, so there is no build-time mismatch.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := resolveWrapup(agentKey)
	st := &harness.Settlement{
		Prompt:        perRun, // fallback (also the value for both reasons when not clamped)
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  tt,     // task is due
				harness.ReasonMaxTurns: perRun, // turns ran out first, the task still has time left
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}
