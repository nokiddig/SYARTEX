---
name: api-recon
description: Use this skill when collecting a website's API endpoints.
---

# API Recon (frontend API reconnaissance)

Under **authorized** conditions, discover as completely as possible: **backend APIs** (path, method, parameters, response body), **frontend routes**, and **UI feature trigger points** (tabs, dialogs, table actions, etc.).

---

## Boundaries and prohibitions (Agent must read; violations are out of bounds)

This skill **only performs API / parameter-surface reconnaissance**. It is not a vulnerability-hunting or exploitation phase.

### Task boundaries

| Scope | Allowed | Forbidden |
|---|---|---|
| **Goal** | Enumerate path, method, parameters, routes, UI trigger points | SQLi/XSS/privilege escalation/brute force/fuzzing for vulnerabilities, request-tampering attacks, destructive operations |
| **Authentication** | Hook + stub/mock to bypass the **client-side** login gate | Asking the user for or guessing credentials; submitting a real login form |
| **Runtime** | Hook APIs without credentials and use mock responses to let the SPA enter the post-login shell | Flows that depend on a real backend session to continue |

### Credential-free dynamic analysis (Phase 3 default)

1. Use `preload.js` / `runtime_harvest.js` to **intercept and stub** bootstrap APIs such as login, permissions and menus;
2. For business query APIs, return a mock body whose **structure is correct, business code is success, and data may be empty**;
3. Let the frontend render post-login pages even with no backend or in a 401 environment, so that more XHR/fetch/WebSocket requests are triggered;
4. **Empty data, blank tables and placeholder UI are all expected** - do not switch to real login or vulnerability testing because of them.

**In one sentence**: use mocks to prop open frontend routes and component mounts and **record only outbound requests**; what the backend returns does not matter, what matters is **which further APIs** the frontend will call.

### Hard process prohibitions

| Forbidden | Do this instead |
|---|---|
| Before Phase 1 is finished, grep/curl/Read the main entry `index-*.js` to extract API paths | Run `OUTDIR/harvest_static.py` |
| Hand-write scripts such as `extract_apis.py` to replace harvest | Edit `OUTDIR/harvest_static.py` and rerun |
| Repeat the same grep/command after it has failed 2 or more times | Change strategy: read tool_logs, edit harvest, consult reference |
| Skip gates A/B and run the original `scripts/` directly | Copy to OUTDIR and adapt to the target |
| Real username/password, OTP, OAuth or other authentication | stub/mock (see above) |
| Skip stubbing on the pretext of "getting real data" and run privilege-escalation/injection tests | Record only outbound requests; that is the recon boundary |
| Irreversible operations such as deleting, exporting sensitive data or bulk writes | Same applies to coverage clicks |
| Claim all pages and APIs were obtained without completing runtime + dynamic enumeration | See "Definition of done" or note the limitations |
| Claim all parameters are known without completing the parameter trigger matrix + diff | Phase 3b matrix + Phase 5 diff |
| Infer required/optional from a single runtime sample | Multi-sample diff or inference from validation rules/errors |

---

## Two-layer model + runtime modes

| Layer | Output | Limit |
|---|---|---|
| **Static** (JS bundle) | Full endpoint paths, route drafts, payload-assembly field candidates | No HTTP method; parameters require Phase 1b; misses URLs built at runtime |
| **Runtime** (live session) | Method + body + response + dynamic URLs + WS/SSE; multi-sample diff fills in parameters | Requests are only sent when the page actually renders; a single sample cannot determine required/optional |

| Runtime mode | Engine | Suited for |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, METHOD/params/response body, WS/SSE, reproducible batch runs |
| **coverage** | browser + `preload.js` | Click tabs/dialogs/tables for deeper feature coverage |
| **both** | depth first, then coverage | Most complete, slowest |

**Parameter methodology** (no generic script): use harvest/regex for paths; for parameters use **anchor window expansion + UI binding chain + multi-sample diff + error-based inference** (grep recipes are in section J of [reference.md](reference.md)).

---

## Definition of done

Only claim recon is complete when all of the following are met:

- [ ] **Static**: Phase 1 harvest produced `api_static.txt`, `routes.txt`, `js/`
- [ ] **Runtime**: at least one of depth or coverage; coverage/both require **hooks in effect + a dynamic enumeration loop**
- [ ] **Entered the shell**: visiting business paths does not land on `/login` (mind hash routing)
- [ ] **Parameters**: coverage/both completed the parameter trigger matrix + `param_samples.json`; Phase 5 merged `params_merged.json`
- [ ] **Depth** (if module pages are blank): Phase 4 permission tree reconstructed and rerun until **module-level APIs** appear (not just locale/bootstrap)
- [ ] **Delivery**: Phase 5 outputs complete (see the Phase 5 output table); `insert_assets` wrote the services and endpoint assets

---

## Scripts and gates

`scripts/` contains reference templates only; it is **forbidden** to run the originals directly and present the result as final.

**Rule**: read first → adapt to the target → write into `OUTDIR` (e.g. `recon/`) → record in `CHANGES.md`; if a script does not fit, rewrite it following the methodology and borrow only its structure.

| Gate | When | Reference script → OUTDIR copy | Usual must-change items |
|---|---|---|---|
| **A (static)** | After Phase 0, before the **first** run of harvest/spider | `harvest_static.py` / `spider_mpa.py` | **The default regexes run as-is on most sites**; change the endpoint regex, webpack/Vite `publicPath`, MPA exclude/cookie only when the manifest/dialect does not match |
| **B (runtime)** | After Phase 2, before running depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success value, stubs, login regex, api prefix, hash/history |

**Mandatory SPA order** (not interchangeable; Phase numbering takes priority over "explore first, script later"):

| Step | Required | Forbidden |
|---|---|---|
| After Phase 0 | The next Bash = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read the main entry `index-*.js` (usually >500KB) |
| Gate A | Copy script → small edits if needed → **run immediately** | Extract APIs by hand first and then decide whether to harvest |
| Before Phase 1 is finished | Validate output with `wc -l`; on 404 edit harvest and retry | Hand-write extract scripts; grep repeatedly for URLs that were not downloaded |
| From Phase 1b | grep only `OUTDIR/js/*.js` | Use the main bundle in place of harvest |

- ✅ Copy `harvest_static.py` → (optionally) edit regex → **run immediately**
- ❌ curl main bundle → grep many times → write a temporary extract script → harvest last
- **MPA**: after Phase 0 the next Bash = `python3 OUTDIR/spider_mpa.py ...`

---

## Tool and output constraints

| Constraint | Description |
|---|---|
| Large files | `index-*.js` >100KB **must not** be Read/grepped into context; batch-process with OUTDIR scripts |
| grep output | Must use `\| head -20` or `-m 5`; keep only path summaries in the conversation, do not paste bundle snippets |
| Validation | Use `wc -l`, `ls \| wc -l`; do not Read whole directories |
| Initial regex probe | Optional, at most 1 time, only on small chunks <=50KB or HTML; the harvest is authoritative for static results |
| reference | Recipes/templates/troubleshooting are in [reference.md](reference.md); do not inline its full text again |

---

## Execution roadmap

```
Phase 0 classify + OUTDIR
  → Gate A → Phase 1 harvest (★ run immediately ★)
  → Phase 1b parameter reverse engineering
  → Phase 2 three auth gates → config.json
  → Gate B → Phase 3 runtime + parameter matrix
  → Phase 4 permission tree (if needed) → rerun Phase 3
  → Phase 5 merge report + insert_assets bulk-inserts all discovered service and endpoint API assets; never omit any discovered asset when inserting
```

Tick items off in order; **do not enter the next Phase before the previous item is complete**.

1. [ ] **Phase 0**: probe SPA/MPA; create `OUTDIR` → [Phase 0](#phase-0-classification)
2. [ ] **Gate A + Phase 1**: copy scripts → **immediately** harvest → validate with `wc -l` → [Phase 1](#phase-1-static-analysis)
3. [ ] **Phase 1b**: anchor window expansion + binding layer → `param_candidates.json` → [Phase 1b](#phase-1b-parameter-reverse-engineering)
4. [ ] **Phase 2**: three auth gates → `config.json` → [Phase 2](#phase-2-three-auth-gates)
5. [ ] **Gate B**: adapt the runtime scripts → [Phase 3](#phase-3-runtime)
6. [ ] **Phase 3**: depth / coverage / both; confirm entering the shell; parameter trigger matrix → `param_samples.json`
7. [ ] **Phase 4** (if needed): permission tree → patch stubs → rerun Phase 3 → [Phase 4](#phase-4-permission-tree-reconstruction)
8. [ ] **Phase 5**: merge outputs + report + `insert_assets` → [Phase 5](#phase-5-merge-and-report)

---

## Phase 0: Classification

Fetch the entry HTML and **create `OUTDIR`** (do not modify `scripts/` inside the skill):

- **SPA**: empty shell + `<div id=app>` + chunks → Phase 1-5
- **MPA**: SSR + `<form>`, no endpoint bundle → after Gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Outputs `forms.txt`, `links.txt`, `api_inline.txt`. For an SPA, if forms ≈ 0 → switch to Phase 1.

---

## Phase 1: Static analysis

Follow [Scripts and gates](#scripts-and-gates) and [Tool and output constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

harvest: parse the HTML scripts → webpack/Vite manifest → download all lazy chunks → produce `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- Chunk count vs manifest: on 404 edit harvest and retry; do not curl chunks one by one by hand
- If `api_static.txt` is too small → loosen the endpoint regex inside OUTDIR and rerun (see reference)

### Phase 1b: Parameter reverse engineering

Paths come from Phase 1; parameter fields need separate recon. See [Tool and output constraints](#tool-and-output-constraints) for grep rules.

**Completion criterion**: for important APIs you can answer: field name, transport location, inferred type, required or not, sample value, confidence.

#### 1b.0: Transport shape

| Shape | Where the parameters are | What to look at first statically |
|---|---|---|
| REST JSON | body + query | `(params\|data\|body)\s*:\s*\{` next to the path anchor |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Traditional form | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path parameters | `/user/:id` | route table + `useParams` / `$route.params` |
| Encrypted/signed | wrapped into `sign`/`data` | Hook the encryption function's input (reference section D) |

Output: annotate each API with `transport: query|json|form|graphql|encrypted`.

#### 1b.1: Anchor window expansion

Using a known path as the anchor, widen the window to find the payload-assembly object:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper layer | Parameter clues |
|---|---|
| axios instance | `data` / `params` |
| Unified request | Interceptors inject global fields |
| OpenAPI client | Generated method signatures |
| React Query / SWR | Second argument of the hook |
| Vue composable | Composable arguments |

Type remnants: `yup`/`zod`/rules, `Form.Item name=`, embedded Swagger.

→ `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2: Binding layer

```
Form field → onFinish/handleSubmit → transform → API payload
```

| Binding source | Technique |
|---|---|
| Form submit | Follow submit → transform → API |
| Table search | `getFieldsValue()` → `params` |
| Route | `:id` / `?tab=` |
| Interceptor | Global `tenantId`, pagination, sign |
| Enum select | `options` → API enum values |

Trace the payload-assembly function upward from `fetch`/`XHR.send` using the DevTools call stack.

#### 1b.3: The three payload-assembly questions (≠ the Phase 2 three auth gates)

| Question | What to answer |
|---|---|
| **Assembly** | Where the payload is built, traces of transforms |
| **Validation** | required, pattern, enum |
| **Transport** | path / query / body / multipart / headers |

The interceptor gate (Phase 2) also reads the globally injected fields (Authorization, `X-Tenant-Id`, sign).

#### 1b.4: Connection to Phase 3

Candidate fields come from the static/binding layers; **required/optional/conditional dependencies** must come from the Phase 3 parameter matrix + diff + Phase 5 error-based inference.

---

## Phase 2: Three auth gates

grep in `OUTDIR/js/` (with `head`) and write to `config.json` (recipes are in reference):

| Gate | Question | Keywords |
|---|---|---|
| **Render gate** | How does the app decide the user is logged in? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor gate** | What triggers a redirect to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content gate** | Where do menus/permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Do not treat localStorage key names as credentials; confirm them from the chunk/request chain.

**Exit = Gate B**: write the conclusions to `config.json` and edit `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b: API observation (optional)

Use `preload.js` in OUTDIR to confirm session key names, Authorization, and nested API URLs:

| Config | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | header observation |
| `extractUrlsFromResponse: true` | sub-APIs inside responses |
| `observe.storageReads/cookieReads: true` | backfill config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

Export each coverage round: `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, `__API_RECON_OBSERVE__`.

---

## Phase 3: Runtime

Gate B must already be passed; follow [Boundaries and prohibitions](#boundaries-and-prohibitions-agent-must-read-violations-are-out-of-bounds) and the credential-free mock strategy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json` (template in reference).

### Hook and stub (shared by depth + coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 exact | auth/permission/bootstrap stubs | Pass first-screen authentication |
| L2 negative fix-up | all JSON responses | Not-logged-in code → success |
| L3 fallback | `/api` etc. not matched by L1 | Empty success body to prop up the UI |

- **depth**: fake auth + `forward` rewrites the business code + `stubs`; iterate over `routes` (hash/history); produces `runtime_api.json`
- **coverage**: inject `preload.js` at **document-start** (CDP `addScriptToEvaluateOnNewDocument` or Userscript)

Verify: `window.__API_RECON_PRELOAD__` exists; business paths do not bounce to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b: Coverage dynamic enumeration (required)

1. Main navigation/sidebar - click every item, wait 1-3s for network activity
2. Tabs - `role=tab`, `.ant-tabs-tab`
3. Tables - view/edit/details of the first row
4. Toolbar - export, filter, create (**avoid irreversible deletes**)
5. On entering each module - merge APIs/routes
6. SPA - controlled `pushState` for paths not covered by `routes.txt` (forbidden for MPA)

**Parameter trigger matrix** (required): for each module record one sample per operation type and **diff the multiple samples**:

| Operation | Parameters usually added |
|---|---|
| First list load | Pagination + default filters |
| Click search | keyword, filter |
| Advanced filter | More optional fields |
| Create/edit | Full entity |
| Batch/export/sort | `ids[]`, `exportType`, `sortField` |

**Under stubs the outbound body/headers are still real** - the request is the source of truth. Recordings → `scan_raw.json`, `param_samples.json`, `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` + document-start preload
- **React**: `routes.txt` + sidebar clicks + `pushState`
- **both**: 3a depth first, then 3b coverage

---

## Phase 4: Permission tree reconstruction

**Trigger**: module pages are blank / every route shows only bootstrap (e.g. locale) → the content gate has not been passed.

| Symptom | Meaning |
|---|---|
| Entered the shell successfully | Render gate + interceptor gate passed |
| Sidebar items missing / click shows blank | stub shape or permission codes incomplete |
| Every route has the same, very few APIs | `v-if permission` did not pass |
| `routes.txt` far smaller than the bundle | Must be completed from the auth module |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) → `getResultTree` → `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

Stub checks: the outer `response_code` matches the interceptor gate; flat codes align with the tree; `routes` cover every link in `route_map`.

After updating `config.json`, **rerun Phase 3**. For large SPAs you can tune `waitUntil`, `routeTimeout`, `perRouteMs` (see reference sections A3/I).

---

## Phase 5: Merge and report

### Output table

| File | Phase | Content |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | Static bundles and paths |
| `param_candidates.json` | 1b | Static parameter field candidates |
| `config.json` | 2 | Three gates + runtime config |
| `runtime_api.json` | 3a | Detailed depth recording (including WS/SSE) |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | Multiple samples, click log, detail |
| `route_map.json` etc. | 4 | Permission tree intermediate files (if executed) |
| `params_merged.json` | 5 | Merged parameter fields + confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | Routes, APIs, params, feature trigger points, limitations |
| **insert_assets** | 5 | Write all service and endpoint assets into the asset store |

### 5b: Parameter merge

Diff from `param_samples.json`; **there is no generic merge script**. Confidence rules are in reference J7 (high/medium/low/pending trigger).

### 5c: Error-based inference

Within the authorized scope you may send incomplete requests to read 400s (**this is parameter recon, not vulnerability testing**): `field 'x' is required`, enum errors, etc. Watch for `data` wrappers, `variables`, and the pre-encryption `bizData`.

The report must state: runtimeMode, static/runtime API counts, parameter confidence, uncovered modules, and a summary of `CHANGES.md` relative to the reference scripts.

Suggested structure for `site_map.json`:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

For more fields and grep recipes see [reference.md](reference.md).

---

## General notes

- **Framework-agnostic**: webpack/Vite/Angular lazy loading works the same way
- **Transport**: REST/JSON, GraphQL, WebSocket, SSE; gRPC-web is out of scope
- **SSR**: client-side fetches can be recorded; RSC/Server Actions cannot be fully enumerated
- **Blind spots**: JSVMP, WASM, strong HMAC/mTLS validation → static + note the limitation
- **Parameter blind spots**: conditional linkage, hidden params, WASM payload assembly → "pending trigger" / "unreachable"
- **Static is the safety net**: when runtime is blocked, static can still enumerate endpoints

---

## Additional resources

- Grep recipes, `config.json` template, troubleshooting, hooks, parameter reverse engineering section J, site_map template: **[reference.md](reference.md)**
- For reference script paths see the [Scripts and gates](#scripts-and-gates) table
