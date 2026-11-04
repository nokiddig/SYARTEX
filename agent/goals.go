package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// goalsDefaultTmpl is the built-in EDITABLE body (section [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You are a penetration-testing goal decomposer. Your job is to identify the **final outcomes to be achieved** from the user's input, not to plan attack steps.

**Step 1 (do this before splitting goals): extract operation constraints**
From the "task goal / task description", identify what the operator explicitly specifies about [which operations may and may not be performed], and register them one by one with set_constraints (if the description and goal involve no operation constraints, you may skip extraction):
- type=deny: forbidden operations (e.g. "do not scan ports", "no write/delete operations against the production environment", "no brute forcing", "do not touch a certain subdomain").
- type=allow: explicitly allowed / restricted operation scope (e.g. "passive reconnaissance only", "only against a certain domain").
- A constraint is not a goal and not an attack step: it specifies the boundary of operating behavior.
- **A constraint must be [self-contained and pinned to concrete targets]**: replace **referring expressions** such as "the current target / current port / current IP / current domain / this site" with the **concrete values** from the task goal/description. Constraints are injected on their own into the execution-stage prompts, and once detached from context a referring expression cannot be resolved.
  Example: if the target is https://abc.example.net -> write "only test abc.example.net" instead of "only test the current target"; "only test target port 443, do not scan other ports" instead of "only test the current port". If the original text only says "the current target" but the target address is already explicit, fill the address in.
- **Register only constraints that the goal/description [explicitly states or stresses]; never invent any**; when unsure of the type, use deny (more conservative).
- If the goal/description really contains no operation constraints, **do not** call set_constraints.
After registering the constraints (if any), proceed with the goal splitting below.

**Goal = a final deliverable / verifiable outcome**

**Not goals (must not be listed as sub-goals)**:
- Information gathering, reconnaissance, endpoint scanning
- Vulnerability analysis and verification process
- Attack steps, exploitation techniques
- Result verification steps

**Splitting principles**:
- The user describes only one final goal -> output one
- There are several **mutually independent** final deliverables -> list them separately
- Annotate vulnclass where a goal maps to a clear vulnerability class; leave it empty for information-gathering / business-logic goals
- Never invent goals the user did not mention

Submit the result by calling set_goals.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

**Extra duty: register the test asset scope**
Besides splitting goals, you must also identify from the "task goal / task description" the **explicitly given test asset scope** and register it with add_task_scope (this task's authorization boundary, and also the denominator of asset-testing coverage). **Minimum-scope principle: register only the one target the user explicitly named, never widen it on your own.**
- Target is a URL or an address with a hostname (e.g. https://xxx.example.com/path, app.example.com) -> take its **full hostname**, kind=subdomain, value=full hostname.
  Example: target https://a1b2c3.lab.example.net/path -> kind=subdomain, value=a1b2c3.lab.example.net (**not** example.net).
  **Never** shrink a hostname that has a subdomain down to its root domain - seeing xxx.example.com and registering the whole example.com widens the scope beyond the user's target and violates the minimum-scope principle.
- Use kind=root_domain, value=example.com only when what the user gave is a **bare root domain with no subdomain at all** (e.g. just writing example.com), or they explicitly say "the whole site / all subdomains / the entire domain".
- Plain IP or network segment -> kind=ip / cidr, value=IP or CIDR.
- **Do not** register company scope (company) - right after the task is created the asset system usually does not have this company yet, so it cannot be registered; company-level scope is left to the later plan stage.
Other rules:
- Register only the scope **explicitly written in the goal/description**; never invent or infer domains/IPs that are not mentioned.
- In reason, briefly state which sentence the basis comes from, for auditing.
- If the goal/description contains no explicit asset scope, **do not** call add_task_scope.
First register the scope (if any) with add_task_scope, then call set_goals to submit the goals.`

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// prov is supplied by the caller (rather than built here from a Config) so goal
// decomposition rides the SAME provider instance as the rest of the engine — it
// shares the rate limiter, gets recorded by llmrec, and participates in LLM
// failover instead of quietly bypassing all three.
//
// desc is the task's free-text description (background: target scope / flag count / engagement notes, etc.).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
//
// as + taskID, when non-nil/positive, wire the add_task_scope tool so the
// decomposer can register the explicit asset scope it extracts from the goal.
//
// ts is the task's exploration store: set_goals writes the decomposed goal nodes
// straight into it (the same managed tool the main agent uses to add goals at
// runtime). The returned specs are read back from the store so callers can emit
// per-goal activity and detect the "LLM produced nothing" case for their fallback.
func DecomposeGoals(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	return DecomposeGoalsWithProvider(ctx, prov, dataDir, goalText, desc, as, ts, taskID, false, 0, emit)
}

// DecomposeGoalsWithProvider is the task-runtime variant used when a task has an
// ordered provider chain. It preserves the same tools and write behavior while
// letting the caller own provider selection/failover. maxTokens is the profile's
// per-reply output cap (0 = send none).
func DecomposeGoalsWithProvider(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, nonStreaming bool, maxTokens int, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	// Goal decomposition is a one-shot call: no transcript store is attached, so agentcore does not
	// attach a session id to ctx (it only does so when there is a writer, see agentcore.Prompt). Yet
	// gateways that do prompt caching / sticky routing keyed on a session-id header (opencode zen answers
	// 400 MissingSessionID without x-opencode-session) read exactly this value from ctx - without it
	// you get "chat works, decomposition gets 400". Attach a stable id explicitly: decomposition
	// requests of one exploration share it (helps cache hits), and the name does not collide with
	// planner/worker, so llmrec.parseSession can attribute it correctly.
	if ts != nil {
		ctx = transcript.WithSessionID(ctx, fmt.Sprintf("exp%d-goals", ts.ID()))
	}
	// worker="goals" tags the goal nodes' provenance; ts/taskID let set_goals link
	// each goal under the task root. This is the catalog's real set_goals tool, so a
	// web-edited description/schema on it applies here too.
	tsx := &ToolSet{as: as, ts: ts, taskID: taskID, worker: "goals"}
	// Description rides in the user message (same channel as the goal), NOT via the
	// {{.EngagementDescription}} template var — else a prompt that references the var
	// would inject the description twice. System prompt stays pure static instructions.
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{DataDir: dataDir, Now: nowStr()})
	// set_constraints is always available (independent of the asset store): the body already contains the
	// step "extract operation constraints before splitting goals" (wording is editable on the agent edit page), so only the tool needs wiring here.
	tools := []actool.CoreTool{tsx.setGoals(), tsx.setConstraints()}
	// Wire add_task_scope only when we have a real asset store + task to write to.
	// The scope-extraction tail is appended in lockstep so the prompt never asks for
	// a tool that isn't present.
	if as != nil && taskID > 0 {
		tools = append(tools, tsx.addTaskScope())
		sys += goalsScopeTail
	}
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background information, may contain target scope / flag count / engagement notes; for reference only, do not invent anything it does not mention):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  tools,
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		// 3 steps (extract constraints -> register scope -> split goals) each need one tool call; leave enough turns so set_goals is not missed before wrap-up.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // when the profile selects non-streaming, goes through Provider.Complete
		MaxTokens:    maxTokens,    // 0 = no cap sent, the server default decides
	}, userMsg, captureEmit)
	// set_goals persisted the goals directly; read them back so the caller sees what
	// was written (empty slice ⇒ the LLM produced nothing ⇒ caller falls back).
	if ts == nil {
		return nil
	}
	nodes, _ := ts.ListByKind(db.KindGoal, 10000)
	var out []GoalSpec
	for _, n := range nodes {
		var p struct {
			Text      string `json:"text"`
			VulnClass string `json:"vulnclass"`
		}
		_ = json.Unmarshal(n.Payload, &p)
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, GoalSpec{Text: p.Text, VulnClass: p.VulnClass})
		}
	}
	return out
}
