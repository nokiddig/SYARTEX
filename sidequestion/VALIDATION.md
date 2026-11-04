# `/btw` validation record

Date: 2026-09-10. Branch: `codex/btw-side-question`. Baseline: `8dae851b9b622f2ff2631f332fde9719d0b16fba`.

A separate PostgreSQL test database and data directory were used; real model credentials were injected only into the separate test environment, were not written to code or this record, and the product default model was not modified. Go 1.26.3, norma v0.3.6, Next.js 16.2.9.

The actual model conversations, returned objects, engineering assertions and the raw Qwen review text are saved in [validation-2026-09-10.json](validation-2026-09-10.json), which contains no API credentials.

## Engineering checks

| Scope | Result | Evidence |
| --- | --- | --- |
| Structured messages, deep copy of tool arguments | Passed | `TestCheckpointDeepCopyAndBoundaries` |
| Summary / compression requests do not overwrite, complete replies and final-state publishing, partial replies excluded | Passed | `TestCheckpointDeepCopyAndBoundaries`, `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Identity of the actual model-pool member | Passed | `TestSnapshotExcludesPartialStreamAndSelectsPoolMember` |
| Tool pairing, 20-group replay, budget trimming and over-limit error | Passed | `TestBuildRequestCompactionToolPairingAndBudget` |
| Main and side run in parallel, two-way cancellation isolation | Passed | Blocking Provider, `TestMainSideConcurrencyAndIndependentCancellation` |
| No tool execution, streaming / non-streaming, usage kept on failure | Passed | `TestServiceNoToolsAndUsageOnFailure` |
| Real norma ChatAgent + local Read tool, main transcript / activity isolation | Passed | `TestSideActualChatCheckpointToolResultAndTranscriptIsolation`, streaming and non-streaming sub-cases |
| Persistence, paging, idempotency, partial answer kept across restart | Passed | `TestSideHistoryIdempotencyPagingAndRecovery` |
| Clear vs late-write race, parent resource deletion, version comparison | Passed | `TestSideClearLateWritersAndDeletedParent` |
| MainAgent / Worker archive and restore, v1/v2/v3 | Passed | `TestSideTaskArchiveVersions` |
| Three parent endpoints, authentication, resource ownership, Worker logical deletion | Passed | `TestSideHTTPGlobalLimitTaskWorkerAndDeletion`, `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Busy main session can still ask side questions, independent SSE reconnect / disconnect, cancel, clear | Passed | `TestSideHTTPBusyIsolationClearAndReconnect` |
| Concurrency of 1 per parent session / 4 globally | Passed | Two `TestSideHTTP…` cases |
| Snapshot saved before submission, follow-up after restart, legacy sessions cannot forge a snapshot | Passed | `TestSideCheckpointPersistsBeforeAdmissionAndRestart` |
| Refuse to continue after a cached configuration is deleted or the model changes | Passed | `TestSideRejectsDeletedOrChangedCachedProfile` |
| Cancel before archiving and wait for the final answer and usage to be persisted | Passed | `TestSideTaskDrainPersistsBeforeArchive` |
| Usage and side-question attribution recorded only once when the streaming consumer cancels early | Passed | `TestSideUsageRecordedOnceOnConsumerCancellation` |
| Worker / deadline runtime context restored automatically after restart keeps publishing new snapshots | Passed | `TestSideRestoredWorkerRuntimePublishesNewCheckpoint` |
| Race check of the related packages | Passed | Commands below |
| TypeScript and production build | Passed | `npx tsc --noEmit`, `npm run build` |
| Biome on the new frontend modules | Passed | `biome check`, 3 new modules |

With `ARTEX_PG_DSN` pointing at a separate disposable database, the automated checks can be reproduced (never point it at a production database):

```sh
go test -race ./agent ./db ./server ./sidequestion ./llmrec ./llmpool \
  -run 'Test(Side|Checkpoint|Snapshot|BuildRequest|Service|MainSide|CaptureRun|TaskArchive|CompleteForwards|StopIntent|CancelIntent)' -count=1
cd web
npx tsc --noEmit
npx biome check src/lib/side-questions.ts src/hooks/use-side-questions.ts src/components/side-question-workspace.tsx
npm run build
```

The full Go regression is not all green: two existing tests in the `server` package fail during temporary-directory cleanup, both reporting `TempDir RemoveAll … directory not empty`:

- `TestInheritedActivityDetailAndRelationDeletion`
- `TestTaskMetadataPatchReturnsRenameAndPin`

After exporting the source from the unmodified baseline above and rerunning the `server` package in the same isolated environment, these two cleanup failures also reproduce. The baseline run additionally had a failing target-node-count assertion in `TestCoreTaskLifecyclePG`; the final modified `server` regression does not have that assertion failure. Other packages pass, and the side-question test cases and race checks pass. Baseline issues were not marked as passing for this acceptance, and no existing assertions were changed to hide problems.

The Next.js build prints the existing warning about inferring the workspace root from multiple lockfiles; the build completes and all pages are generated successfully.

## Browser checks

Using the Codex In-app Browser connected to a separate local Go service and Next.js dev server. On desktop and a 390 × 844 narrow screen, the following manual-automation steps were done, checking screenshots and browser logs:

- While a regular chat is running, typing `/btw` shows the main content and the side question at the same time; the desktop side panel works.
- Consecutive follow-ups; after stopping a side question the generated part is kept; the main flow continues.
- Closing the panel keeps the request running, and reopening restores the completed answer; after a page refresh an empty `/btw` restores the history.
- On the narrow-screen Drawer the input, buttons, history and close action work, with no horizontal overflow.
- Clear uses a confirmation dialog; after clearing the history disappears while the main transcript and snapshot are kept.
- The task MainAgent and two Workers were asked in turn and switched between; the Agent tabs and histories did not get mixed up.
- A blocking local model fixture kept a Worker running; submitting `/btw` from the Worker's main input and stopping the side question left the Worker still showing live running and its own pause button, and the side question kept its partial answer.
- The browser error / warning log is empty.

The controllable fixture is used to verify concurrency timing precisely, independent of a real model's output speed. During debugging, two Worker-running checks did not form a valid concurrency window (the task had ended / the answer ended early); they were redone and passed after the fixture was fixed, and the initial attempts are not counted as valid passes.

## Real model conversations

`grok-4.6` was probed first, via the OpenAI-compatible endpoint `http://127.0.0.1:12580/tingly/openai`. The probe returned HTTP 200 with model name `grok-4.6` and `READY`, in 2.82 seconds. The first choice was available, so the Tingly `glm` and Zhipu `glm-5.3` fallback chains were not enabled; these two fallback services were not verified this time.

| Scenario | Actual result |
| --- | --- |
| Asking about the asset, goal and marker while the main session runs | Returned `redhaze.top`, the goal of reading and summarizing the homepage, and `BTW-REAL-0910`; the side question completed in 16.97 seconds |
| Asking for the tool evidence after the main session finished reading the homepage | Correctly cited WebFetch 200, the curl redirects 301 → 302 → 200, and the page title; 7.24 seconds |
| Side question asks Bash to create a test file | Refused to execute and the target file was not created; 7.74 seconds |
| A completed side question does not change the main context | The main transcript SHA-256 and the main activity record stay identical; side tool executions: 0 |
| Follow-up after truly stopping / restarting the Go service | Kept the earlier 3 side-question histories and answered the asset, marker and title directly from the persisted snapshot, without rerunning the main Agent |
| New session using the Grok non-streaming configuration | Correctly answered the asset and `ATOMIC-0910`; usage returned and saved: input 11734, output 138, cache_read 11520 |

<!-- intentionally Chinese: the page title below is verbatim page data captured from the real site, not translated -->

In the asset case the main session read the public homepage with WebFetch and Bash/curl; the landing page was `https://id.redhaze.top/home` and the title was "红幕科技 RedHaze Group · 全球综合集团门户". Bash staged the response in a local test file; nothing was written to the remote side. This fact is verified separately from "the side question executed no tools".

Main transcript checksum: `e7e61f135a4a120954b539f357e8c4205d7d5cd7460dcaf3dc0fd066463e1d00`.

**Usage limitation:** Tingly's Grok streaming response returned no usage. Separately sending `stream_options.include_usage=true` directly was also checked: HTTP 200, 12 data frames, 0 usage frames. So a 0 in the streaming test means the endpoint did not provide usage and must not be read as no billing. Non-streaming usage and the fixture's failure / cancellation usage were saved correctly.

## Qwen review

Review model `qwen-flash`, OpenAI-compatible endpoint `https://dashscope.aliyuncs.com/compatible-mode/v1`, HTTP 200. It was given the first three real side-question conversations, the main-session tool evidence and the engineering assertions; it returned `verdict: accept` and `concerns: []`, judging that the answers match the evidence for the asset, marker and page read, and that the side-question tool refusal meets the constraints. Review usage: prompt 6625, completion 312, total 6937.

This Qwen review does not cover the later-added service restart and non-streaming tests. Qwen's summary of "no writes" was too broad: the main-session curl did create a local temporary response file, as recorded above. Concurrency, zero tool executions and transcript isolation are judged by engineering assertions; the model review only assists in assessing answer quality.
