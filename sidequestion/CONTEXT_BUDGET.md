# Side-question long conversations and context budget

Fixed on 2026-09-11. The original implementation treated the character count of the request JSON directly as tokens and inherited the main task's 32K output reservation, so it rejected normal side questions early when HTML, JS and tool results were plentiful.

## Open-source implementation review

- [Grok CLI side-question context](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/agent.ts#L739): extracts snippets from the most recent user and assistant text, with a character budget of about 2000 and at most 400 characters per message. It does not maintain a continuous side-question history on this path.
- [Grok CLI standalone request](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/utils/side-question.ts): independent cancellation signal; output capped at 2048 tokens when the model supports it; no tools provided.
- [Grok CLI main-session compaction](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/compaction.ts): estimates tokens, keeps recent content, folds new content into the old summary, and handles truncation across turns.
- [OpenCode session compaction](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/compaction.ts): whole-request estimation, output/buffer reservation, recent content plus a rolling summary, and a tool-free summary request; its defaults are a recent-content budget of 8000 and a summary output cap of 4096 tokens.
- [OpenCode overflow recovery](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/runner/llm.ts): overflow recovery is only attempted before assistant output has started, and the recovered call does not re-enter the same overflow recovery path.

ARTEX adopts the independent output budget, recent content with a rolling summary, and bounded recovery. It keeps norma v0.3.6's structured messages and tool pairing instead of copying Grok's text excerpts, and does not write OpenCode's main-session compaction events into the ARTEX main transcript.

## Request budget and execution

- Messages keep norma's per-content-block UTF-8 byte estimate with a 4/3 margin; the system prompt, tool schemas and message-wrapping overhead are counted on top. The estimate is not an exact model token count.
- Side-question output defaults to at most 8192 tokens and never exceeds the output limit already set in the main configuration. The service environment variable `ARTEX_BTW_MAX_OUTPUT_TOKENS` can set a limit between 256 and 32768; the product default model and the main task parameters are not modified.
- The input budget is the context window minus the output limit and a safety margin; an unknown window uses the platform default of 200K. The safety margin is 5% of the window, with a minimum of 128 and a maximum of 8192 tokens.
- Successful Q&A exchanges are loaded in increasing ordinal order, at most 20 per batch. At most 20 exchanges are kept verbatim, with a token budget of at most 1/4 of the input budget and no more than 16K.
- Excess Q&A is folded into the rolling summary. The summary carries the historical source and context time; an earlier assistant answer is not equivalent to new tool evidence, and on conflict the latest main snapshot takes precedence.
- When the main context is still too long, only older messages in the working copy are summarized, keeping at most 8K tokens of recent content; the cut point never splits a tool call from its result. A single oversized group goes into the summary as a whole.
- Summary input is split into UTF-8-safe chunks according to the actual remaining window, with an output cap of 2048 tokens; an empty, truncated, tool-calling or over-budget summary is never written to the cache. A single side question makes at most 12 summary calls and is bound by the same 120-second timeout; reaching the limit fails explicitly instead of looping forever.
- If the model first returns a context-exceeded error and has not yet output text or a tool call, recovery retries at most once after further trimming; if the estimated size did not decrease, recovery stops immediately. Other model errors and partial streamed output do not trigger this recovery.
- All usage obtained, including summaries, failed attempts and usage at cancellation, is accumulated onto the same side-question request. When the Provider returns no usage, only zero can be recorded; an estimate is never passed off as actual usage.

## Persistence and UI

`side_question_sessions.memory` stores the old Q&A summary, the ordinals it covers, and the main-context summary cached by snapshot identity. `side_question_requests.context_info` stores the preparation phase, the number of groups actually replayed, summary usage and the budget estimate.

A summary is only saved while the original request is still running and the clear version matches; clearing also clears the cache, and a late write does not restore cleaned data. A new snapshot does not reuse an old snapshot's summary. The summary fields are saved with v3 task archives; when restoring an old v3 archive that lacks the fields an empty object is filled in, and v1/v2 remain compatible.

POST first admits and returns the request, and preparation and compression run in the background without holding the admission lock or a database transaction. SSE/history show the phases preparing, summarizing Q&A, compressing the copy and answering; a compression failure is saved as the failed final state of that side-question request. The frontend keeps the error and restores the draft of the failed question, without covering the input box with a floating notification; history polling no longer clears the submission error.

## Validation record

- 19/20/21/50-group replay, retention of old conclusions beyond 20 groups, and reuse of the summary cache after restart: automated tests pass.
- Very long Chinese answers and code context, chunked request budgets, tool pairing, unchanged snapshots and cache invalidation on a new snapshot: automated tests pass.
- Summary failure/cancel/truncation/over-length/tool return, clear races, the call cap, exactly one overflow recovery and no retry on partial streams: automated tests pass.
- On a separate PostgreSQL: paging, restart, v1/v2/v3 archives, restore of summary cache and budget metadata from archives, old v3 archives missing the new fields, and 20 parent sessions sharing four concurrency slots: passed.
- Go candidate service build, frontend TypeScript check, Biome check of the modified components, and a Next.js production build in a separate directory: passed.
- The built-in browser used a separate UI fixture at 1280×720 and 390×844 to verify the preparing phase, the summary-scope hint, draft restore after failure, no floating error notification, no horizontal overflow and no console errors. The temporary fixture was removed.
- A read-only replay of existing local Worker snapshots passed the new budget check; for example, Worker #3's 293085-character snapshot is no longer wrongly rejected by the local character count. No external model call was made for this item.
- The real conversation test that sends a private Worker snapshot to Grok was rejected by the automatic approval review, was not run, and is not counted as a pass.
- At 2026-09-11 00:37 the local backend was restarted at the user's request, keeping the original database, data directory and login configuration. The running file and the candidate binary have identical SHA-256, and `/api/health` returned healthy on both the backend and the frontend proxy.

Verification commands (use only a separate test database):

```sh
go test -race ./sidequestion ./db ./server -run 'TestSide|TestCheckpoint|TestSnapshot|TestBuildRequest|TestService|TestMainSide|TestTaskArchive' -count=1
go build ./cmd/artex
npx tsc --noEmit
npm run build -- --webpack
```

The frontend production build uses a separate copy so the currently previewed `.next` is not overwritten. The candidate service is at `/private/tmp/artex-btw-budget-candidate`, was copied to `/private/tmp/artex-btw-preview/artex` and started; the original binary was backed up in the same directory as `artex.before-context-budget`.
