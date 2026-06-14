# Research: session-transcript

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code persists every conversation as an append-only JSONL transcript, one file per session, at $CLAUDE_CONFIG_DIR/projects/<encoded-cwd>/<session-id>.jsonl (default ~/.claude). Each line is one JSON object — a user message, assistant response, system event, hook progress, queued input, or file-history snapshot — and every record carries a uuid plus parentUuid, forming a DAG/linked-list rather than a flat log. Long sessions are split by "compact_boundary" segments that inject a synthetic summary user message and reset the parent chain; cross-file continuation is detected by a sessionId that changes mid-file while parentUuid bridges the gap. Resume (--continue/--resume <id|name>), fork (--fork-session or /branch), and rewind (/rewind, double-Esc) all operate by walking this parentUuid chain and (for code rewind) the file-history-snapshot entries. The SDK's SessionStore interface is a dual-write mirror of the same JSONL entries (local disk first, then append()) and cannot be combined with persistSession:false or enableFileCheckpointing.

## Components
### On-disk layout & project key encoding
**Purpose:** Determines the physical path each session transcript is written to and how the directory name is derived from the working directory.

**Mechanism:** On session start Claude Code derives an encoded directory name from the absolute working directory by replacing every non-alphanumeric character with '-' and creates (or opens) ~/.claude/projects/<encoded-cwd>/<new-session-uuid>.jsonl. Each line is appended as a self-contained JSON object; the file is append-only and never truncated/rewritten. Resume resolves the encoded dir from cwd, then scans for the target session-id (or the most-recently-modified one for --continue). Moving a session with /cd (v2.1.169+) relocates the file into the new directory's project storage. Session-ID lookup is scoped to the current project dir + its git worktrees; a session created elsewhere yields 'No conversation found with session ID: <id>'.

**Data model:** Path layout: $CLAUDE_CONFIG_DIR/projects/<encoded-cwd>/<session-id>.jsonl + subagent sidecars under subagents/agent-<id>.jsonl and file-history snapshots. Encoded-cwd = absolute cwd with every non-alphanumeric char replaced by '-' (e.g. /Users/me/proj -> -Users-me-proj); confirmed by docs and GitHub issues: non-ASCII chars collapse to '-' too (issue #19972), and even underscores get replaced (issue #39424), so two distinct paths can collide. session-id is a random UUID; the filename stem MUST equal the sessionId field on every line.

**Config:** CLAUDE_CONFIG_DIR relocates the entire ~/.claude root. cleanupPeriodDays (settings.json, default 30, min 1, 0 rejected) sweeps stale files at startup and also sweeps orphaned subagent worktrees. CLAUDE_CODE_SKIP_PROMPT_HISTORY=1 / --no-session-persistence / persistSession:false suppress writes. There is no disable for cleanup, only delay (set 99999 for ~274 years).

### Transcript entry schema (common fields)
**Purpose:** Defines the shape of each JSONL line so the chain can be reconstructed for resume/rewind/fork.

**Mechanism:** Every line carries type, uuid, parentUuid, sessionId, timestamp, plus optional cwd/version/gitBranch. uuid is a per-record identifier; parentUuid points to the PRECEDING record's uuid, building a linked list / directed-acyclic-graph (in practice a tree) — this is what makes resume, rewind, and fork possible. The first record's parentUuid is null. Because it's a DAG not a flat log, the same file can represent branching (forks written into a new file but sharing prefix uuids). On the SDK SessionStore path, entries are emitted as SessionStoreEntry objects = opaque JSON-safe values one-per-line.

**Data model:** { type, uuid, parentUuid, sessionId, timestamp, cwd, version, gitBranch, plus type-specific fields }

**Config:** ISO-8601 UTC timestamps. version field carries the Claude Code release that wrote the line. gitBranch captured per-line for the Ctrl+B branch filter.

### Message types: user & assistant
**Purpose:** The two conversational record kinds; everything else is metadata around them.

**Mechanism:** Type 'user': message.role='user', content is EITHER a plain string OR an array of content blocks; tool results come back as a block { type:'tool_result', tool_use_id, content:string|text/image-block-array, is_error }. Extra user fields: userType ('external' for human input), todos (current task-list snapshot), permissionMode. Type 'assistant': message is the full API response with model, role, content (array of {type:'text',text} / {type:'tool_use',id,name,input} / {type:'thinking'} blocks), stop_reason, usage, id; extra field requestId. Compaction summary is a user-typed line with isCompactSummary:true, isVisibleInTranscriptOnly:true and content beginning 'This session is being continued from a previous conversation that ran out of context.'

**Data model:** { type:'user'|'assistant', message:{ role, content, [usage, model, stop_reason, id] }, subtype, user/assistant-only fields }

**Config:** userType distinguishes human vs system-injected. todos field persists the structured Task list state alongside the message. permissionMode records the session's permission level.

### Metadata record types: system, progress, queue-operation, file-history-snapshot
**Purpose:** Non-conversational events written into the same JSONL so the transcript is a complete execution log.

**Mechanism:** Type 'system': carries subtype. Notable subtypes: 'compact_boundary' (the compaction marker — see Compaction component), 'stop_hook_summary' (end-of-turn hook results: hookCount, hookInfos[command+duration], hookErrors, preventedContinuation, stopReason), and (SDK mirror) 'mirror_error'. Type 'progress': hook execution events; data.type e.g. 'hook_progress', data.hookEvent (e.g. 'PostToolUse'), data.hookName (e.g. 'PostToolUse:Bash'), data.command. Type 'queue-operation': operation:'enqueue', content = queued user text while the assistant was mid-turn. Type 'file-history-snapshot': snapshot.trackedFileBackups = map of file path -> backup state, used by /rewind to restore file trees.

**Data model:** system subtype set includes: compact_boundary, stop_hook_summary, mirror_error (SDK sessionStore failure). progress.data: { type:'hook_progress', hookEvent, hookName, command }.

**Config:** Hook events keyed by hookEvent (PreToolUse/PostToolUse) and hookName (e.g. PostToolUse:Bash). queue-operation records input-buffered text.

### Compaction segments (within a single file)
**Purpose:** Keeps long sessions running past the context window by periodically summarizing and resetting the active chain, while preserving the original transcript.

**Mechanism:** When context approaches the model's limit (~167K observed), Claude Code writes a system record { type:'system', subtype:'compact_boundary', logicalParentUuid:<last-msg-uuid-before-compaction>, parentUuid:null, content:'Conversation compacted', compactMetadata:{ trigger:'auto'|'manual', preTokens:<token-count> } }. The referenced pre-compaction uuids are dropped from the active context. Immediately after, it appends a synthetic user message with isCompactSummary:true, parentUuid pointing at the boundary uuid, content = an LLM-generated summary of everything so far. A single file can contain MANY boundaries (observed 5 in a 21-hour session, compacting ~every 2h). getSessionMessages returns the post-compaction chain only (e.g. 18 msgs from 503 raw entries); raw history must be read via store.load().

**Data model:** Boundary: { type:'system', subtype:'compact_boundary', logicalParentUuid, parentUuid:null, content:'Conversation compacted', compactMetadata:{ trigger:'auto'|'manual', preTokens:number } }

**Config:** CLAUDE_CODE_AUTO_COMPACT_WINDOW + CLAUDE_AUTOCOMPACT_PCT_OVERRIDE tune the trigger. preTokens lets external tools know how close to the limit the session was.

### Cross-file session continuation (continuation files)
**Purpose:** Allows a single logical conversation to span multiple JSONL files when a session is resumed into a new file.

**Mechanism:** Sometimes a fresh session-id file is created that logically continues an earlier session. The new file's first lines carry the PARENT session's sessionId (a byte-for-byte duplicate of the parent's trailing compact_boundary + messages), then at some line the sessionId switches to the new file's own id; that switch point's record has parentUuid bridging into the parent's last record. Detection is STRUCTURAL — there is no parentSessionId/resumedFrom field: extract session-id from the filename; if the first record's sessionId differs, the first id is the parent and only records whose sessionId == filename id belong to THIS file (prefix ones are duplicates to skip). A shared slug field (human-readable name, e.g. 'zesty-singing-newell') persists across continuations.

**Data model:** File d621b0b1.jsonl contains: lines[0..N] with sessionId=d8af951f (parent, skip as duplicates) then lines[N+1..] with sessionId=d621b0b1 (this file's own). shared slug across both files.

**Config:** slug is the cross-file conversation identifier. Continuation prefix lines are byte-duplicates of parent's tail — dedup by sessionId.

### SessionStore mirror (SDK external storage)
**Purpose:** Mirrors transcript lines to an external backend (S3/Redis/Postgres) so sessions resume across hosts; defines the formal append/load contract the Go impl should mirror.

**Mechanism:** SDK options.sessionStore replaces/augments local storage. projectKey = the same stable filesystem-safe cwd encoding; sessionId = session uuid; subpath set for subagent/sidecar transcripts ('subagents/agent-<id>'). append(key,entries[]) called after each local batch; load(key) called once before subprocess spawn on resume. Dual-write: Claude Code subprocess ALWAYS writes local disk first, then forwards the batch to append(). If append rejects/times out, error is logged and a {type:'system',subtype:'mirror_error'} is emitted into the iterator; query continues (local copy is durable); failed batches are NOT retried. load must return entries deep-equal to appended (byte-equal not required). forkSession rewrites all sessionId fields + remaps uuids, then appends under a new key (NOT a byte/copy-object shortcut). Cannot combine sessionStore with persistSession:false (throws) nor with enableFileCheckpointing (throws — file-history blobs are local-disk-only).

**Data model:** SessionKey={ projectKey:string, sessionId:string, subpath?:string }; subpath e.g. 'subagents/agent-<id>' is opaque key suffix following on-disk layout.

**Config:** Python SDK always persists; TypeScript-only persistSession:false for ephemeral. mirror_error system msg emitted (not retried) on append failure. SessionStore key includes subpath for sidecars.

### Subagent transcripts & sidecar files
**Purpose:** Stores per-subagent conversation logs and supporting artifacts under the same project dir.

**Mechanism:** Each subagent (Task tool) gets its own transcript at subpath 'subagents/agent-<id>' (relative to the session directory). listSubagents requires the store's listSubkeys; getSubagentMessages uses listSubkeys when available else falls back to direct subpath. On resume, listSubkeys is called to restore subagent files; without it only the main transcript is materialized. Other sidecars include file-history snapshots for /rewind and the session summary. Subagent transcripts are excluded from --resume/--continue pickers and claude agents list when spawned under CLAUDE_CODE_CHILD_SESSION (v2.1.172+).

**Data model:** Sibling/sidecar files alongside <session-id>.jsonl in the project dir; listSubkeys enumerates them for resume.

**Config:** Main file = main conversation. subagents/agent-<id>.jsonl for each subagent. Permission decisions, summaries, and snapshots all sidecar'd under the same session dir.

## Key behaviors
- project dir name = absolute cwd with EVERY non-alphanumeric char replaced by '-' (collapses underscores and non-ASCII, so non-ASCII paths fragment/collide — known issue #39424, #19972).
- --continue resumes most-recently-modified session for the current dir; --resume opens picker, or resumes by exact name (ambiguous name => picker with name prefilled) or by raw session-id. /resume <name> on ambiguity ERRORS instead of opening picker.
- session-id lookup is scoped to current project dir + its git worktrees; --resume from a different cwd reports 'No conversation found with session ID: <id>'. Session picker Ctrl+W widens to all worktrees, Ctrl+A to all projects.
- --fork-session + (--continue|--resume) OR /branch create a copy: prints BOTH new and original session ids, original stays in picker. 'Allow for this session' permissions do NOT carry into the fork. Resuming the same session in two terminals without forking INTERLEAVES into one transcript.
- Transcript file is append-only and never truncated/rewritten, even through /clear and compaction; /clear starts a fresh context but the old transcript remains resumable.
- Default cleanup: 30 days at startup; minimum 1; setting 0 is REJECTED with a validation error; you cannot disable deletion, only delay it (99999 ~= 274 years). cleanup also sweeps orphaned subagent worktrees.
- claude -p / Agent SDK sessions DO NOT appear in the session picker but are resumable by explicit id. Python SDK ALWAYS persists to disk; only TypeScript supports persistSession:false (in-memory only) and that cannot coexist with sessionStore.
- Compaction is detectable structurally: compact_boundary sets parentUuid:null + logicalParentUuid; the following user msg has isCompactSummary:true and content starting 'This session is being continued from a previous conversation that ran out of context.' Re-feeding isCompactSummary lines as real dialogue is a classic bug — skip them.
- Checkpoints (/rewind, double-Esc) revert CODE+conversation/conversation-only/code-only or summarize from/up to a point. Only edits via Claude's Write/Edit/NotebookEdit are tracked — Bash-driven file changes (rm/mv/cp) and external edits are NOT tracked. Original messages are always preserved in transcript even after summarize.
- CLAUDE_CODE_CHILD_SESSION (v2.1.172+) marks nested sessions and auto-excludes them from --resume/--continue/up-arrow history/agents list; CLAUDE_CODE_FORCE_SESSION_PERSISTENCE=1 overrides; honored on v2.1.169 and earlier, removed in v2.1.170-2.1.171.

## External interfaces
- CLI flags: --continue (alias -c), --resume (alias -r) [<name|session-id>], --fork-session, --from-pr <number>, --no-session-persistence, -n <name>
- In-session commands: /resume [<name>], /rename <name>, /branch [<name>], /rewind, /clear, /compact [instructions], /export [filename]
- Env vars: CLAUDE_CONFIG_DIR, CLAUDE_CODE_SKIP_PROMPT_HISTORY, CLAUDE_CODE_CHILD_SESSION (v2.1.172+), CLAUDE_CODE_FORCE_SESSION_PERSISTENCE, CLAUDE_CODE_AUTO_COMPACT_WINDOW, CLAUDE_AUTOCOMPACT_PCT_OVERRIDE
- settings.json keys: cleanupPeriodDays (default 30, min 1, 0 rejected)
- SDK options: resume:<id>, continue:true, fork_session:true, persistSession:false, sessionStore, enableFileCheckpointing
- SDK result message fields: session_id, subtype; SystemMessage carries session id early (TS direct field, Python nested in data)
- SDK functions: listSessions(), getSessionInfo(), getSessionMessages(), renameSession(), tagSession(), deleteSession(), forkSession(), listSubagents(), getSubagentMessages()
- File path scheme: $CLAUDE_CONFIG_DIR/projects/<encoded-cwd>/<session-id>.jsonl (+ subagents/agent-<id>.jsonl)

## Open questions
- Exact set of all current system subtypes beyond compact_boundary / stop_hook_summary / mirror_error (e.g. tool approval, timing, init) — would require reading the latest claude-code-sdk source.
- Precise algorithm for slug generation (the human-readable name shared across continuation files) and where it is stored on each line.
- Exact JSON schema of file-history-snapshot.trackedFileBackups entries and how /rewind maps a snapshot to a restore point in the DAG.
- Whether sessionId lines that differ from the filename in a continuation file are byte-for-byte identical to the parent's tail or lightly transformed (the writeup claims byte-identical; confirm against source).

## Sources
- [Manage sessions - Claude Code Docs (code.claude.com)](https://code.claude.com/docs/en/sessions) — Official source for --continue/--resume/--fork-session/--from-pr, /branch, /rewind, /rename, picker shortcuts (Ctrl+W/A/B), /export, and the exact transcript path ~/.claude/projects/<project>/<session-id>.jsonl + cleanupPeriodDays default + CLAUDE_CONFIG_DIR.
- [How Claude Code Session Continuation Works - Massively Parallel Procrastination](https://blog.fsck.com/agent-blog/2026/02/22/claude-code-session-continuation/) — Deepest technical source for the JSONL record schema (user/assistant/system/progress), parentUuid DAG, compact_boundary fields (logicalParentUuid, parentUuid:null, compactMetadata.trigger/preTokens), isCompactSummary, and cross-file continuation detection algorithm + slug field.
- [docs/claude-code-transcript-format.md - kent/consciousness forge](https://evilpiepirate.org/forge/kent/consciousness/src/commit/6a7ec9732b8f6964f07e112b27eda8b4fa6920f7/docs/claude-code-transcript-format.md) — Concise field reference: common fields (uuid/parentUuid/sessionId/timestamp/cwd/version/gitBranch), tool_result content blocks, assistant usage/stop_reason/requestId, system subtypes (stop_hook_summary), progress/queue-operation/file-history-snapshot types, compaction segment model.
- [Persist sessions to external storage (SessionStore) - Claude Code Docs](https://code.claude.com/docs/en/agent-sdk/session-storage) — Authoritative SessionKey/SessionStore/SessionStoreEntry contract, subpath 'subagents/agent-<id>', dual-write-first-to-disk semantics, mirror_error, forkSession uuid-rewrite (not byte copy), persistSession:false incompatibility, getSessionMessages returns post-compaction chain.
- [Work with sessions (Agent SDK) - Claude Code Docs](https://code.claude.com/docs/en/agent-sdk/sessions) — Official encoded-cwd rule (every non-alphanumeric char -> '-', /Users/me/proj -> -Users-me-proj), continue vs resume vs fork semantics, session_id on result/SystemMessage, resume-across-hosts mechanics.
- [Checkpointing - Claude Code Docs](https://code.claude.com/docs/en/checkpointing) — Official /rewind behavior, checkpoint = per user prompt, persists across sessions, 30-day cleanup, only Write/Edit/NotebookEdit tracked (Bash/external not tracked), summarize from/up-to here.
- [Claude Code settings - Claude Code Docs](https://code.claude.com/docs/en/settings) — Exact cleanupPeriodDays semantics: default 30, minimum 1, 0 rejected with validation error, also governs orphaned subagent worktree removal; worktree.baseRef/symlinkDirectories settings.
- [Environment variables - Claude Code Docs](https://code.claude.com/docs/en/env-vars) — Definitive env-var surface: CLAUDE_CODE_SKIP_PROMPT_HISTORY, CLAUDE_CODE_CHILD_SESSION (v2.1.172+), CLAUDE_CODE_FORCE_SESSION_PERSISTENCE, CLAUDE_AUTOCOMPACT_PCT_OVERRIDE, CLAUDE_CODE_DEBUG_LOGS_DIR default ~/.claude/debug/<session-id>.txt.
- [Don't let Claude Code delete your session logs - Simon Willison](https://simonwillison.net/2025/Oct/22/claude-code-logs/) — Independently confirms ~/.claude/projects/encoded-directory/*.jsonl location, the 30-day deletion default (github issue 4172), and the cleanupPeriodDays:99999 workaround (cannot disable, only delay).
- [[FEATURE/BUG] project path encoding - anthropics/claude-code#19972](https://github.com/anthropics/claude-code/issues/19972) — Confirms the encoding replaces non-alphanumeric (and non-ASCII) chars with '-', causing collisions and readability loss for non-ASCII paths.
