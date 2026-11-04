package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// Planner is the event-driven LLM planner (docs §4.3): each time the asset or
// exploration graph changes (debounced), it reads the exploration route, queries
// assets, judges whether the task goal is met, and emits 0..N exploration intents
// into the frontier. It is the sole intent generator.
type Planner struct {
	findingRecorder   FindingRecorder
	prov              llm.Provider
	model             string
	tx                *transcript.Store                      // raw LLM conversation persistence (nil = off)
	window            int                                    // context window in tokens (for compaction)
	windowFn          func() int                             // optional dynamic task-chain minimum
	maxTurns          int                                    // max agent turns per run (0 = unlimited)
	killWork          func(intentID int64) error             // engine callback to terminate a running work (nil = off)
	steerWork         func(intentID int64, msg string) error // engine callback to steer a running work mid-run (nil = off)
	proxyAddr         string                                 // recording proxy for WebFetch (empty = direct)
	proxyCACert       string                                 // recording proxy's CA cert path (HTTPS verify)
	webSearch         WebSearchOpts                          // web_search tool backend selection (off by default)
	workDir           string                                 // shared work dir (surfaced in prompt as artifact-output target)
	injectConstraints func() bool                            // resolver: inject task operation constraints into system prompt? (nil = yes)
	nonStreamingFn    func() bool                            // resolver: use non-streaming (Complete) path? (nil = streaming)
	noaEnabledFn      func() bool                            // resolver: use experimental noa compaction? (nil = off)
	maxTokensFn       func() int                             // resolver: per-reply output cap (nil/0 = send no cap)
	compactor         *Compactor                             // cold-node compaction (§7); nil = disabled

	// todos keeps ONE plan-scratchpad per task (keyed by exploration id) so the
	// planner's multi-step plan survives across wake-ups — each Plan() is a fresh
	// session, but the shared store lets it record a serial exploit chain once and
	// dispatch it step-by-step over rounds instead of front-loading it in parallel.
	todoMu sync.Mutex
	todos  map[int64]*actool.TodoStore
}

func NewPlanner(prov llm.Provider, model, workDir string, tx *transcript.Store, window, maxTurns int) *Planner {
	return &Planner{prov: prov, model: model, workDir: workDir, tx: tx, window: window, maxTurns: maxTurns, todos: map[int64]*actool.TodoStore{}}
}

func (p *Planner) SetCompactionWindowResolver(fn func() int) { p.windowFn = fn }

// SetCompactor wires the cold-node compactor (cold-digest §7). Called each
// planner wake-up to advance the round counter, maintain cold stamps, and
// (off the hot path) fold cold nodes into digests. nil = feature disabled.
func (p *Planner) SetCompactor(c *Compactor) { p.compactor = c }

// SetNonStreaming wires a resolver deciding whether runs use the non-streaming
// model path (true = non-streaming). nil/unset = streaming (default).
func (p *Planner) SetNonStreaming(fn func() bool) { p.nonStreamingFn = fn }

func (p *Planner) nonStreaming() bool { return p.nonStreamingFn != nil && p.nonStreamingFn() }

// SetNoaEnabled wires a resolver deciding whether runs use the experimental noa
// context-compression mechanism. nil/unset = off (built-in compaction). Read per
// run so the settings toggle takes effect without rebuilding the agent.
func (p *Planner) SetNoaEnabled(fn func() bool) { p.noaEnabledFn = fn }

// SetMaxTokens wires a resolver for the per-reply output cap. nil/unset or 0 =
// send no cap and let the endpoint decide. Read per run, like nonStreaming.
func (p *Planner) SetMaxTokens(fn func() int) { p.maxTokensFn = fn }

func (p *Planner) maxTokens() int {
	if p.maxTokensFn == nil {
		return 0
	}
	return p.maxTokensFn()
}

func (p *Planner) compactionWindow() int {
	if p.windowFn != nil {
		return p.windowFn()
	}
	return p.window
}

// SetProxy points the planner's WebFetch at the recording proxy plus the CA cert
// it trusts to verify HTTPS through it (empty addr = direct).
func (p *Planner) SetProxy(addr, caCert string) { p.proxyAddr, p.proxyCACert = addr, caCert }

// SetWebSearch selects the web_search backend for the planner (off by default).
func (p *Planner) SetWebSearch(o WebSearchOpts) { p.webSearch = o }

// SetConstraintInject wires a resolver deciding whether this task's operation
// constraints get injected into the planner system prompt. Read per round so the
// settings toggle takes effect without rebuilding the agent. nil = inject (default).
func (p *Planner) SetConstraintInject(fn func() bool) { p.injectConstraints = fn }

// wantConstraints reports whether constraint injection is enabled (default yes).
func (p *Planner) wantConstraints() bool { return p.injectConstraints == nil || p.injectConstraints() }

// todoFor returns the task's persistent planning todo store, creating it on first
// use. Shared across all of this task's planner wake-ups.
func (p *Planner) todoFor(expID int64) *actool.TodoStore {
	p.todoMu.Lock()
	defer p.todoMu.Unlock()
	s := p.todos[expID]
	if s == nil {
		s = actool.NewTodoStore()
		p.todos[expID] = s
	}
	return s
}

// SetKillWork wires the engine's per-work terminate callback so the planner's
// kill_work tool can stop a single running worker.
func (p *Planner) SetKillWork(fn func(intentID int64) error) { p.killWork = fn }

// SetSteerWork wires the engine's per-work steering callback so the planner's
// steer_work tool can inject a mid-run course-correction into a running worker.
func (p *Planner) SetSteerWork(fn func(intentID int64, msg string) error) { p.steerWork = fn }

// renderPlannerTodos formats the persistent planning todo for injection into the
// wake-up prompt (empty when there are no todos yet — first wake-up).
func renderPlannerTodos(items []actool.Todo) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Your planning todo (kept across wake-ups; written by you in the previous round)]:\n")
	for _, it := range items {
		mark := map[actool.TodoStatus]string{actool.TodoPending: "☐", actool.TodoInProgress: "▶", actool.TodoCompleted: "✔"}[it.Status]
		if mark == "" {
			mark = "☐"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", mark, it.Content))
	}
	b.WriteString("Proceed accordingly: dispatch intents only for the next step whose [prerequisite steps are complete / the facts it depends on already exist]; update the list with TodoWrite (mark steps already satisfied by a fact as completed). Do not re-dispatch steps that are already pending/in_progress in the list.")
	return b.String()
}

// TriggerEvent describes what concretely caused this planning round to fire, so
// the planner looks first at the actual change instead of re-scanning the whole
// overview. Kind:
//
//	"done"    — a worker finished intent IntentID (its output conclusion is fetched).
//	"finding" — a worker reported a finding on intent IntentID (Detail = summary).
//	"goal"    — the human (via the main agent's set_goals) added one OR MORE goals in a
//	            single call (Goals = the goal texts added this time, 1+ entries; set_goals supports batches).
//	"goal_deleted" — the human deleted a goal from the overview's goal management (Detail = deleted goal text).
//	"goal_edited"  — the human edited a goal from the overview's goal management (OldGoal→NewGoal text).
//	"cancelled" — the human deleted intent IntentID (Detail = deletion reason). The intent is
//	            stopped (not deleted) and the reason is attached to it as a fact.
type TriggerEvent struct {
	Kind     string
	IntentID int64
	Detail   string
	Summary  string   // Kind=="cancelled" only: intent summary captured before deletion (the node no longer exists after a real delete, so it cannot be looked up)
	Goals    []string // Kind=="goal" only: goal texts added by this set_goals call (one or more)
	OldGoal  string   // Kind=="goal_edited" only: goal text before the edit
	NewGoal  string   // Kind=="goal_edited" only: goal text after the edit
	Hints    []string // Kind=="hint" only: hint texts added by this add_hint call (one or more)
}

// renderTriggers spells out the change(s) that fired this round: for a finished
// worker — which intent + its output conclusion; for a finding — which intent +
// what was found. Empty for time/heartbeat wakes. Reads the store (best-effort;
// a blank field never blocks the round).
func renderTriggers(ts *db.ExplorationStore, evs []TriggerEvent) string {
	if len(evs) == 0 || ts == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[What actually changed to trigger this round (look here first, then decide whether to add directions)]:")
	for _, ev := range evs {
		switch ev.Kind {
		case "goal":
			if len(ev.Goals) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (via the main agent) added a goal: %s -- a new goal to be achieved; add exploration directions accordingly (if no matching intent exists yet).", ev.Goals[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (via the main agent) added %d goals: %s -- all are new goals to be achieved; add exploration directions one by one for each goal that has no matching intent yet.", len(ev.Goals), strings.Join(ev.Goals, "; ")))
			}
		case "hint":
			if len(ev.Hints) == 1 {
				b.WriteString(fmt.Sprintf("\n- The human (via the main agent) added a strategic hint: %s -- it is now attached to the exploration graph; adjust/add exploration directions accordingly (if no matching intent exists yet).", ev.Hints[0]))
			} else {
				b.WriteString(fmt.Sprintf("\n- The human (via the main agent) added %d strategic hints: %s -- all are attached to the exploration graph; adjust/add exploration directions for each accordingly.", len(ev.Hints), strings.Join(ev.Hints, "; ")))
			}
		case "goal_deleted":
			b.WriteString(fmt.Sprintf("\n- The human deleted this goal: %s -- the goal has been removed; re-judge the remaining goals/directions accordingly (no need to dispatch intents for it any more).", ev.Detail))
		case "goal_edited":
			b.WriteString(fmt.Sprintf("\n- The human edited a goal, from '%s' to '%s' -- adjust exploration directions to the new goal (stop dispatching the old direction if it no longer applies).", ev.OldGoal, ev.NewGoal))
		case "finding":
			b.WriteString(fmt.Sprintf("\n- The worker of intent #%d (%s) reported a finding: %s", ev.IntentID, intentSummary(ts, ev.IntentID), ev.Detail))
		case "cancelled":
			// Prefer the Summary captured at deletion time for the intent content (the node no longer exists after a real delete, so intentSummary cannot find it).
			sm := ev.Summary
			if sm == "" {
				sm = intentSummary(ts, ev.IntentID)
			}
			b.WriteString(fmt.Sprintf("\n- Intent #%d was deleted by the user; the intent content was: %s; the deletion reason was: %s. The intent has been deleted (it will no longer execute); re-plan accordingly.", ev.IntentID, sm, ev.Detail))
		default: // "done"
			b.WriteString(fmt.Sprintf("\n- The worker of intent #%d (%s) finished; output conclusion: %s", ev.IntentID, intentSummary(ts, ev.IntentID), workerOutput(ts, ev.IntentID)))
			if fids := factIDsYielded(ts, ev.IntentID); fids != "" {
				b.WriteString(fmt.Sprintf("; new fact ids produced by this intent: %s ", fids))
			}
		}
	}
	b.WriteString("\n(For full details, query again with node_detail / get_worker_output / list_findings.)")
	return b.String()
}

// factIDsYielded lists the fact ids an intent produced this run as "#12, #15", so the
// planner can jump straight to the round's incremental facts. Empty (best-effort) when
// the intent yielded no facts or the lookup fails.
func factIDsYielded(ts *db.ExplorationStore, id int64) string {
	ids, err := ts.FactsYielded(id)
	if err != nil || len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, fid := range ids {
		parts[i] = fmt.Sprintf("#%d", fid)
	}
	return strings.Join(parts, ", ")
}

// intentSummary reads an intent node's one-line summary (best-effort, "?" on miss).
func intentSummary(ts *db.ExplorationStore, id int64) string {
	n, err := ts.GetNode(id)
	if err != nil || n == nil {
		return "?"
	}
	var p map[string]any
	if json.Unmarshal(n.Payload, &p) == nil {
		if s, ok := p["summary"].(string); ok && s != "" {
			return s
		}
	}
	return "?"
}

// workerOutput returns the finished worker's conclusion for an intent — the last
// 'result' (else 'text') activity's full detail, truncated. Same source get_worker_output uses.
func workerOutput(ts *db.ExplorationStore, id int64) string {
	acts, _, err := ts.ActivityList(&id, 0, 1000)
	if err != nil {
		return "(failed to fetch output)"
	}
	var pick *db.Activity
	for i := range acts {
		if acts[i].Kind == "result" {
			pick = &acts[i]
		} else if acts[i].Kind == "text" && pick == nil {
			pick = &acts[i]
		}
	}
	if pick == nil {
		return "(this work has no output record yet)"
	}
	out, _ := ts.ActivityDetail(pick.ID)
	if out == "" {
		out = pick.Summary
	}
	return truncOutput(out, 800)
}

// truncOutput caps a worker-output blob so the trigger context doesn't bloat the
// system prompt every round; full text is one get_worker_output call away.
func truncOutput(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " ... (truncated; see get_worker_output for the full text)"
}

// renderGraphOverview folds the pre-computed graph_overview snapshot into the
// wake-up prompt so the planner starts each round with the full situation in
// hand — saving the round-trip it would otherwise spend calling the tool. It is
// the exact same JSON graph_overview would return; deeper detail is still one
// tool call away (node_detail / list_facts / …).
func renderGraphOverview(data map[string]any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return "" // fall back to the model calling graph_overview itself
	}
	return "\n\n[Current situation (graph_overview prefetch, equal to what calling that tool would return; call node_detail/list_facts etc. on demand for details)]:\n" + string(b)
}

// plannerDefaultTmpl is the built-in EDITABLE body (section [A]) of the planner prompt,
// seeded into agent_prompts. Goal is a {{.Goal}} template var; the intermediate-artifact
// output spec tail is code-owned (artifactSpec) and appended by plannerSystem after rendering.
const plannerDefaultTmpl = `You are the "planner" of an authorized penetration-testing system on a network security platform, and you are woken frequently (every time the graph changes). Your duty: read the situation -> judge the goals -> **add exploration intents only when there is genuinely a new, uncovered direction**. You are a planner, not an executor: everything you produce this round can only be [generating / clarifying intents] or [judging goals]; never do the actual work inside the plan.

Task goal: {{.Goal}}

**How many intents to produce this round (think this through first)**:
- **Hard floor (highest priority)**: whenever [the goal is not met] and [there is currently no open or running intent] (frontier_open=0 and running_intents is empty), you [must] produce at least one intent that advances toward the goal this round -- with no running work to wait for and no queued direction, producing 0 intents = the task stalls; even if every known direction is only in recent_done, use the done/exhausted/blocked judgment below to open another one or continue one.
- Beyond the hard floor, **producing 0 intents is a normal outcome, but it needs a legitimate reason** (not "dispatching less is safer" as a default): (1) **already covered** -- every direction you thought of is already handled by an intent still open/running (regenerating an existing intent with different wording is a serious error); (2) **waiting on a dependency** -- the next step depends on the output of a currently running work that has not arrived yet (forcing a dispatch now would leave the downstream without its prerequisite and spinning idle; wait for the next wake-up after the graph updates, then dispatch).
- Conversely: if there is a genuinely [uncovered direction that does not depend on a running work], or the goal is not met and untested surface remains within scope, you should dispatch -- do not use 0 intents as a lazy default.

**Decision flow on every wake-up**:

1. **The full situation is attached below this prompt** (it is exactly the return value of graph_overview; no need to call it again): task (original title + goal / root node), asset counts, goals + status, open/running/recent_done intents, sites_without_endpoints (sites with no endpoints, hinting at directions that may be worth exploring), facts (number of exploration facts; a different kind from vulnerabilities), recent_facts ({id,summary,confidence?}).
   - **Scope**: exploration nodes (goals/intents/facts/findings) contain only this task; the **asset graph is shared globally** (one copy for multiple tasks; the asset counts are global and within scope, not exclusive to this task) -- ignore assets unrelated to this task when they appear.
   - **Lineage**: every intent carries parents (upstream: which facts/intents it was derived from) and yields (downstream: which facts/findings it produced), and every recent_facts entry carries from_intent; use them to understand "which facts came from which direction, and whether a new direction can be synthesized".
   - **Negative / doubtful observations** (in recent_facts, e.g. "port closed" / "not injectable") are the worker's observations, not conclusions: before trusting one, call node_detail(id) and look at its evidence -- only when the evidence is solid, confidence=observed, and the means are exhausted do you treat that direction as temporarily sealed off; if the evidence is missing, it is merely "looks like" / "probed only once", or confidence=inferred, treat it as [not yet established], and if it is within scope and no other intent covers it, by default dispatch one review intent to confirm or overturn it (**review the same negative direction at most once**; if it is still negative after the review and the evidence is reasonable, respect that conclusion and do not dispatch again).
   - **Call these on demand only when you need more detail**: list_facts (paginated, newest first, default 20, filterable with q, page with before, returns total/has_more), list_findings (all vulnerabilities), node_detail(id) (full evidence/details; lists/recent_facts give only summaries), list_assets (pull: q search, type/company_id/task_id filters, pagination, or direct fetch by id/ids), asset_neighbors. Assets are shared globally; do not pull everything by default.

2. **Judge the goals (core duty)**: the goals field already contains the goals and their status; for an unmet goal already proven by some finding/fact, call prove_goal(goal_id, evidence_id, reason) to mark it met. **When the one you mark is exactly the last unfinished goal, the system automatically judges the whole task complete** -- finishing is driven only by calling prove_goal one by one; there is no other "complete it all in one click" means.
   - ⚠️ **Quantified acceptance check (never stamp early)**: when a goal contains quantifiable conditions (coverage reaches X percent, capture N flags, obtain some privilege), before prove_goal you [must] check the measured values in the graph_overview above (coverage.pct, the findings_total count, etc.): if the bar is not met, you [must not] call prove_goal; dispatch intents to make up the gap instead; never mark met early on the grounds of "mostly achieved / the core is already taken". Example: the requirement is 100 percent coverage but the measured coverage.pct=40 -> not met, keep dispatching supplementary testing intents.

3. **(Optional, opening only, very lightweight) Probing to understand**: only when the graph has almost no facts yet (recent_facts is basically empty, the task has just begun) and the situation alone cannot make the initial intent concrete, use Bash etc. to do a very small amount of read-only probing of the target (e.g. 1-2 curl calls to see the home page/fingerprint). **The only legitimate output is one sentence of a more precise intent description** -- never the discovery/verification/exploitation of a vulnerability, nor enumeration results for endpoints/directories/parameters (those are the worker's job; write them as intents and dispatch them). Three hard boundaries:
   - If the graph already has facts produced by workers (facts>0 / recent_facts non-empty) -> probing yourself again is [forbidden]; base every judgment on the existing facts, and this round's output can only be "dispatch new intents" or "finish"; to dig into a lead, dispatch an intent for a worker to investigate, do not curl it yourself.
   - Even at the opening, probe at most <=3 times and then stop; the probing is only to make the initial intent clear; as soon as you notice you are "investigating in depth" rather than "quickly setting a direction" (enumerating endpoints/directories one by one, trying ids one by one, decoding chains, repeatedly probing the same interface, any test or verification of an injection / privilege bypass / vulnerability -- all of this is heavy worker work), stop immediately and write it as an intent.
   - Whatever can be judged from the existing facts/situation needs no probing at all.

4. **Decide which new directions to add**: **the "restraint" here means only [do not duplicate existing intents], not "dispatch as few as possible"** -- when the goal is not met, the default question is "what deeper, harder, not-yet-covered approaches are there to get closer to the goal", not "can we wrap up". An intent is an [open exploration direction] (not a fixed type/menu); combine the known facts, assets and goals to judge directions yourself, and compare each one against open + running + recent_done:
   - Already covered by an open/running intent -> do not generate (it is being handled).
   - Has appeared in recent_done -> **first look at that intent's state (every entry carries it) to tell how it stopped, then decide**:
     - **done (ran to normal completion)**: already covered -> do not re-dispatch as is; whether it is a dead end is judged from the fact conclusions it yielded, not from the state; re-dispatch only when a [material new mechanism] appears (new facts/assets/parameters/a clearly different approach), and write in the summary how it differs from last time; rewording or "try once more, it might work" does not count, and retrying is forbidden.
     - **exhausted (budget ran out, cut off halfway, only partial results written back) / blocked (model or network failure, basically no probing got done)**: both mean it did not end properly midway and the information is incomplete -- first use get_worker_trace / get_worker_output to see how far it actually got and where it got stuck, then choose from the following: close to a breakthrough but cut off by the budget -> dispatch "continue from the previous progress"; a purely external failure that kept it from running (blocked is often this) -> re-dispatch the same direction directly; stuck at the same place every time -> change the approach/direction. The basis is always the real progress in the trace, never the state itself.
   - A brand-new direction not covered by any intent -> generate.
   - Every known direction is covered by intents still open/running -> do not generate, just finish (there are running/queued works; wait for them to advance); but if only recent_done coverage remains, there is no open/running intent left and the goal is not met -> by the hard floor at the top you must open another or continue one.
   - **Depth over coverage**: coverage is a lower bound / acceptance item, not the exploration goal itself; after finding a high-value entry point (possibly leading to RCE / privilege escalation / data exfiltration), prefer dispatching intents to [drive that path through in depth], rather than spreading wide to even out coverage and testing every asset shallowly.
   - **Keep routes diverse; do not converge too early**: when the goal is not met, if the existing intents all crowd onto the same route/entry point while an [essentially different] uncovered direction exists (another entry surface / another class of assets / another exploitation chain), prefer adding that divergent direction instead of adding synonymous intents on the same line (look at the substantive difference, not the wording); if that divergent direction is already covered by existing intents, still do not generate. The ideal is 2-3 routes with different mechanisms coexisting (e.g. "attack from the upload chain" and "attack from the authentication bypass"), and only after one of them delivers evidence of [getting closer to the goal] do you concentrate resources there. **But diversity always yields to the [Operation constraints] at the top**: entry surfaces/ports/hosts/operations excluded by a constraint must never get an intent, even if essentially different.

   **Serial exploitation chains: dispatch step by step, do not split them into parallel.** For a strongly dependent serial chain (1->2->3, where each later step depends on the actual output of the previous one): do not dispatch it all at once in parallel (the downstream, lacking a prerequisite that does not exist yet, will only repeat work or spin idle); use TodoWrite to record the whole chain as todos (one per step), dispatch only the step whose prerequisite is already satisfied this round (usually the first step), and after it produces facts, dispatch the next step on the next wake-up (the prompt will carry the todo list) and mark the satisfied ones completed. Do not split "the same thing" into two ("confirm the trigger point" and "trigger the trigger point" are the same step); only [parallel, mutually independent] dimensions (e.g. enumerating several unrelated endpoints) should use multiple intents in parallel.

5. **Submit**: use [one single] add_intent call to submit the filtered new directions in a batch (the intents array, at most the 4 highest-value ones; do not call it repeatedly one by one):
   - **summary**: one sentence of natural language describing the direction (the full address of the test target + what to do + why), no fixed taxonomy; deduplication relies mainly on comparing it with existing intents.
   - **asset_ids**: the ids of the target assets this direction will test/attack (pass them whenever possible, 0/1/many, from list_assets) -- whenever the direction revolves around concrete assets (site/endpoint/parameter/host), be sure to pass them, for coverage deduplication and for linking into the asset chain; pass all of them if it spans several assets; leave empty only for pure global reconnaissance with no concrete asset.
   - **parent_ids**: which upstream nodes this direction was synthesized from (optional, 0/1/many) -- if several facts combine to produce one intent, pass them all; if it derives from some upstream intent/finding, pass its id too; leave empty for a brand-new top-level direction.

Do not duplicate and do not pad; but when the goal is not met and there are uncovered, deeper approaches, dispatch them. Be concise, focused and efficient.`

func plannerSystem(goal, dataDir, workDir string) string {
	body := renderSystem("planner", plannerDefaultTmpl, PlannerVars{Goal: goal, DataDir: dataDir, Now: nowStr()})
	return body + artifactSpec(workDir)
}

// Plan runs one planning round. emit, if non-nil, receives the planner's execution
// steps (so users can see how it reads the situation and judges goals — the
// planner is the intent generator and was previously a black box). Returns whether
// the planner judged the goal met.
// triggers carries the concrete change(s) that fired this round — worker(s) done
// and/or finding(s) reported (may be several — the engine debounces a burst; empty
// for time/heartbeat wakes). They are spelled out at the top of the prompt so the
// planner looks first at the actual change (which intent, its output/finding).
func (p *Planner) Plan(ctx context.Context, taskID int64, as *db.AssetStore, ts *db.ExplorationStore, goal string, triggers []TriggerEvent, emit func(db.Activity)) (met bool, reason string, err error) {
	// cold-digest §2.3/§7: advance this task's planner-round counter, maintain the
	// cold_since_round stamps, and (if a threshold is hit) kick off background
	// compaction. Synchronous part is cheap (a few queries); the LLM compaction
	// runs in a detached goroutine so it never adds latency to this round.
	p.compactor.OnPlannerRound(ctx, ts)
	tsx := NewToolSet(ts, "planner")
	tsx.SetFindingRecorder(p.findingRecorder)
	if as != nil {
		tsx.SetAssetStore(as, as.Companies())
	}
	tsx.SetTaskID(taskID)
	tsx.SetCoverageEnabled(as == nil || as.CoverageEnabled(taskID))
	tsx.killWork = p.killWork   // enable kill_work tool (nil = unavailable)
	tsx.steerWork = p.steerWork // enable steer_work tool (nil = unavailable)
	if origin, _ := ts.OriginFactID(); origin > 0 {
		tsx.SetOwnerNode(origin) // planner-side anchors default to the task root (origin fact)
	}
	// Domain tools + the base default toolset (Read/Write/Edit/MultiEdit/LS/Glob/Grep/Bash).
	// When the asset-coverage feature is off, drop add_task_scope/list_untested_assets (kept out of the prompt).
	base := append(tsx.DropCoverageTools(tsx.PlannerTools()), actool.DefaultTools()...)
	ctx = WithRunInfo(ctx, RunInfo{TaskID: taskID, ExplorationID: explorationID(ts)})
	tools, def, cleanup := AugmentTools(ctx, "planner", base)
	defer cleanup()
	// The key situation (just-finished intents + the prefetched full graph) goes into [this round's user input] (see input below); the system
	// prompt keeps only the static planning body. Moving it out keeps the system prompt stable every round, which helps caching; the cost is that if a single round
	// grows long, the situation may be squeezed by compaction (a planner round is usually short, so the risk is low). situational is spliced into input below.
	situational := renderTriggers(ts, triggers) + renderGraphOverview(tsx.graphOverviewData())
	// Task-level deadline / final mode (injected via ctx, see taskclock.go). On the final round the task-timeout
	// planner wrap-up prompt is spliced into this round's user input (along with situational) as [this round's operating instruction], so that it only does the last
	// goal judgment and produces no new intents.
	tc := taskClockFrom(ctx)
	if tc.Final {
		situational += "\n\n[Task final wrap-up (special instruction for this round; it overrides the regular planning flow above)]: " + resolveTaskTimeoutWrapup("planner")
	}
	// This task's working directory <workDir>/tasks/<taskID>, created up front.
	taskDir := ensureRunDir(p.workDir, taskID, 0)
	ctx = intercept.WithReviewContext(ctx, taskDir, intercept.ReviewBackground{})
	sysBody := plannerSystem(goal, p.workDir, taskDir)
	if p.wantConstraints() {
		sysBody += constraintBlock(ts) // operation constraints (if any) are injected into the system prompt to frame the exploration boundary
	}
	system, boundary := deferredSystem(sysBody, def)
	// The planner has no wall-clock budget of its own; when there is a deadline, clamp MaxDuration to the remainder so that a running planning round
	// enters wrap-up when the task is due (timeout -> task-timeout prompt, turn budget -> per-run prompt).
	maxDur, clamped := clampMaxDuration(tc.DeadlineUnix, 0)
	settle := wrapupSettlement("planner", nil)
	if tc.DeadlineUnix > 0 {
		settle = wrapupSettlementForTask("planner", nil, clamped)
	}
	opts := agentcore.Options{
		Provider:        p.prov,
		SystemPrompt:    system,
		DynamicBoundary: boundary,
		Tools:           tools,
		DeferredTools:   def.Deferred,
		UnlockSet:       def.Unlock,
		PermissionMode:  permission.ModeBypass,
		EnableWebFetch:  true, // goes through the recording proxy (traffic is logged); load the proxy CA to verify HTTPS certificates re-signed by the MITM
		WebFetchProxy:   p.proxyAddr,
		WebFetchCACert:  p.proxyCACert,
		// Web search (optional). ddgs needs no key; brave-free needs BraveKey; tavily needs TavilyKey.
		// WebSearchProxy is a separate egress proxy (http/https/socks5), unrelated to the traffic-recording MITM proxy; empty = direct.
		EnableWebSearch:       p.webSearch.Enabled,
		WebSearchBackend:      p.webSearch.Backend,
		BraveSearchAPIKey:     p.webSearch.BraveKey,
		TavilySearchAPIKey:    p.webSearch.TavilyKey,
		DeepSeekSearchBaseURL: p.webSearch.DeepSeekBaseURL,
		DeepSeekSearchAPIKey:  p.webSearch.DeepSeekAPIKey,
		DeepSeekSearchModel:   p.webSearch.DeepSeekModel,
		WebSearchProxy:        p.webSearch.Proxy,
		BashEnv:               proxyEnv(p.proxyAddr, p.proxyCACert), // Bash subcommands go through the proxy and trust the CA by default
		WorkingDir:            taskDir,                              // this task's working directory <workDir>/tasks/<taskID>
		ToolOutputDir:         cmdOutDir(taskDir),
		MaxTurns:              p.maxTurns, // 0 = unlimited (configurable in agent management)
		MaxDuration:           maxDur,     // 0 = unlimited; with a deadline = time remaining until the deadline
		Compaction:            compactionConfig(p.compactionWindow()),
		// Planning todos shared across wake-ups: keeps a serial chain alive across rounds (the session is new, the store is not).
		Todos: p.todoFor(ts.ID()),
		// On hitting [this round's] turn budget -> the SDK runs wrap-up: land the conclusions already worked out this round (add_intent for what should be dispatched,
		// prove_goal for what can be proven, TodoWrite for serial chains) rather than stop planning -- the planner will be woken again and again afterwards.
		// When clamped (squeezed by the task deadline), PromptByReason is used instead (see wrapupSettlementForTask).
		Settlement:   settle,
		NonStreaming: p.nonStreaming(), // when the profile selects non-streaming, goes through Provider.Complete
		MaxTokens:    p.maxTokens(),    // 0 = no cap sent; the server default decides
	}
	if p.tx != nil { // persist raw LLM conversation; one accumulating file per task's planner
		opts.Transcript = p.tx
		opts.SessionID = fmt.Sprintf("exp%d-planner", ts.ID())
	}
	// Experimental: when enabled, noa takes over context compression (archives are kept together under <workDir>/noa/<SessionID>, persistent).
	noaSession := fmt.Sprintf("exp%d-planner", ts.ID())
	enableNoa(&opts, p.noaEnabledFn, p.workDir, noaSession, noaWarn(noaSession))
	// The situation (just-finished intents + the full graph) is now spliced into this round's user input (see input below). The user input also carries the
	// instruction + the cross-wake-up todos (todos are the model's own planning scratchpad and can be regenerated, so putting them in user is fine).
	// The opening line comes in two kinds depending on whether this round has a concrete change: with a change -> point to the [actual changes] block below; without one
	// (heartbeat patrol / hint / resume, etc.) -> do not falsely claim "the graph changed", and instead prompt it to also review the running intents.
	lead := "A concrete change just happened (see [What actually changed to trigger this round] below); plan the next step accordingly: "
	if len(triggers) == 0 {
		lead = "This round is a wake-up from a **scheduled patrol (heartbeat tick) / no concrete change signal** -- the graph does not necessarily have anything new. Also review the running intents: use steer_work to correct ones that have made no progress for a long time or gone off track, and kill_work to cut losses on ones whose direction is entirely wrong; then judge the goals and decide whether to add directions: "
		// On a heartbeat / no-change wake-up, if the whole graph has no open or running intent -> exploration has stalled (no worker running,
		// no queued direction). Tell the planner explicitly and force it to produce new directions this round, rather than reviewing running intents only and idling for a round.
		if active, err := ts.HasActiveIntent(); err == nil && !active {
			lead = "This round is a wake-up from a **scheduled patrol (heartbeat tick)**, and there is currently **no open or running intent at all** -- no worker is running and no direction is queued; exploration has stalled. You **must** produce one or more new intents this round that advance toward the goal and are **non-duplicates** of the existing intents in the graph (producing 0 intents is not allowed); first judge from the situation below whether the goal has been met, and if not, add directions immediately: "
		}
	}
	input := lead + situational + "\n\nBased on the situation above, judge the goals. When a goal has been [truly achieved] (the target result has been obtained / the target vulnerability has been confirmed), mark them one by one with prove_goal. **Hard floor: whenever the goal is not yet met and there is currently no open or running intent (frontier_open=0 and running_intents is empty), you must produce at least one intent that advances toward the goal this round -- there is no running work to wait for and no queued direction, so producing 0 intents = the task stalls. Only when existing open/running intents are advancing, or the goal is already met, may this round produce no new intents.**" +
		renderPlannerTodos(opts.Todos.List())
	// MaxDuration now interrupts a running tool when the wall-clock deadline hits and enters wrap-up in place (on a live ctx), so a single stuck round no longer
	// bypasses wrap-up and no external hard ctx backstop is needed. ctx carries only pause / kill / shutdown.
	_, _, err = captureRun(ctx, opts, input,
		func(r db.Activity) {
			if emit != nil {
				r.Worker = "planner" // planner activity has no intent_id (it generates them)
				emit(r)
			}
		})
	return tsx.GoalMet, tsx.Reason, err
}
