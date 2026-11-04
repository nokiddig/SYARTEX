# Changelog

Notable changes to this project are recorded in this file. The format follows [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/).

## [Unreleased]

## [0.3.15] - 2026-10-07

### Intercept

#### Added

- **Added built-in intercept rule "Delete-style API paths"**: the existing built-in destructive HTTP rule only recognized the DELETE **method**, but most applications expose delete endpoints that can be triggered with GET/POST, so calls such as `curl 'http://t/api/user/delete?id=1'` would really delete the target's data. A `deny` rule now covers `/delete /del /remove /unlink /erase /destroy` (the verb must be followed by a delimiter, so `/delivery`, `/details` and `/delta` are not blocked by mistake). Existing instances receive it after upgrading, and it can be disabled or deleted under "System -> Command intercept".
- **Token usage of the model fallback approval can be viewed separately**: approval calls previously had no metering attribution of their own, so how much they cost was guesswork. They are now metered separately, and a usage card appears below the "Model fallback approval" switch under "System -> Command intercept": cumulative stats for call count, input, output, and cache reads/writes, plus a mini bar chart of daily consumption over the last 30 days.

### Finding notifications

#### Added

- **Findings can be pushed to IM**: a new "System -> Notifications" page pushes scan findings to six kinds of channel: **DingTalk, Feishu, WeCom, generic Webhook, Telegram and email**. Any number of instances can be configured per type, and each instance has its own enable switch, rate limit and filter rules.
- **Two delivery modes, real-time and digest**: in real-time mode every match is sent immediately, one message each; in digest mode matches are merged into a single message every global period (30 minutes by default), with the number of new findings and the severity distribution at the top. To get "high severity in real time, everything else as a digest", create two channels and configure them separately.
- **Filter rules support four dimensions**: minimum severity, restriction to tasks / asset scope, inclusion and exclusion of vulnerability-type keywords (exclusion takes precedence), and whether to receive disposition status changes (off by default). Messages carry a "View details" button whose target address comes from the global "Callback base URL" setting; if it is left empty, no button is added.
- **Delivery history and manual resend**: lists the status, attempt count, failure reason and owning channel of every delivery, and can be filtered by channel and status; failed items can be resent in one click (a resend resets the retry counter).
- **Masked echo of channel credentials**: webhook URLs, signing secrets, bot tokens, SMTP passwords and the like are echoed only as masked values with a tail hint; submitting the value back unchanged means "do not modify", and clearing the input deletes the field.

#### Fixed

> This section records the conclusions of a security audit performed after the first version of the feature was complete; every item was reproduced first, then fixed, then covered by a regression test.

- **Fixed the mask being bypassable by "change the target address, keep the credentials" (critical)**: config merging kept the stored value for every "unmentioned key", so changing only the address and saying nothing about the credentials made the server send the real stored credentials to an arbitrary address, completely silently. It is now required that once the target address changes, every credential field must be stated explicitly: give a new value, or leave it explicitly empty to mean it is no longer needed.
- **Fixed markdown-based channels doing no escaping of untrusted content**: finding titles and summaries come from model output and asset URLs come from scan results, so a crafted title would render as a clickable external link in DingTalk / WeCom / Feishu, and image syntax would be fetched by the client at render time, leaking the reader's IP. Content is now flattened to a single line and markdown metacharacters are escaped, and the escaping happens at each channel's rendering exit rather than in the shared title function.
- **Fixed the SSRF surface of delivery addresses**: loopback, intranet and cloud metadata addresses were previously all deliverable, and the first 200 bytes of a failure response body went into `last_error` and were echoed by the delivery history, forming a semi-blind read primitive. The defense is now applied at the **dial stage**, which covers DNS rebinding, and cross-host redirects are rejected; loopback and link-local addresses require an explicit `ARTEX_NOTIFY_ALLOW_LOCAL=1`, while RFC1918 private networks are still allowed (self-hosted SMTP relays on an intranet are common).
- **Fixed push credentials leaking through error messages**: DingTalk's `access_token`, WeCom's `key` and Telegram's bot token all live in the URL, and when a delivery failed the full URL flowed into `last_error`, the delivery history, server logs and frontend messages. They are now redacted everywhere, keeping only `scheme://host` and the underlying cause.
- **Fixed silent loss when a digest message was truncated but the whole batch was marked delivered**: truncated-off findings were neither in the message nor in the failure list, yet the delivery history showed success. Messages are now packed per **whole item**, only the ones that fit into this message are marked, and the rest return to the queue to continue in the next message without consuming a retry; the message header also states how many items will continue in the next message.
- **Fixed a typo in the minimum severity threshold silently disabling the filter**: when `min_severity` was written as a typo such as `hgih`, the unknown level's ordinal was 0 and the check degraded to always true, so users thought they received only high severity while in fact every finding was flooding the group. The value is now validated on the write path and the allowed values are listed.
- **Fixed `rate_per_min = 0` (no rate limit) being unreachable**: the write-to-database layer quietly changed an explicit 0 to the default, so operators thought the limit was lifted while they were still throttled. The default is now filled at the API layer when the field is absent.
- **Fixed digest channels bypassing the token bucket entirely**: `rate_per_min` previously had no effect in digest mode. The number of items claimed is now bounded by both this round's quota and an in-memory upper limit.
- **Fixed failure handling that judged by the maximum attempt count of the whole batch, so old deliveries dragged new ones down with them**: an old delivery that had already been retried twice pulled brand-new deliveries in the same batch into failed, and new findings were lost permanently without a single retry. It is now decided item by item.
- **Fixed deliveries with an unparseable snapshot in a digest batch being silently marked successful**: such entries are skipped in the rendering stage but were counted as successful together with the whole batch. They are now explicitly judged failed with a reason given.
- **Fixed the number of deliveries per channel per round possibly exceeding the lease duration**: in multi-instance deployments, rows whose lease expired would be claimed again, sent twice and have their attempt count incremented twice. The per-round cap is now derived backwards from "lease / single timeout", and an assertion pins down the relationship between these constants.
- **Fixed Telegram truncation possibly cutting through an HTML entity**: truncation avoided only half-cut tags but not entity fragments such as `&amp`, which could make the parser reject the **whole** message. It now avoids both.
- **Fixed the email channel treating a transient SMTP failure as permanent**: 4xx replies such as greylisting `450` are temporary rejections that should be retried later, but they were always judged dead, so on a server with greylisting enabled every push failed after the first attempt. The first digit of the reply code is now used to distinguish them: 4xx is retryable and 5xx is a permanent failure.
- **Fixed submitting a nested structure actually writing the mask literal into the database**: object fields such as `webhook.headers` can only be masked or submitted as a whole, and a mask sentinel placed inside the object was stored as a real value, silently breaking authentication. Such a submission is now explicitly rejected.

#### Design notes

- **The transaction that writes a finding does only one blind INSERT**: `notification_events` and the finding are written in the same transaction, so committing guarantees the two are atomically consistent. This INSERT deliberately does not read the channel table or run the user's filter rules, and is wrapped in a `SAVEPOINT`; otherwise a single misconfigured filter condition could abort the transaction and keep a high-severity finding from being saved.
- **Delivery uses leases rather than long transactions**: after claiming with `FOR UPDATE SKIP LOCKED`, the row is set to `sending` and `next_attempt_at` is pushed into the future to act as the lease, so no database lock is held during delivery; rows left behind by a crashed process are claimed again after the lease expires, which self-heals without retrying forever.
- **Rate limiting does not consume the retry budget**: the quota for this round is computed from the token bucket first, and only that many items are then claimed. The reverse order would make deliveries held back by rate limiting count an attempt for nothing, and the three-attempt budget would be used up by pure waiting and end in failure.

### Test infrastructure

#### Fixed

- **Fixed the cleanup order of the company ICP ownership test leaving assets behind permanently**: `defer d.Close()` runs first when the function returns and `t.Cleanup` after it, so all the cleanup statements ran on an already closed connection, the errors were discarded, and the test assets and company stayed in the database forever, even making other tests that assert on asset counts fail inexplicably. Closing the connection now also goes through `t.Cleanup`, registered first, and cleanup failures are made visible.

### Traffic

#### Added

- **The traffic list supports content search, path / status code / length filters and column sorting**: the filter row can filter by **response content** (backed by the full-text index), **path**, **status code** (an exact value or a bucket such as `2xx`) and **response length range**; the table header can sort by time, status code and response length, and the sort preference is remembered locally. Two indexes were added and take effect automatically on existing instances after upgrading; the Agent tool's query contract is unchanged.
- **The traffic list gains "Clear all"**: deletes all traffic records at once (ignoring the current filters) and cleans up historical host directories that the index no longer records; after clearing it also runs one full compaction and reports how much space was actually freed. Traffic evidence already bound to findings lives in a separate evidence store and is not affected.

#### Fixed

- **Fixed disk space not being released after deleting traffic**: the index database was created without `auto_vacuum`, so the file never shrank; the full-text index also uses `contentless_delete`, where `DELETE` only writes a tombstone and does not reclaim the original postings, so deleting traffic actually made the index larger (measured: capturing 6MB of bodies -> 16MB index, still 16MB after deleting everything). Newly created index databases now enable `auto_vacuum=incremental`, and after each deletion full-text index merging and incremental reclamation run in the background in chunks, returning space to the system step by step (the same scenario drops back to 104KB); reclamation releases the write lock between chunks, so it does not block traffic recording.

> Upgrade note: `auto_vacuum` can only be set when the database is created, so **the index databases of existing instances remain in the old mode** and incremental reclamation is a no-op on them (a hint line is printed at startup). On such databases, tombstone growth has stopped after the upgrade, and the space already occupied can be reclaimed once with "Clear all" - that step converts the database to incremental reclamation mode, after which routine deletions take effect on their own.

### LLM

#### Fixed

- **Fixed custom session headers not taking effect on the "one-shot LLM call" paths**: goal decomposition (round 0) and cold-node compression are both one-shot calls that do not attach a transcript store, so there is no session id on the context and the gateway therefore cannot read the header; for endpoints such as opencode zen that return 400 outright when the header is missing, this showed up as "round 0 fails while later planner rounds work perfectly". These two paths now explicitly attach a session id that is stable per exploration, which also lets token usage be attributed correctly to the corresponding exploration (it was not recorded at all before).

### Tools

#### Fixed

- **Fixed output truncation of MCP and custom tools not taking effect, so large results flooded the context in full**: MCP tool returns previously went through no truncation at all, and HTTP-type custom tools, because an empty tool context was passed in, always used a hard-coded 6000 characters instead of the session-configured limit. Both paths now go through `actool.Capture`: output is truncated at the session's `MaxOutputChars` (default 30000), and when `ToolOutputDir` is configured the full output is spilled to disk and the message keeps only the opening fragment and a file pointer.
- **Upgraded norma to v0.4.3**: the harness layer now runs `CaptureOnce` uniformly, so no tool (including ones added in future) can overflow the context because truncation was not wired up.

### Deployment

#### Fixed

- **Fixed the activity stream failing to connect under reverse proxy deployments**: the SSE address was previously built unconditionally as `${hostname}:8787`, so in a deployment proxied to 443 the page loaded from `https://domain/` while SSE was pointed at port 8787, which is not open to the public network, showing up as the activity stream reconnecting repeatedly. It now distinguishes by `NODE_ENV`: production defaults to same-origin, and only `next dev` keeps `:8787` to bypass Next.js buffering of SSE; the `NEXT_PUBLIC_SSE_BASE` override is kept. The README gains reverse proxy deployment notes.

### Discovery

#### Fixed

- **Fixed the "By asset" view having no scrollbar when the asset list overflows**: the outer card only had a `max-height` and no definite height, so the percentage height of the scroll viewport could not be resolved, and with many assets the list either burst out of the card or was cut off and could not be scrolled. The height cap is now applied directly to the asset tree's native scroll container and adapts to the window height.

### Database

#### Fixed

- **Set an upper limit on the connection pool**: `database/sql` by default does not limit how many connections the application itself may open, and when the pool has no idle connection it opens a new one unconditionally, all the way up to PostgreSQL's `max_connections` (100 by default) before being refused. It is now capped at 32; excess queries wait for an idle connection, which under the same load shows up as slowness instead of errors; if `max_connections` has been lowered, lower this accordingly.
- **Improved connection reuse**: the default value of `MaxIdleConns` is 2, so connections beyond 2 concurrent ones were closed after use and the next request had to redo the TCP and PostgreSQL authentication handshakes. The idle cap is now raised to equal the total cap, and a 30-minute connection lifetime and 5-minute idle eviction are added so the pool can naturally replace long-lived bad connections.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.14] - 2026-09-24

### Task list

#### Added

- **The task list gains a "Findings" column**: shows counts per severity tier as Crit / High / Med / Low, with non-zero tiers colored by severity, so the vulnerability scale and distribution of each task is clear at a glance.

#### Changed

- **The "Description / Goal" columns are narrower**: overlong content is shown truncated with an ellipsis and the full text is visible on hover, so long text takes up less horizontal space in the list.

### Exploration graph and planning situation overview

#### Changed

- **Slimmed down the planning situation overview (graph_overview) and set count limits on each list**: recent facts, finished intents, pending intents, cold-zone summaries and confirmed finding details all became "latest window + count fallback", and whatever is not shown can be queried on demand; this significantly reduces the LLM context size per round and keeps long tasks from bloating the context (the cold zone of the related-task overview is throttled as well).

#### Fixed

- **Removed the reverse duplicate edges between digest and finding in the exploration graph after folding cold nodes**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.13] - 2026-09-19

### Asset interception

#### Added

- **Added global asset interception rules (blocklist)**: supports entering domains / IPs / URLs with exact and fuzzy matching, as well as CIDR ranges, with create, read, update and delete of rules and enable/disable; the management page is at "System -> Asset Interception". Fuzzy blocking of government (`.gov` / `.gov.cn`) and education (`.edu` / `.edu.cn`) sites is built in by default.
- **Asset interception is wired into the execution chain**: before dispatching an intent (`add_intent`) and inserting assets (`insert_assets`), the Agent first runs an interception check on the target assets - intents that hit the blocklist are not dispatched, assets that hit the blocklist are not inserted, and the asset information and block reason are returned to the Agent.
- **Added task-level block / allow (allowlist) rules**: independent of the global rules and effective only for this task; the evaluation order is "block first, then allow" - a block match means forbidden; if no block matched but this task has allow rules configured and none of them match, the result is "testing not allowed"; when no allow rules are configured, the allowlist is not enabled. They can be entered when creating a task, and can also be added, edited, deleted and enabled/disabled in the task detail "Overview".
- **Task templates support a preset category and task-level block/allow rules**: a template can save a task category and a set of task-level rules, which are brought into the new task form when the template is applied.

### Action review

#### Fixed

- **Tightened the model judge output protocol to reduce wrong allows caused by truncation**: the limit of the verdict explanation (comment) was lowered from 500 Chinese characters to 120 Chinese characters, and "output JSON only, with no preamble or code block" is now enforced, preventing a verdict from being truncated by `MaxTokens`, becoming unparseable and then being allowed under the model failure policy (fail-open).

### Task archives

#### Fixed

- **Archiving now skips symbolic links instead of failing the whole package**: the archive format supports only regular files and directories end to end, and previously any symbolic link appearing in the working directory made the whole task archive fail; the symbolic link is now skipped and logged, and the remaining files are archived normally (links are not followed and the directory tree is never left).

### Account and compliance

#### Added

- **Added a "Terms of use and disclaimer" dialog before login**: login is possible only after ticking the agreement checkbox.

### License and dependencies

#### Changed

- **The project adopts the AGPL-3.0 open-source license**, and the license and disclaimer notes in the README were improved.
- **Upgraded norma to v0.4.1**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.12] - 2026-09-17

### Exploration trail broadcast board

#### Added

- **The task detail page gains an "Exploration trail broadcast board"** (#144): presents the task's exploration nodes (start / goal / intent / fact / finding / hint / compression) as a timeline feed, with filtering by type, keyword search, ascending/descending order, pagination and auto refresh; when on the "first page, newest first" it is the live position and refreshes every round, and when you leave that position it only accumulates an unread count without disturbing what you are reading, and "N new broadcasts · back to latest" returns in one click. Items are grouped by day, so even when paging through a long task you can tell which day something happened.
- **The broadcast board shows the node id on every row** (#145), making it easier to cross-reference the exploration trail graph and locate a specific node.
- **Expanded broadcast node details show upstream, downstream and anchored assets** (#147): expanding a broadcast item shows the node's upstream (where it came from) / downstream (what it produced) relations, hovering a related entry pops up the corresponding node's card (type / status / source / time / summary / payload excerpt); it also lists the assets anchored to the node (type label + recognizable text). The related data is delivered together with the broadcast page, so expanding sends no additional request.
- **Broadcast board search can filter by node id** (#150): besides "content / source", the search box now matches node ids, and entering a plain number or the "#41" form shown in the UI locates the corresponding node exactly.

### Intent management

#### Added

- **Intent deletion supports two modes, "soft delete / hard delete"** (#149): pending, running and paused intents can all be deleted, and a reason must be provided; the deletion mode is chosen in the confirmation dialog.
  - **Soft delete (default)**: the intent is marked "deleted" and the reason is recorded in a separate field, keeping the intent node and all its outputs and lineage.
  - **Hard delete**: physically removes the intent, together with the exclusive descendant nodes "supported only by it" (cascading along the output / intent chain down to the leaves) so that no orphaned data is left; the token metering of the deleted intent is kept, archived under its original date; shared nodes (still referenced by other intents), goals and task-root facts are always kept, and the hard delete dialog shows the estimated number of nodes affected by the cascade.

  Both modes notify the planner that "the intent was deleted by the user + reason" and replan accordingly.

### Action review

#### Added

- **Approval records can be filtered by status and decision source** (#139): approval records can be filtered by approval status and decision source to quickly locate the target record.

#### Fixed

- **Pending approval requests load independently of history pagination** (#133): pending requests are no longer affected by history list pagination and remain fully visible while browsing history.

### Agent

#### Changed

- **The Worker role description was reworded as a generic cybersecurity platform description** (#138).

#### Fixed

- **Fixed cold node compression never triggering**: the per-task planner now has a Compactor wired in; cold node compression (cold-digest) never ran before because it was not connected, and has now been restored.

### Chat

#### Added

- **Chat supports @ references to records of multiple types, with scrolling pagination** (#135): records of many types can be referenced with @ in chat, and the reference candidates load with scrolling pagination.

#### Fixed

- **Long message bubbles are constrained within the conversation panel** (#137): overly long message bubbles no longer overflow the conversation area.
- **Fixed uploading a file before a new chat was created failing with "missing upload file"**: the Composer's file selection now snapshots the `FileList` into an array before clearing the input and then invokes the callback; previously, in the draft state a conversation had to be created asynchronously first, and by the time execution resumed the `FileList` live-bound to the input had been cleared, so the upload lacked the file field and the backend returned 400.

### Traffic

#### Fixed

- **The traffic recording proxy listens only on 127.0.0.1 by default** (#129, #130): this avoids exposing an open proxy on other network interfaces under the default configuration.

### Web

#### Fixed

- **Fixed the static-export task list page losing the global header**.
- **Filled in the mock of related traffic for the demo finding detail page, fixing a blank screen**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)
- [@dingpotian](https://github.com/dingpotian)

## [0.3.11] - 2026-09-15

### Action review

#### Added

- **Approval records support source navigation** (#125): clicking an approval record's "Source" jumps back to the exact tool execution that triggered that approval - it opens the original full conversation, automatically pages in to the target position, expands the command and result in place and highlights it centered in the scroll area, keeping the surrounding messages readable; auto-correction stops when the user scrolls manually. Across regular chats, task Workers, planners and main Agent segmented sessions, positioning is exact via the persisted `tool_use_id` and the task mapping (a scoped call ID index was added); when the link is missing, duplicated or ambiguous, an explicit notice is shown instead of jumping to some other execution. When the conversation was deleted, the task archived or the record is missing, a clear notice is given.
- **Approval records support pagination** (#115).

#### Changed

- **Slimmed down the model review input** (#124, #125): the input of the model review (the LLM judge after no rule matched) is narrowed to a versioned JSON of "the one complete tool call at hand + an explicitly selected short background + the local working directory". The background takes only genuine user messages (the current user message of the chat / task main Agent); Workers no longer carry an intent summary and the background inherited from the parent Agent is cleared too; planner and auto-triggered sessions do not fabricate user messages. Task description, goals, operation constraints, global exploration situation, historical calls and the Worker's complete intent are no longer attached - these are still used as before for Agent execution and independent session audit, they just do not enter the action review. Every verdict is required to output JSON `decision`/`comment`, and the explanation must contain "actual operation, consequence if successful, matched rule"; instructions inside parameters or background cannot change the review policy. The input snapshot actually sent to the model is saved together with a fingerprint; old snapshots are kept and labeled with their version, and old inputs are not fabricated from current data.

#### Fixed

- **A model verdict wrapped in a code block is no longer silently allowed** (#126): if the verdict JSON returned by the model was wrapped in a ``` code block, the parse failure previously led to a silent allow through the model failure policy; the code fence is now stripped before parsing, and only if parsing still fails is the configured failure policy applied.

### Agent

#### Added

- **Added experimental noa context compression** (norma upgraded to v0.4.0): an experimental feature that can be turned on in system settings, off by default. When on, noa (model-driven context compression) takes over context compression for the four kinds of Agent - main Agent, planner, Worker and chat - replacing the built-in compression; the original compressed text is archived persistently, concentrated under `<workDir>/noa/<sessionID>/` (not scattered across task directories), in a globally unique directory per session ID. If integration fails it automatically falls back to the built-in compression without interrupting real tasks; the switch is read once per run, so toggling only affects runs started afterwards.

#### Fixed

- **The exploration graph tools reject instead of crashing on nil when there is no task context**: calling exploration-graph tools outside a task context now returns a clear error instead of crashing on a nil store dereference.

### MCP

#### Added

- **Supports legacy SSE MCP servers** (#117): compatible with MCP servers that only offer the old-style SSE transport.

### Traffic

#### Fixed

- **Traffic search matches record hosts port-aware** (#114): host matching in `traffic_search` now includes the port, so records of the same host on different ports no longer interfere with each other.
- **An error in the `traffic_search` description migration no longer interrupts later reporter migrations**: a single failing migration is isolated and does not affect the execution of later migrations.

### Web

#### Changed

- **Added a divider between task retest finding options** (#122): a separator was added between the retest finding choices for clearer visuals.

#### Fixed

- **Fixed the whole demo task detail page crashing**: in mock mode the "Sessions" tab of the task detail fetches `GET /api/tasks/<id>/side-questions` (side-question history), but the mock handler had no such route, and the read fallback, treating "a path ending in s is a collection", returned `[]`, so `data.items` was `undefined` and the side hook's `merge()` threw `TypeError: t is not iterable` when iterating it; that exception happens inside the `setItems` updater, is deferred by React to the render phase and rethrown, the caller's `catch` cannot catch it, and the whole page was taken over by the error boundary showing "This page couldn't load". The mock handler now explicitly returns an empty side history, and `sideAPI.history` also normalizes the return value (anything where `items` is not an array is coerced to `[]`) as defense in depth.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.10] - 2026-09-13

### Network

#### Added

- **Added DeepSeek official source to web search**: it directly reuses the currently active LLM configuration. It differs in nature from the other three sources - DeepSeek has no directly callable search API, and search exists only inside its Anthropic-compatible API (the `web_search_20250305` server-side tool), executed on the DeepSeek server. It therefore **supports only the official DeepSeek endpoint + the anthropic protocol** (OpenAI-protocol endpoints reject server-side tools outright), and each search **consumes one extra model call**, the request **does not go through the search egress proxy**, is **not recorded in traffic logging**, and the results contain **only titles and links** (no summaries; when body text is needed, WebFetch fetches it). The settings page explains these limitations but **does not validate or block**; whether they are met is for the user to confirm, and the "Test search" button can run it once to verify.

### Agent

#### Added

- **Workers gain cross-work lookback**: added `search_all_worker_traces` (no need to know the intent_id first; searches by keyword globally across the execution steps of all works in this task for matching steps) and `get_worker_trace` (after locking onto one work, lists the step stream, searches in place by keyword, and fetches the full content by step_id), used to reuse observations that other works saw but did not write into a fact, and to avoid duplicated effort.
- **`node_detail` is delegated down to workers**: together with the lookback tools above, once a worker has an intent_id / node id it can directly query that node's full details.
- **Planning rounds triggered by `add_hint` are now explicitly announced**: previously a newly added hint was only folded into the situation overview for the planner to discover on its own; now a single `add_hint` records a trigger for the planner ("a human added N strategic hints: ..."; a batch counts as one, with no per-item flooding), and the planner is explicitly told "this round was triggered by newly added hints" and sees the hint content directly.

#### Changed

- **The global situation hint was changed to a "loosened" tone**: it encourages divergent exploration and timely reporting of cross-intent leads rather than converging too early.
- **Slimmed down the worker boundary wording**: being blocked at first does not mean something has been fully probed; the focus is "finish all bypass techniques within this intent before drawing a conclusion".
- `insert_assets` drops the per-asset `related` input: its only purpose was to decide whether to add the asset to this task's scope, and the value was not stored (it was voided when the same asset was registered again, and the UI could not show which ones had been judged unrelated), so in practice it only made the model do an extra judgment that could not be kept.
- **The test scope (`task_scope`) is no longer affected by the asset coverage switch**: the automatic scoping of `insert_assets` (`source='auto'`) runs whether coverage is on or off, and `add_task_scope` is always offered to the planner / in-task main Agent / goal decomposition Agent. The scope is the task's authorization boundary and the filter baseline for asset queries; the coverage switch only decides whether to use it as the denominator for metrics, and should not decide whether the scope itself accumulates. Previously, turning coverage off disabled both the `auto` and `agent` write paths at once, leaving `task_scope` with only rows added manually in the UI. `list_untested_assets` is still hidden when coverage is off (it is a purely coverage-oriented view).

#### Fixed

- **Fixed assets leaking between tasks** (#59): `list_assets` previously hard-coded the task id to 0 and queried the whole shared asset store bare, and the model had no way to write a scope filter, so assets of other tasks (especially IPs) were taken as this task's targets and the testing direction was led astray. It now returns only assets that fall within the test scope (`task_scope`) of [this task and its directly related tasks], matched by **ownership** rather than literal value: a root domain in the scope finds all subdomains / services / endpoints under it, and a network range finds the hosts and services inside the range; fetching an out-of-scope asset directly by id also fails. Non-task contexts (Auto/pentest) have no scope to rely on and still fall back to the whole store. It also fixes an ownership blind spot where hosts reached directly by IP (such as `http://1.2.3.4/api`) could not be matched by a network range scope. The UI's "Test assets" view (filtered by task producer) is not affected.
- **Fixed the worker cross-work lookback tools being deleted by mistake**: after `search_all_worker_traces` / `get_worker_trace` (and `node_detail`) were added to the worker default toolset, an old migration that "narrows the worker tool surface" unbound them again at startup, so workers actually could not get these tools. They have been moved out of that unbind list, and databases that already ran the old migration get them re-bound to the worker once.
- **Fixed occasional failures storing `/btw` side questions**: before the side_question checkpoint is written to the database, the NUL (`\u0000`) escape that JSONB does not support is stripped, so content containing that character no longer errors on write.

### Triggers

#### Fixed

- **Merged trigger sessions de-duplicate task description/goal per task**: when several tasks are merged into one trigger, the same task's description/goal is no longer repeated in the message, so long goals do not pile up repeatedly and blow up the context.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.9] - 2026-09-11

### Agent

#### Added

- The in-task main Agent supports **multiple sessions**: you can create, switch and reset the context independently, and every session can be interacted with.
- Supports persistent `/btw` **side questions**: ask follow-ups without interrupting the main thread, with Q&A stored in the database and still there after a restart.
- `spawn_task` gains `source_task_ids`, so a new task can **inherit read-only** the assets and findings of its source tasks.

#### Changed

- Turned off the worker's cross-engagement memory and narrowed its default tool surface: reading context and cross-work review are planning duties, and a worker only handles the execution and write-back of a single intent.
- Slimmed down the default worker prompt; `get_worker_output` drops the `terminated` and `worker_name` fields; the descriptions of `insert_assets` / `list_assets` were slimmed down.

#### Fixed

- Fixed the problem of a work being interrupted early by an **idle turn**: the model sometimes spends a whole turn outputting only thinking, with neither body text nor a tool call; the harness then sees a natural finish (`end_turn` with no `tool_use`) and wraps up directly with `completed` + an empty summary - an intent not yet finished breaks off halfway, showing up as "the task ended normally but there is no text summary at all". None of the five layers of LLM retry can reach it: it is not an error, the entry point of same-provider safe-window retry is "stream failure", the circuit breaker judges success directly when `err == nil`, and intent rerun only recognizes `model_error`; the SDK's empty-response retry cannot reach it either, because that layer judges emptiness by "whether any event was yielded", and the thinking deltas are themselves events. Now the work's Stop hook recognizes such a turn and injects a continuation instruction, letting the model carry on to the next step with the thinking it already produced - deliberately not "resend as is", because such idle turns are usually determined by the shape of the prompt and context and are stable behavior rather than random glitches, and resending would only make the model think it through again.
- The count reuses the **empty-response retry count** under "Retry & backoff" on the LLM page (both answer "the model finished but produced no substantive content", only the criteria and means differ), 2 by default, and entering `-1` turns it off and returns to the old "wrap up on idle turn" behavior. It is a **total** cap per intent rather than a consecutive count: the harness itself already limits consecutive idle turns to one push, and the quota refreshes only after a tool turn has really happened, so what this number blocks is a loop like "tool -> idle turn -> push -> tool -> idle turn" exhausting the intent budget. When triggered, the log records `[work <worker> · #<intent>] empty turn (thinking only, no text, no tool), injecting continuation instruction (n/N)`. See `docs/LLM重试设计.md` section 1.1 for the design.
- Fixed the context budget and input layout of `/btw` in long conversations; in non-secure contexts (non-HTTPS access) the side-request ID generation degrades gracefully.

### Traffic

#### Added

- Findings support **linking multiple pieces of traffic evidence**, which can be ordered, annotated, and distinguished by role (request / response / corroboration).
- The report Agent **automatically links** relevant traffic evidence before writing the report.
- Added an Agent **traffic binding switch**, and completed the hand-off of evidence between Agents.

### Task

#### Added

- Supports **session-level finding retest**: the retest runs in an independent Agent session, and the list and detail views show the run status.

### Intercept

#### Added

- Approval records gain **details and execution audit**: you can view the tool request context, the model/rule first-pass judgment, execution output and the parameter fingerprint.

### Assets

#### Added

- Task test assets support **DSL search**.

### LLM

#### Fixed

- Fixed the test connection not re-sending the custom session header, which made opencode zen return 400.

### Network

#### Added

- The HTTP transport of MCP supports **skipping TLS certificate verification**, making it easy to connect to services with self-signed certificates.

### UI

#### Added

- The conversation list is grouped by Agent, with expand/collapse and independent pinning; conversations can be filtered by Agent.
- The attack chain graph renders compression (digest) nodes and shrinks their members.

#### Fixed

- Fixed a white screen caused by login credentials getting out of sync.
- The wording of the intercept notice now clearly states "platform control", so it is not misread as a defense on the target side.

### Deployment and updates

#### Added

- Supports **one-click update from the page**: the system configuration page gains a "Version & updates" card, and the top bar lights up a hint when a new version is available. The flow is download the release package -> verify `SHA256SUMS` -> smoke test -> stage -> exit and let the supervisor script relaunch to complete the swap, and the page refreshes automatically.
- Added the supervisor start scripts `start.sh` / `start.bat` as the official entry point (included in both the release package and the Docker image; `install.sh` is unchanged); they decide whether to relaunch based on the exit code and are responsible for forwarding SIGTERM to artex. The verification and swap logic are all in Go, and the scripts are kept dead simple.
- Automatic fallback on failure: if verification or the smoke test fails, the update is discarded and the current version keeps running; if the new version fails to start 3 times in a row, the previous version is rolled back. The settings page also has a manual rollback (note that the database schema is not rolled back).
- Updates only trust GitHub domains and force HTTPS, and the release source is not configurable; one-click update is disabled in development builds. GitHub query results are cached for 30 minutes so that the top-bar hint does not exhaust the API quota.

#### Known limitations

- Under Docker only the program is swapped, not the image: the toolchain is not upgraded along with it, and recreating the container reverts to the version bundled with the image; use `docker compose pull artex` when needed.
- The `skills/` in the release package are not synced, so built-in skills newly added in a new version do not take effect automatically.
- Updating means restarting, which interrupts running tasks.

### Dependencies

#### Changed

- Upgraded norma to v0.3.7.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.8] - 2026-09-09

### LLM

#### Added

- The LLM page gains a "Retry & backoff" tab: both the **count** and the **interval** of the five retry layers are configurable. A failed model call goes, from inside out, through connection retry (SDK; connection reset / timeout / 429 / 5xx before the stream starts), empty-response retry (SDK; normal completion with no content at all, openai format only), same-provider safe-window retry (replay of a broken stream before any output was delivered to the caller), polling circuit breaker (consecutive failures up to a threshold cool the config down and skip it) and intent rerun (when a worker ends with model_error, the whole intent is rerun), and an outer layer only gets its turn once the inner ones are used up. Each layer has two knobs with uniform semantics: empty = use the original default count and exponential backoff; a count = use that count; an interval = replace exponential backoff with a fixed interval; -1 = turn this layer of retry off. The first three layers follow the endpoint, and each model configuration can override the global defaults field by field (pinning only the interval still inherits the global count); the circuit breaker and intent rerun are process-level semantics and exist only once, globally. Changes take effect hot after saving, with no restart needed. Leaving everything empty is the current behavior, and an old database is byte-for-byte unchanged after upgrading (new columns default to 0, and absent settings keys mean all defaults). The connection test deliberately carries none of these parameters - it has a 30s hard timeout, and stacking user-configured retries on top would only make a usable endpoint test as a timeout failure. See `docs/LLM重试设计.md` for the design.
- Each LLM configuration supports a custom **session header** (`session_header_key`): when non-empty, every LLM request carries that HTTP header, whose value is the session id of the current run (`conv-<id>` for chat sessions, `exp<x>-worker-i<intent>` for workers, and so on), for gateways that do prompt caching / sticky routing by a session-id header. The implementation reads the session id from the request context and injects it, with no change to norma needed, and even one shared provider can send different header values per session. Old databases get a migration, and the frontend LLM configuration dialog gains an input box.

#### Fixed

- Fixed saving an LLM configuration failing with `23502` when the session header field was empty: the column is `NOT NULL DEFAULT ''`, and `NULLIF($n,'')` had been wrongly applied, writing an unfilled session header as NULL and tripping the not-null constraint; the value is now passed directly with empty-string semantics.

### Agent

#### Added

- Wall-clock timeout is now **wrapped up in place**: relying on norma v0.3.6, when `MaxDuration` is reached it interrupts the running tool and continues on the live ctx for the wrap-up turns (writing back what has been identified + a summary, final state timeout), no longer relying on the external hard ctx of the worker/planner (`maxDur+90s` each) to kill a stuck run outright as `aborted_tools`. Chat uses the same harness and automatically gets the same behavior; mainagent has no `MaxDuration` and is not affected.
- Stall fallback: when the planner wakes on a heartbeat / no-change wakeup and the whole graph has no open or running intent left, the opening message becomes a stall alarm, explicitly stating that no worker is running and no direction is queued, and that this round must produce one or more mutually non-duplicate new intents (producing 0 intents is not allowed).

#### Changed

- Worker prompt restructured: the intent / launch instruction / raw JSON of the anchored assets moved into the system prompt, re-assembled every turn, never compressed away by compaction, and continuation no longer depends on the first transcript message surviving; the launch user message was slimmed down to only the global situation overview (degradable, tolerant of staleness). The cost is that per-intent data is mixed into the system prompt and cross-intent cache reuse is lost, a deliberate trade-off for "never lose the intent".
- Slimmed down the default planner / worker bodies and fixed several problems seen in practice: the planner distinguishes `recent_done` by status (blocked/exhausted: check the trace first and then decide, neither treat as a dead end nor blindly rerun), negative conclusions became "observation / doubt, not a final verdict" and evidence is checked before adopting them, when the goal is not met and there is no open/running intent it must produce one (hard floor), depth takes priority over coverage; worker negative conclusions write only "observation + tentative reading", with the verdict authority belonging to the planner, and cross-intent leads are written into the fact's summary for the planner instead of being chased by the worker itself. Reseed appends the new defaults as a new version and switches to it, while user-customized / old versions are kept in history and can be rolled back.
- Narrowed the work agent's default toolset and asset write-back hints: a worker only handles the execution and write-back of a single intent, and reading context / cross-work review are planning duties, so `list_facts` / `node_detail` / `list_companies` and cross-work search (`search_all_worker_traces` / `list_worker_traces` / `get_worker_trace`) were removed from its default tools, leaving only `list_findings` (check for duplicates before reporting a vulnerability) + `add_finding` / `record_fact` + `insert_assets` / `list_assets`; the prompt drops the stale field notes that conflict with the `insert_assets` schema (`type=tech`/`on_url`/`props`). Old databases get a one-time migration that strips the corresponding bindings, and the same-named bindings of planner/main are untouched.

### Exploration graph

#### Added

- **Exploration graph cold node compression (cold-digest)**: old and long-inactive intents / facts are folded into digest nodes shown in `graph_overview`, while the original nodes are kept permanently and can be fully restored by id (lossless storage, only the presentation is compressed, folding is reversible). Hot/cold is decided by reverse reachability + any live branch makes it hot, with R=6 rounds of debouncing and grouping by connected component; background compression (minor folds uncovered cold blocks, major recompresses from source to merge fragments) comes with activity re-checks and cooldown mutual exclusion, never goes on the hot path and never overwrites a node that has revived. The overview provides `cold_digests` and the asset-indexed `cold_index`, and `expand_digest` / `expand_index` do the restoring. The overview of related / inherited tasks likewise reuses its own folded view, and `expand_digest` supports read-only cross-task restoration. `expand_digest` / `expand_index` are given only to the planner and main agent, not to workers. All come with old-database migrations.
- `graph_overview` outputs `finding_list` in full: findings are the highest-value output of a task and a single task usually has few of them, so they are now carried in full in the overview (unlike facts, which only get the recent window), letting the planner/worker see all confirmed vulnerabilities at a glance every round without calling `list_findings`. Each is slimmed down to `{id, summary, evidence?, from_intent?, assets?}`, where the affected assets are given directly as readable content (url / domain / ip:port) rather than bare ids.

#### Changed

- `graph_overview` no longer lists `hosts` flat: the coverage block drops the host list (in large-scope tasks each round carried up to 500 host strings, of limited value for planning decisions) and keeps only `host_count`, and specific hosts are queried on demand with `list_assets`. Added the top-level `done_intents_total` (total number of finished intents), parallel to `recent_done_intents` which is truncated to at most 15, so the planner knows whether anything was cut when de-duplicating.

### Tools

#### Changed

- `add_company_scope` default binding changed from worker to planner: defining the company asset scope is a duty of planning / main control / Auto, and workers only execute exploration. New databases seed the default binding as mainagent/planner/auto, and old databases get a one-time migration.
- The planner default-binds `list_assets`: it now has both `list_assets` (DSL search over the whole store) and `list_untested_assets` (untested within scope); old databases are backfilled once without overriding a user's unbinding.

### Task

#### Added

- The task list gains a "Running workers" column: counts the intent nodes with `state='running'` under the task, with exactly the same definition as "Running workers" in the task detail page overview, and shows 0 when there are no running workers.

### Network

#### Added

- Added a **global egress proxy** setting: all target traffic can go out through a unified global proxy (http/https/socks5, `user:pass` supported). When traffic capture is on it acts as the upstream of the MITM recording proxy (traffic is still recorded and then leaves through the proxy; both the intercept and pass-through paths use the upstream, without leaking the source IP); when capture is off it is injected directly into the agent's bash environment and WebFetch (`proxyEnv` adds `ALL_PROXY` support for socks5). The setting is stored in the settings KV table (no migration needed), and the frontend system configuration page gains a "Global proxy" card, independent of the web search proxy and the LLM proxy.

### UI

#### Fixed

- Corrected semantic errors in the intent status labels: `exhausted` "Exhausted" -> "Budget exhausted" (it actually means the step / time budget was reached and the run was cut off midway with only partial results written back, not that the direction was fully explored), `blocked` "Blocked" -> "Error" (it actually means model / API / network failures exhausted their retries and the intent was essentially never really explored, not a target / WAF block), and added the previously missing `stopped` "Stopped" (a work stopped manually by the user).

### Dependencies

#### Changed

- Upgraded norma to v0.3.4: MCP tool output gains truncation and spilling to disk (see `651b961`), and the later v0.3.6 supports wall-clock in-place wrap-up.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.7] - 2026-08-31

### LLM

#### Added

- Model configuration gains an "Output limit", with a choice of which request field name the limit uses: the limit caps how many tokens a single reply can generate and is sent with every request; 0 (the default) = do not send the field and let the server default decide; it is a different thing from the "Context window" - the latter is the model's total capacity, used only locally to compute the compression threshold and never appearing in the request. The field name switch only matters for the `openai` (Chat Completions) format: empty (default) sends `max_tokens`, which the vast majority of compatible gateways accept alone; OpenAI's official reasoning models (o-series / GPT-5) conversely accept only `max_completion_tokens` and reject `max_tokens` outright with `unsupported_parameter`, and such endpoints must be switched manually to the new field. The two other formats each fix the field name (`max_tokens` for Anthropic, `max_output_tokens` for the Responses API), so for non-openai formats the switch is greyed out and cleared on save. It also wires up a path that had never been connected: planner / worker / chat / main Agent / goal decomposition all had no output limit set, so openai-style requests never sent one and Anthropic fell back to the SDK's 8192; adding only the config item without connecting this line would mean a filled-in number had no effect; the value is now resolved every round according to the configuration, just like "streaming output", so it takes effect on the next round after a failover switch. Behavior is completely unchanged for old databases after upgrading (new column defaults to 0 and an empty field name).
- When "Test" is clicked in the admin UI, the status code and raw gateway response body of every HTTP attempt are written to the server log (the response body trimmed to 4K), making it easier to diagnose 401s, quota messages, empty frames, HTML pages being returned and the like, instead of only seeing the ok / err collapsed in the UI. This log does not depend on the "LLM records" switch.

#### Changed

- The task LLM configuration chain can now be modified in any state, no longer restricted to "running / paused / chain exhausted": after a task ends (done / failed / timeout) the main Agent conversation still goes through this chain, and when a model on the chain broke it could be neither changed nor interacted with. The terminal-state guards in both the backend HTTP and the DB transaction were removed, and the frontend dialog also allows editing and saving for terminal-state tasks. Saving for a terminal-state task no longer reopens quota-blocked intents (those intents would be moved to open, with no worker executing them and no longer meeting the condition for "rerun intent"); to keep running, use rerun intent / add a goal, which pull the task back into the running state.

### Agent

#### Changed

- "Send a message to a running Worker to adjust its direction" now reuses the existing pause / resume + transcript resume mechanism, behaving the same as the main Agent conversation: previously it went through a self-built intervention persistence protocol that touched the scheduling barrier, the resume flow and more than ten activity query filters. Messages are now injected through the next turn's input, and the intent runs directly in its own goroutine, not limited by the 3-slot worker pool and running as soon as it is sent; the frontend keeps the message box and restores the "Continue directly" button, with sending now carried over SSE. No DB schema change. Trade-offs: messages live only in memory with no crash recovery, and when a message is sent the task may momentarily exceed its concurrency by one worker (a low-frequency scenario, acceptable).

### Task

#### Fixed

- Fixed an out-of-memory (OOM) crash when archiving large tasks: archiving now writes the snapshot as a stream instead of reading the whole task into memory at once; it also closes three recovery gaps on the cold archive path - the traffic of modern installs lives only in SQLite, and previously a crash between the PostgreSQL commit and the SQLite commit left the traffic of an already archived task stuck in hot storage with no way to recover it; a staging journal is now written whether or not there is a history directory, and a missing `journal.json` is treated as discardable.
- Fixed system sluggishness when there are many tasks: loading conversations and loading the UI had obvious delays. The queries for the task list, task context and exploration records were optimized together, and the frontend dashboard, conversation page and task detail page now make fewer duplicate requests.

### Assets

#### Changed

- Removed the "at most 256 rules" limit on a company's asset scope: companies that enter their scope one IP / domain at a time easily hit this cap and could then only be split into several companies, while the scope itself is no slower for having more entries. The limit was removed in all three places - frontend, backend and the demo mock; a single rule is still limited to 1024 characters, and the 2 MiB request body limit stays as a backstop (roughly 40-50 thousand rules).

### Skills

#### Fixed

- Fixed uploading a skill archive failing with "Upload failed: zip: unsupported compression": the Go standard library ships only the Store / Deflate decompressors, so bzip2 and Zstandard archives written by archivers at non-default levels could never be unpacked. Both decompressors are now added (pure Go, no new external dependency); methods that truly cannot be unpacked, such as Deflate64 / LZMA / XZ / PPMd, as well as encrypted archives, now report a clear hint before extraction, naming exactly which file used which compression method and how to repack it, instead of throwing the low-level English error at the user.
- Fixed the file name validation inside skills rejecting Chinese file names: path validation was originally an ASCII allowlist of `[A-Za-z0-9-_./]`, so a single Chinese-named file in the archive (such as `参考/说明.md`) made the whole upload fail with "archive contains an illegal path". It is now a Unicode denylist: file names in any language and spaces are allowed, while control characters, invalid UTF-8, zero-width and bidirectional control characters (RLO file name spoofing), `\ % # ? * : " < > |` and `..` / absolute paths / empty path segments are still rejected, and the directory traversal protection is unchanged. Skill names are relaxed as well - ASCII is still limited to lowercase letters, digits and hyphens (the agentskills.io spec), while Chinese and other non-ASCII letters can be used directly as a skill name, but spaces, dots and path separators are not accepted.
- Fixed garbled file names or whole-archive rejection after extracting Chinese archives made by Windows archivers: such zips do not set the UTF-8 flag and write file names in GBK, and the names are now decoded with a GBK fallback before path validation. A quoted frontmatter such as `name: "中文技能"` also yields the skill name correctly now.

### UI

#### Added

- The Discovery page gains an "All findings" flat view, set as the default, switched with the header tabs against the original "Grouped by task" view: in the grouped view you have to expand task after task to see findings, which is a detour when you only want to scan the list across tasks. The flat view is one large cross-task table (10/20/50/100 per page) whose behavior is fully consistent with the grouped view - tick to export, expand inline for evidence and the detailed report, edit name/category/severity inline, change disposition status, drill in, delete. The two views share the stat cards and filter bar, switching does not lose filters, and the current view and filters are remembered locally; polling hits only the current view, and inline changes are written to both caches, so switching over never shows stale data.
- The Discovery page gains a "By asset" view: on the left an asset tree (company -> root domain / IP -> subdomain -> service -> endpoint, with the company level appearing only when an asset really has an owner), on the right the findings under the whole subtree of the selected node, sharing the same table and the same filters as the other two views. The tree includes only assets that have findings, with the ancestor chain filled in on demand (when a finding hangs only on the deepest endpoint, the levels above can still be assembled), and the count on a node is aggregated over the subtree and de-duplicated by finding; findings whose asset was deleted or that never had a linked asset go into the "Unlinked assets" bucket. Unlike the other two views, the asset view does not poll - it queries when you enter the view, change filters, add/edit/delete findings on the page, or click the refresh button on the tree; the left tree is a navigation structure and there is no need to recompute it every 5 seconds. Each tree level shows only the increment relative to the previous level (subdomains with the root domain suffix removed, services shown as `https :443`, endpoints showing only the path), with the full values in the hover tooltip and the breadcrumb; when there are too many nodes the endpoint/service levels are dropped for the whole level with a notice, and the counts still roll up into the upper level.

#### Fixed

- Fixed the conversation transcript being squeezed and not fully visible in the mobile conversation detail: on narrow screens the conversation list is collapsed, giving the width to the transcript itself.

### Dependencies

#### Changed

- Upgraded norma v0.3.2 -> v0.3.3: added `Config.MaxTokensField`, so that the output limit of OpenAI Chat Completions can be sent as `max_completion_tokens` instead (reasoning models accept only this key). The two keys are mutually exclusive and only one is sent, with the default still `max_tokens`.
- Upgraded `golang.org/x/mod` v0.37.0 -> v0.40.0, fixing two dependency security alerts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)
- [@begininvoke](https://github.com/begininvoke)

## [0.3.6] - 2026-08-27

### LLM

#### Fixed

- Fixed the "non-streaming" switch turning back into "streaming" after being saved and reopened (#69): the DTO of the configuration list endpoint was missing the `streaming` field, so the response never returned it, and the frontend, reading `undefined`, always fell back to the default streaming; the actual write and DB storage were fine, it simply could not be read back. The DTO now has the field back (without `omitempty`, since `false` must also appear in the response).
- The connection test now verifies that the model really replies, and tests using the configuration's real send/receive mode (#65): previously it only checked whether HTTP succeeded, so a request that went through but got zero reply from the model (thinking burned the whole budget / body swallowed by a safety policy / a compatibility layer dropping `content`) was still reported as "connection successful", at odds with the "no reply" behavior seen in conversations; and the test always streamed, so a non-streaming configuration was actually tested on a different channel. An empty reply is now judged a failure outright, the model's reply is shown in the message on success, and the current streaming switch is carried into the test, keeping "test passed" consistent with "conversations work".

### Agent

#### Changed

- `list_facts` became pagination + keyword filtering, so many facts no longer blow up the context in a single call (#74): returns the latest 20 by default, supports `limit` (max 100) / a `before` cursor / `q` summary keyword, and returns `{facts, total, has_more, next_before}`; an overlong single summary is truncated by character count (the full text still goes through `node_detail`). The worker / planner prompts were updated to pagination semantics. Old databases get a one-time migration that refreshes the new parameter schema into the tool catalog table (`SeedTool` only inserts the first time, otherwise the tool management page shows "no parameters").

### Tools

#### Added

- The "Tool executions" page gains tool call count statistics (#72): the toolbar "Statistics" button opens a dialog showing, per tool, the call count, share and failure count (descending by count); it reuses the list's task / keyword filters, counts the whole result set rather than the current page, and fetches only when the dialog is opened.

### Dependencies

#### Changed

- Upgraded norma v0.3.1 -> v0.3.2: fixes reasoning/refusal being silently dropped in OpenAI-style responses (Responses adds `reasoning_text`), de-duplication of the three names of the reasoning field, bounded retry for empty responses (zero content blocks), and gateway 400s caused by the compression boundary cutting through a tool pair (also fixing existing orphans already baked into transcripts).

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.5] - 2026-08-25

### LLM

#### Added

- Each LLM configuration supports a streaming / non-streaming switch (streaming by default): on uses streaming SSE; off uses true non-streaming (`stream:false`, the complete JSON returned at once), which can bypass the poor SSE implementations of some gateways (empty frames, thinking fields dropping frames), at the cost of losing live progress and live Token counting while running. worker / planner / mainagent / chat / goals all read the value dynamically from the currently active configuration; the three Provider wrapper layers `llmpool` / `llmrec` / task runtime are all compatible with non-streaming; `llm_profiles` gains a `streaming` column and old databases are patched with `ALTER` (default `true`, old configurations notice nothing).
- Supports LLM configurations in the OpenAI Responses API format: every configuration gains a third format `openai-responses` (hitting `POST /v1/responses`), alongside Chat Completions / Anthropic; `BaseURL` is normalized, the default model is `gpt-5`; the `format` constraint of `llm_profiles` adds `openai-responses` with an idempotent migration patching old databases; the frontend format dropdown gains "OpenAI (Responses API)". Dependency upgraded to norma v0.3.1 (including the `reasoning_content` passthrough fix).
- Records the raw HTTP text of LLM requests/responses: the real wire body is captured at the HTTP transport layer, keeping the tool schemas, `tool_use` blocks and raw SSE frames that the normalized view cannot see; the multiple attempts of norma's internal retries are each kept; `llm_records` gains `raw_request` / `raw_response` columns and old databases are patched with `ALTER`; the detail panel of the records page gains a "Raw" view toggle and request/response copy buttons (with an `execCommand` fallback for non-secure contexts).

### Conversations

#### Changed

- Multi-select in the conversation list became a "Multi-select" mode switch: by default the list no longer shows a checkbox on every row (cleaner look), the header becomes a "N total + Multi-select" button; clicking "Multi-select" enters selection mode (checkboxes, select all, batch delete), "Done" exits and clears the selection, and after a fully successful batch delete it exits automatically (on failures it stays in selection mode to make retrying easy); single-item rename / pin / delete still go through each row's ⋯ menu.

### UI

#### Changed

- Improved task management, conversation actions and traffic viewing (#57): the task list column sort preference is persisted, inline rename is triggered directly by an icon (no longer through a menu), and the drawer (sheet) interaction and traffic viewing details were polished.

#### Fixed

- Worker asset labels now show only domains and IPs, and empty labels are handled correctly.

### Agent

#### Added

- The planner gains a "quantified acceptance check" when judging goals: when a goal contains quantifiable acceptance conditions (asset test coverage reaches X percent, N flags obtained, a certain privilege gained), the measured values in `graph_overview` (`coverage.pct`, findings counts and so on) must be checked before `prove_goal`; if the measured value falls short, `prove_goal` is forbidden and intents are dispatched to close the gap instead, and it must not mark a goal as met early on the grounds of "the main part is done / roughly achieved". Fixes a goal requiring 100 percent coverage being judged complete with only 40 percent measured.

#### Changed

- Rewrote the criteria for "0 intents this round" in the planner prompt: the original text described 0 intents as "the most common and most important principle", which made the planner stop too early when goals were unmet and untested surface remained in scope. It now says 0 intents should be produced only in two specific situations - (1) every direction thought of is already covered by open/running/recent_done intents; (2) the next step depends on the output of a currently running work that has not appeared yet (wait for it to finish and plan again on the next wakeup after the graph updates). A reverse constraint was added: when there really is an uncovered new direction that does not depend on running works, or the goal is unmet and untested surface remains, do not stop just because "0 intents is common".
- Strengthened the "evidence bar for negative conclusions" in the worker prompt: for negative conclusions such as "not injectable / port closed / no login entry" that may make the planner abandon a whole direction, the worker must exhaust the reasonable techniques within that intent (change encoding / parameters / path / method) before concluding; if the techniques were not exhausted or the evidence is weak, always mark `confidence=inferred`, to avoid welding a whole route shut with a careless `observed` negative (an early wrong negative in a task in particular leads the direction astray and is hard to self-heal).
- The planner / worker default prompt changes above are appended as a new version and switched to current via one-time migrations (`reseedPlannerPrompt` / `reseedWorkerPrompt`, each guarded by a settings flag); old versions stay in the version history, and users who customized the prompts can restore them from the version records.

### Operations

#### Added

- Added `reset-password.sh` to reset the administrator password (the username is fixed as `ARTEX`): supports both local / docker deployments, connection info can be given explicitly or read automatically from `--dsn`/`$ARTEX_PG_DSN`/`config.json`; it uses `pgcrypto` inside the database to generate a bcrypt hash compatible with the backend login and writes it back to `settings.auth.password_hash`, with no service restart needed after the reset. The password is passed in through an environment variable, never enters the process argv, and is escaped to prevent injection.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

## [0.3.4] - 2026-08-24

### UI

#### Added

- After ticking several tasks in the task list you can change their category in bulk; the target category supports "Uncategorized" to move tasks out of the current category; the whole batch is written in one transaction, and the only failed items can be tasks that were deleted after being ticked.

#### Changed

- The "Task category" of a new task changed from a dropdown select to a searchable input: typing filters the existing categories, and a name not in the database is created and selected immediately on Enter (or by clicking "Create" in the dropdown), with the selected item shown as a removable tag; still limited to a single category.

### Conversations

#### Fixed

- Fixed sending a message being blocked by "LLM not configured, cannot chat" when the conversation had a chosen LLM configuration but there was no globally active configuration. The pre-send check originally looked only at the global active configuration while actual execution prefers the configuration chosen by the conversation, so the two places resolved inconsistently; both now use the same logic (conversation/Agent binding first, global as fallback).
- The hint shown when chatting is impossible is now split by state: with no configuration at all it says "Add a configuration", with configurations but none active it says "Activate one or specify a configuration for this conversation", and it no longer always says "not configured", which makes the cause easier to locate.

### Agent

#### Added

- For a task whose goals are all met, intents dispatched by the main Agent via `add_intent` can be picked up and executed directly by Workers and stop when done: the Planner no longer runs at that point (a task with no open goal does not enter planning), which keeps it from re-judging the goal as met and cancelling the intents just dispatched; once the frontier drains the task returns to completed. Before dispatching an intent the main Agent asks the user, based on the intent's content, whether to register it as a formal goal, and registering it restores the task's regular autonomous planning.

#### Changed

- When `step_ids` of `get_worker_trace` / `get_task_worker_trace` exceeds the one-time limit (5), it no longer errors outright; it returns the full content of the first 5 steps and tells, through `returned_step_ids` / `omitted_step_ids` / `notice` in the result, which were fetched this time and which are still not fetched, with the hint "no need to fetch more if this is enough". Duplicate and invalid ids passed in are de-duplicated and removed before counting.

#### Fixed

- The task list is now sorted by task id descending (most recently created first), replacing the previous sort by creation time. Several tasks created at the same moment have identical timestamps and an unstable order, and together with the 10-second polling and the random iteration of the in-memory map, this made them swap positions in the list frequently.

### Assets

#### Fixed

- Fixed company ownership recomputation being brought down by a single row of dirty data: when `assets.ip` held a hostname (written by the Agent or the asset API), `a.ip::inet` would raise `22P02`, making adding/changing a company asset scope and deleting a company all fail and roll back. The safe conversion `try_inet()` is now used, and invalid values are skipped instead of aborting the whole statement.
- An unparseable `ip` is no longer skipped silently: after a company scope is saved, it now clearly tells how many assets have an `ip` that is not a valid IP (with the specific ids and values), reminding that those assets will not be attributed by IP/CIDR rules; the same information is also written to the server log, covering paths with no frontend response (deleting a company, scopesentry sync, Agent writes).
- The IP/CIDR matching of the task test scope also switched to `try_inet()`, replacing the previous regex guard that only checked the character set (all-hex-letter hostnames such as `abc.def` previously still slipped through and triggered the same error).

#### Changed

- The `ip` field of `ip`, `service` and `endpoint` assets no longer accepts hostnames: `insert_assets` and the asset API return a per-item error carrying `index` and directly give the fix (use `type=subdomain` and fill `domain`, or resolve the A/AAAA record first), so the Agent can correct itself and re-insert. Other assets in the same batch are unaffected and are stored as usual.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.3] - 2026-08-23

### Worker

#### Added

- A running Worker supports individual pause, resume and cancel; pausing keeps the intent, facts and findings, and cancelling transactionally cleans up the current intent and its direct products after the execution exits.
- Added the `paused` intent state, an execution fence and named termination reasons, preventing late blackboard writes after a pause, cancel or task deletion.
- Added an optional task concurrency limit; creation, resume, finding deep-dive and queue backfill all go through a persistent FIFO admission path.

#### Changed

- The default wall-clock duration of a single Worker run was changed from 600 seconds to 1200 seconds; after a timed-out task is revived, the timer restarts at the next real execution.
- The real execution states of Worker, Planner and main Agent now drive the task status badge uniformly, fixing the badge still showing "Idle" while a Worker was running.
- The Worker session keeps individual controls and the current-session Token summary, and the model name moved to the right of the current session title.

#### Removed

- Removed the Worker multi-select, select-all and batch pause/resume UI, and also removed the Worker batch control API and the Mock contract.
- Removed the Token badge and Tooltip from Worker list rows; the underlying full Token ledger and the task aggregation endpoint remain.

### LLM

#### Added

- Tasks support an ordered LLM configuration chain; when provider quota exhaustion is clearly recognized it switches to the next configuration automatically, and persists the current configuration, the exhausted state and an error summary.
- Running and paused tasks support editing the full configuration chain, reordering it and manually switching the current configuration, effective from the next LLM call.
- Automatic switches, manual switches and whole-chain exhaustion are written to structured system activity, and a de-duplicable reminder pops up through the task activity stream.
- Added a task role model resolution endpoint, which uniformly resolves the configuration and model used by the next call of the Main Agent, Planner and Worker.
- Added an optional global LLM Pool, supporting configurable call order, fallback when a specified model fails, health status, cooldown recovery and manual reset.
- Added an always-on LLM usage ledger that aggregates input, output and cache Tokens by task, session, model and configuration.

#### Changed

- Goal decomposition, Planner, Worker and the main Agent share the task-level LLM runtime (resolution priority below).
- Failover responds only to explicit quota, balance or billing errors; ordinary rate-limit, authentication, network, server-side and context errors do not trigger a wrong switch.
- The LLM configuration page became a configuration card list with a drawer editor on the right, and provides the Pool polling order, priority, exclusions, health status and recovery actions.
- The configuration chain exhaustion message supports wrapping on narrow screens; the current model changed from a conversation-list icon to a text label next to the current conversation title with a full Tooltip.
- The order in which each role in a task resolves its LLM is now "Agent binding -> task configuration chain -> global": a role with an explicitly bound model always runs on that model, only unbound roles fall to the task chain, and when the task chain is empty they fall to the global one.
- An empty egress proxy means a direct connection and no longer falls back to the `HTTP_PROXY`/`HTTPS_PROXY` environment variables (an explicit `ARTEX_LLM_PROXY` is not affected); the proxy input supports `socks5://user:pass@host:port` with credentials.

#### Fixed

- A transient streaming failure before commit (before any output has been produced) is now retried with safe backoff on the same provider, significantly reducing cases of "stopped after only one or two tools, no summary, final state `model_error`"; quota exhaustion, context too long and deterministic 4xx errors are not retried and are still handled by failover and compression recovery respectively.

#### Removed

- Removed the LLM icon-style marker in the conversation list, to avoid confusion with the Worker status icon.

### UI

#### Added

- Tasks support creating, renaming, deleting and filtering global categories; a new task can choose a category directly.
- Added global task template CRUD and a management drawer on the right; a new task can load a preset description and goals, or save its current content as a template.
- A new task supports linking multiple source tasks and multiple company asset scopes; the company scope automatically recognizes domains, URLs, IPs, CIDRs, ICP and company keywords through a single multi-line text box.
- Task test assets support live add and delete, recording sources such as manual, company, inherited or Agent-discovered; the current test assets and a source summary are shown next to the Worker session title.
- The Discovery page is grouped by task with independent pagination inside each group, and supports filling in a description for a finding and creating a high-priority deep-dive intent.
- Conversations support rename, pin, unpin and delete; the task list supports batch pause and resume for the current page.
- Traffic details and tool execution details became a drawer on the right; HTTP messages now fill in `Host` and highlight the request line, status code, headers, and JSON and markup-language bodies.
- The task list action column gains "Details" and "Pause/Resume" buttons; the conversation send key binding can be configured in system settings (`Enter` / `Cmd+Enter` and so on), and the Web search area gains a proxy input box.
- The badge to the right of the conversation title now shows the LLM configuration name, with the model ID moved to the hover Tooltip; a task can have an optional name, and the task list gains a name column and search (falling back to the description when empty).
- The Skills page gains usage count, last used time and missing dependency statistics, making it easier to find skills that are not taking effect or not installed.

#### Changed

- The task title became a focusable detail link; task assets, company assets, Discovery task groups and the findings within a group use real server-side pagination, stable sorting and exact totals.
- Adding a company now uses a drawer on the right, and scope entry is unified as multi-line text with live recognition, validation and type preview.
- The main Agent input box supports auto-growing, `Enter` to send and `Shift+Enter` for a newline, and avoids accidental sends while a Chinese IME is composing.
- Agent previews, task reports and related details uniformly use shared Markdown rendering; delete confirmations, long errors and mobile drawer widths were fixed uniformly.
- The task template select shows and searches by template name and submits by template ID; the original system font size was restored, and the app version is now uniformly shown as `0.3.3`.
- Task totals and the current-session Token highlight only the input, cache read and output values; the send button uses a simpler up-arrow icon.
- The test scope in the task overview shows company names instead of IDs.

#### Fixed

- Ordinary text containing a dot is no longer misjudged as an ICP filing number.
- Fixed a name clash in the variable catalog with global runtime variables (such as `{{.Now}}`), which made the Agent editor variable list render duplicate keys.

#### Removed

- Removed the independent "Enter" button on task cards; clicking the task title now uniformly opens the details.
- Removed the Dashboard "New task" button, the Logo URL input when adding a company, and the finding count Badges at the top of the Discovery page and in task groups.
- Reverted the 10 percent global font enlargement style, and removed the Worker checkbox, model icon and row-level Token statistics from the conversation list.

### Agent

#### Added

- A new task can directly link multiple existing tasks, inheriting in real time and read-only the goals, facts, findings, finished intents, asset scope and blackboard context of the direct source tasks.
- The blackboard read tools support querying source-task nodes, facts, findings and execution traces on demand; inherited nodes carry a source marker and all write tools refuse to modify them.
- The company scope tools support domains, URLs, IPs, CIDRs, ICP and company keywords; keywords serve only as Agent scope hints and do not take part in automatic asset attribution.
- The finding deep-dive action creates a high-priority manual Worker intent in the original task, with asset anchors and a `derived_from` edge.
- The overview gains a "Goal management" card that supports manually viewing, adding, changing and deleting goals; adding or changing notifies the Planner and revives the task, while deleting hard-deletes the goal node (cascading to edges and anchors) without reviving.
- The main Agent gets the `steer_work` tool by default, which can inject course-correction instructions into a running Worker in real time without interrupting it or losing progress (the intent is still verified to belong to this task).

#### Changed

- Source-task intents do not enter the new task's frontier, and the new task keeps using its own independent exploration, execution queue, working directory and conversation history.
- Deleting a running intent no longer destroys data: it now stops it at `stopped`, requires a deletion reason, attaches the reason to the intent as a fact and writes it into the payload, and the planner perceives it as a `cancelled` trigger (keeping the intent content and reason).
- The main Agent conversation is now driven by the server-side activity stream, and input can be recovered after a failed send; opening a conversation automatically sticks to the bottom and keeps the last reply visible after the details lazy-load.
- Pausing a task terminates the current Main Agent, Planner and Worker calls, but does not stop the user from actively starting a new main Agent orchestration session during the pause.
- Cancellation, closing and streaming interruption now uniformly keep the real termination reason, the content already generated, the number of rounds run, elapsed time, Token usage and tool calls that did not return.

#### Removed

- Removed the main Agent's client-side optimistic message echo, avoiding duplicate messages and cross-conversation stream mixing under pause, failure or concurrent activity.

### Constraints

#### Added

- The goal decomposition stage first extracts operation constraints (allow/deny) and then decomposes goals, and the main Agent can add more at runtime; constraints are injected into the Planner and Worker system prompts at the highest priority, and after de-biasing, diversity and surface-widening exploration explicitly obey the constraints.
- The overview gains a "Constraint management" card with create/update/delete endpoints; constraint injection can be switched per Planner / Worker (both on by default, read each round and effective immediately). The new table `task_constraints` uses `CREATE TABLE IF NOT EXISTS`, and old databases get it created automatically.

### Intercept

#### Added

- Command intercept gains a model fallback approval on top of regex/string rules: when no rule matches, the model makes an `ALLOW`/`ASK`/`DENY` semantic judgment, with the failure fallback action and the approval timeout action configurable; the intercept page splits into two tabs, "Intercept rules / Model configuration", and model judgments are marked with a `[模型]` prefix and a reason.

#### Fixed

- Hardened the judge output parsing, so that a correct `DENY` judgment is not mistaken for an allow.

### Traffic

#### Changed

- Traffic recording now stores a whole exchange in SQLite (large bodies overflow into a blob bucket de-duplicated by hash), and text bodies get a trigram full-text index, supporting arbitrary substring and Chinese search; deletion degrades to a single SQL transaction, from hours down to milliseconds, and no longer stalls recording while it runs. Added streaming download for large bodies and the `traffic_search` full-text parameter; old file-tree data needs no migration and can still be read, searched and deleted.

### Tasks and assets

#### Added

- Creating a task gains an "Asset coverage" switch (on by default): when off, coverage is neither computed nor shown, the situation graph shows only assets, `task_scope` is not accumulated automatically, and the related tools are removed from the Planner / main Agent; company linking is not affected by this switch.
- Each asset in `insert_assets` gains a `related` flag (default `true`): effective only when coverage is on, `false` puts it only into the shared asset store and does not count it toward this task's coverage (such as side sites or unrelated assets found in passing).
- Company scope and task test assets uniformly support recognition of domain, URL, IP, CIDR, ICP and keyword text; domains/IPs can create or reuse global assets, while CIDR/ICP/keywords are kept as task scope context.

### Build

- `build.sh --release` can build Linux amd64/arm64, macOS amd64/arm64 and Windows amd64 in one go, and produces a zip release package with `skills/`, a config example and the README.
- Uses the Go linker to strip debug info and produces a zip release package; UPX is now explicitly optional, to avoid its self-extracting ELF segfaulting at startup in some Linux environments.
- The Release Workflow runs a startup smoke test on the Linux amd64 binary, and ships `SHA256SUMS` with the release package.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.2] - 2026-08-20

### Added

- A new task supports linking multiple existing tasks, inheriting in real time and read-only the facts, findings, finished intents, asset scope and blackboard context of the direct source tasks; the new task still uses its own independent execution queue, working directory and conversation history.
- Tasks support an ordered LLM configuration chain. When provider quota exhaustion is clearly recognized it switches to the next configuration automatically, and persists the current configuration, the exhausted state and structured audit activity.
- Running and paused tasks support editing the LLM configuration chain, reordering it and manually switching the current configuration; automatic switches, manual switches and whole-chain exhaustion are shown on the task detail page.
- A running Worker supports pause, resume and cancel. Pausing keeps the blackboard data, and cancelling transactionally cleans up the intent and the facts, findings and execution records it directly produced after the Worker stops writing.
- Deleting a task can optionally also clean up the associated assets, traffic, findings and task files on disk, with a deletion barrier, concurrency protection and auditable deletion statistics added.
- Added an optional task concurrency limit; new tasks beyond the limit queue in FIFO order and start automatically once a running slot is released.
- Added task asset pagination, company asset pagination, task category statistics and a paginated-intent Mock contract.
- Added the `build.sh` single-binary build script, supporting static frontend export, resource embedding, cross-platform targets and build version injection.

### Changed

- The explicit task-level LLM configuration chain coexists with the existing global LLM Pool; the explicit chain keeps the strict quota-failover semantics, and without an explicit chain the Agent binding and global configuration rules apply as before.
- The main Agent input box supports auto-growing multi-line input, `Enter` to send and `Shift+Enter` for a newline, and avoids accidental sends while a Chinese IME is composing.
- Planner, Worker and the main Agent share the task-level LLM runtime; the task chain uses the smallest context window among the candidate models as the safe compression threshold.
- The task detail page shows running, idle and paused states by the real LLM call status; facts, findings, intents, asset references and graph nodes from source tasks are uniformly marked with their source and kept read-only.
- Agent previews, task reports and related detail views uniformly use the shared Markdown rendering component.
- LLM model configuration uses card and drawer interactions, and the model list scrolls independently inside the drawer.
- Pausing a task no longer blocks the main Agent conversation: the main Agent orchestration session is independent of task pause, and new messages can still be sent while paused (pausing only terminates the round currently in progress).
- The default wall-clock duration of a single Worker run was changed from 600 seconds to 1200 seconds.

### Fixed

- Removed the optimistic echo of the main Agent console in favor of rendering purely from server data, fixing messages getting scrambled or content from other conversations leaking in under pause or send failure.
- Opening a main Agent session now scrolls to the bottom by default and fully shows the last reply: after the full content of the last reply is lazy-loaded and expanded it sticks to the bottom automatically instead of being pushed off screen.
- Fixed the main Agent session continuing to run after a task was paused, and the main Agent not being stopped in sync when the orchestration Agent paused a task.
- Fixed the task status badge and the action buttons getting out of sync when a task completes or when the Planner, Worker or main Agent is actually running.
- Fixed the possibility of late blackboard writes or leftover files between Worker cancellation, task deletion and concurrent writes.
- Fixed Markdown preview breaking, long delete-confirmation text overflowing, mobile width issues and some task detail buttons being misaligned.
- Added named termination reasons to all Agent cancellation paths, so activity details can show who cancelled, the final state, rounds run, elapsed time, Token usage and tool calls that did not return.
- Fixed a race where the parent context could preempt and overwrite the named `shutdown` reason when the backend shuts down, and garbled text caused by truncating Chinese activity summaries by byte.
- Fixed cancellation events with partial streamed output losing the real termination reason, while keeping the content generated before the cancellation.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

[Unreleased]: https://github.com/Autumn-27/ARTEX/compare/v0.3.10...HEAD
[0.3.10]: https://github.com/Autumn-27/ARTEX/compare/v0.3.9...v0.3.10
[0.3.9]: https://github.com/Autumn-27/ARTEX/compare/v0.3.8...v0.3.9
[0.3.8]: https://github.com/Autumn-27/ARTEX/compare/v0.3.7...v0.3.8
[0.3.7]: https://github.com/Autumn-27/ARTEX/compare/v0.3.6...v0.3.7
[0.3.6]: https://github.com/Autumn-27/ARTEX/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/Autumn-27/ARTEX/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/Autumn-27/ARTEX/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/Autumn-27/ARTEX/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/Autumn-27/ARTEX/compare/v0.3.1...v0.3.2
