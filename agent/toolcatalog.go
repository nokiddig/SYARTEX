package agent

import (
	"context"
	"encoding/json"

	actool "github.com/Autumn-27/norma/tool"
)

// This file turns the built-in tools from pure code into a catalog that can be
// enumerated and overridden by the DB:
//   - BuiltinToolSeeds(): expands the built-in tool sets of the execution agents into
//     seed records (key + description + parameter schema + default bound agents), which
//     the server idempotently seeds into the tools table at startup.
//   - ToolResolve hook: at runtime, applies the DB tools rows to the assembled tools:
//     "filter per agent + override description/schema + inject parameter defaults".
//     Key/handler stay in code; the DB only changes the prose and the defaults.
// The handler (Call behaviour) always comes from code - the DB cannot change it, only the
// description the model sees and the default input values.

// ToolSeed is a seedable snapshot of one built-in tool: Key is CoreTool.Name() (tied to
// the handler, read-only in the UI), Desc/Schema come from the tool definition in code,
// and Agents lists which agents the code binds it to by default.
type ToolSeed struct {
	Key    string         // = CoreTool.Name(), primary key, immutable
	Desc   string         // top-level description (can be overridden in the UI)
	Schema map[string]any // parameter JSON-Schema (structure is read-only, description/default can be edited in the UI)
	Agents []string       // default bound agent keys (worker/planner/mainagent)
}

// builtinToolsByAgent builds the domain tool set of every execution agent with a
// read-only shell ToolSet (nil stores). Tool constructors only put closures into the
// Spec and never dereference a store at construction time, so this is nil-safe - the
// tools are only used here to read Name()/Description()/InputSchema(), never Call.
//
// It deliberately does NOT include the generic SDK tools actool.DefaultTools() (Read/
// Write/Edit/MultiEdit/LS/Glob/Grep/Bash): every agent always has them, there is no
// "bind to whom" choice, and most of their documentation lives in Prompt() (this table
// only overrides Description(), which would be a misleading half override). Not seeded
// -> no DB row -> ToolResolve passes them through unchanged, as before. Only artex's own
// domain tools are managed in the table.
func builtinToolsByAgent() map[string][]actool.CoreTool {
	ts := NewToolSet(nil, "")
	return map[string][]actool.CoreTool{
		"mainagent": ts.MainAgentTools(),
		"planner":   ts.PlannerTools(),
		"worker":    ts.WorkerTools(),
		// goals (the goal decomposer) binds set_goals + set_constraints by default: they write
		// the decomposed goals and the extracted operating constraints into the DB. It shares
		// these managed tools with mainagent; the web UI can edit the description/schema and
		// toggle them per agent.
		"goals": {ts.setGoals(), ts.setConstraints()},
		// auto binds finding reporting + asset management tools by default; other domain
		// tools can be ticked in the UI as needed.
		// New databases get this via the seed; old databases are migrated by seedAutoDefaultBindings.
		"auto": {ts.addFinding(), ts.insertAssets(), ts.addCompanyScope(), ts.listAssets(), ts.listCompanies()},
		// pentest (the standalone penetration agent) binds by default: query assets / insert
		// assets / report findings / query findings / query companies.
		// New databases get this via the seed; old databases are migrated by seedPentestDefaultBindings.
		"pentest": {ts.listAssets(), ts.insertAssets(), ts.addFinding(), ts.listFindings(), ts.listCompanies()},
	}
}

// defaultUnbound: these system tools are still listed in the catalog (visible in the web
// UI, can be ticked per agent manually), but are NOT bound to any agent by default -
// ToolResolve drops a tool with an empty binding for every agent, so it needs an explicit
// opt-in. They stay in an agent's base tool set (e.g. goal_met in PlannerTools) for two
// reasons: the seed can construct them to get desc/schema, and once a user binds one back
// manually, the base set contains it so ToolResolve can keep it at runtime.
//
// goal_met: bypasses the per-goal prove_goal and declares the WHOLE task complete globally;
// it carries a lot of weight and a risk of false positives, and duplicates the "prove_goal
// marks the last goal -> automatic wrap-up" flow, so by default it is given to no agent
// and can be bound manually when needed.
var defaultUnbound = map[string]bool{"goal_met": true}

// BuiltinToolSeeds de-duplicates and merges the built-in tool sets of all agents into a
// seed list: tools with the same name (e.g. list_assets, which several agents have) are
// merged into one entry whose Agents is the union; tools in defaultUnbound are forced to
// an empty binding.
func BuiltinToolSeeds() []ToolSeed {
	byAgent := builtinToolsByAgent()
	order := []string{"mainagent", "goals", "planner", "worker", "auto", "pentest"}

	type acc struct {
		tool   actool.CoreTool
		agents []string
	}
	m := map[string]*acc{}
	var keys []string
	for _, ak := range order {
		for _, t := range byAgent[ak] {
			a, ok := m[t.Name()]
			if !ok {
				a = &acc{tool: t}
				m[t.Name()] = a
				keys = append(keys, t.Name())
			}
			a.agents = append(a.agents, ak)
		}
	}

	out := make([]ToolSeed, 0, len(keys))
	for _, k := range keys {
		a := m[k]
		agents := a.agents
		if defaultUnbound[k] {
			agents = []string{} // listed in the catalog and manually bindable, but given to no agent by default (store [] rather than null, consistent with other tools)
		}
		out = append(out, ToolSeed{
			Key:    k,
			Desc:   a.tool.Description(),
			Schema: a.tool.InputSchema(),
			Agents: agents,
		})
	}
	return out
}

// ToolResolve, if set, post-processes an agent's fully-assembled tool list against
// the DB tools table: it drops tools not bound to this agent (or globally disabled)
// and wraps the rest so the model sees the DB-overridden description/schema and
// default input values get injected. Tools with no matching DB row (MCP/skill/host tools like
// traffic) pass through untouched. nil = tools unchanged. Wired in server/assembly.go.
var ToolResolve func(ctx context.Context, agentKey string, tools []actool.CoreTool) []actool.CoreTool

// DecorateTool wraps t so Description()/InputSchema() report the DB overrides and
// Call() injects scalar parameter defaults (from schema's "default" props) whenever
// the model omitted them. Name/Prompt/permission/scheduler flags delegate to t, so
// the tool's identity and handler are unchanged. Empty desc/schema fall back to t's.
func DecorateTool(t actool.CoreTool, desc string, schema map[string]any) actool.CoreTool {
	if desc == "" {
		desc = t.Description()
	}
	if len(schema) == 0 {
		schema = t.InputSchema()
	}
	return &overriddenTool{CoreTool: t, desc: desc, schema: schema}
}

// overriddenTool is a CoreTool decorator: it embeds the original (so all behavioral
// methods — Prompt/IsReadOnly/IsConcurrencySafe/CheckPermissions/Name — delegate)
// and overrides only the model-facing description/schema plus default injection.
type overriddenTool struct {
	actool.CoreTool
	desc   string
	schema map[string]any
}

func (o *overriddenTool) Description() string         { return o.desc }
func (o *overriddenTool) InputSchema() map[string]any { return o.schema }

func (o *overriddenTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	return o.CoreTool.Call(ctx, injectDefaults(in, o.schema), tc)
}

// injectDefaults fills scalar parameter defaults declared in the (possibly edited)
// schema into the input JSON whenever the model omitted the field or left it empty/
// null. Structure (names/types/required) is untouched — only default values are merged in.
func injectDefaults(in json.RawMessage, schema map[string]any) json.RawMessage {
	defs := scalarDefaults(schema)
	if len(defs) == 0 {
		return in
	}
	m := map[string]json.RawMessage{}
	if len(in) > 0 {
		if err := json.Unmarshal(in, &m); err != nil {
			return in // non-object input: don't touch it
		}
	}
	changed := false
	for k, dv := range defs {
		if cur, ok := m[k]; !ok || isEmptyJSON(cur) {
			m[k] = dv
			changed = true
		}
	}
	if !changed {
		return in
	}
	b, err := json.Marshal(m)
	if err != nil {
		return in
	}
	return b
}

// scalarDefaults extracts properties[k]["default"] for scalar params (string/
// integer/number/boolean). Array/object defaults are skipped: merging them is
// ambiguous and not worth the surprise.
func scalarDefaults(schema map[string]any) map[string]json.RawMessage {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	out := map[string]json.RawMessage{}
	for name, raw := range props {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		dv, ok := p["default"]
		if !ok || dv == nil {
			continue
		}
		switch p["type"] {
		case "string", "integer", "number", "boolean":
			if b, err := json.Marshal(dv); err == nil {
				out[name] = b
			}
		}
	}
	return out
}

func isEmptyJSON(raw json.RawMessage) bool {
	s := string(raw)
	return s == "null" || s == `""`
}
