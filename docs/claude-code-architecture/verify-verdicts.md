## memory-claudemd
- [confirmed] Managed-policy CLAUDE.md precedence: managed (highest) → CLI args → local → project → user (lowest); the managed CLAUDE.md (file or the managed-only `claudeMd` settings key) cannot be excluded by claudeMdExcludes, and the Windows legacy path C:\ProgramData\ClaudeCode\managed-settings.json was removed in v2.1.75 (now C:\Program Files\ClaudeCode\).
  evidence: All four sub-claims are confirmed verbatim by primary sources.

(1) Precedence ordering — docs.claude.com/docs/en/settings, section "How scopes interact": "1. Managed (highest) - can't be overridden b
## streaming-protocol
- [confirmed] The headless final event is type=="result" with subtype "result" (or "success"/"error" variants) — NOT "message_stop". message_stop is the Messages-API SSE terminal event inside a stream_event, distinct from the ResultMessage that ends stream-json. Known bug #1920: missing result event hangs consumers.
  evidence: Three authoritative sources confirm the core claim. (1) Headless docs (https://code.claude.com/docs/en/headless) document `--output-format stream-json` and the headless/SDK spec (quoted in issue #1920
  CORRECTION: The headless (stream-json / Agent SDK) conversation is terminated by a top-level event of type=="result" with subtype "success" (or an error variant such as "error") — NOT "message_stop". `message_stop` is a Messages-API SSE event that marks the end of a single message; in stream-json it arrives inside a StreamEvent (top-level type: "stream_event") and precedes the AssistantMessage and ultimately the final ResultMessage, which is what actually ends the stream. Known bug anthropics/claude-code#1920: Claude Code intermittently fails to emit the final {"type":"result",...} event in stream-json mode, which hangs SDK consumers indefinitely.
## system-prompt-assembly
- [confirmed] CLAUDE.md IS NOT IN THE SYSTEM PROMPT: official docs state CLAUDE.md/CLAUDE.local.md content is injected into the conversation as a USER message (project context), not into the system prompt; it therefore does NOT affect system-prompt cache entries. The exception is excludeDynamicSections (TS) / exclude_dynamic_sections (Python), added claude-agent-sdk v0.2.98 / v0.1.58, which moves the env-info block from the system prompt into the first user message.
  evidence: The official Claude Code Agent SDK docs (code.claude.com/docs/en/agent-sdk/modifying-system-prompts) state verbatim: "CLAUDE.md takes a different path: the SDK reads it and injects its content into th
## agent-loop
- [confirmed] Token-budget auto-continue: COMPLETION_THRESHOLD=0.9 (stop at >=90% used) and DIMINISHING_THRESHOLD=500 tokens — early stop requires >=3 continuations AND both current+previous deltas <500. Subagents ALWAYS stop (budget is top-level only). The nudge is an isMeta user message. Source: claude-code-from-source.com ch05 + inematds/claudecode-manual 04-query-engine.md.
  evidence: Confirmed against three independent primary sources that all trace back to the same upstream file (openclaudecode/src/query/tokenBudget.ts).

(1) openonion/claude-code TS rewrite (https://github.com/o
## context-compaction
- [confirmed] API microcompact uses clear_tool_uses_20250919 with DEFAULT_MAX_INPUT_TOKENS=180,000 trigger and DEFAULT_TARGET_INPUT_TOKENS=40,000 (clear_at_least = 140,000); clear_thinking_20251015 with keep:'all' is emitted whenever hasThinking && !isRedactThinkingActive.
  evidence: The deobfuscated Claude Code source `services/compact/apiMicrocompact.ts` (mirrored at github.com/leaf-kit/claude-analysis and claude-code-os.vercel.app) confirms every figure. Constants: `const DEFAU
  CORRECTION: Claim confirmed. One caveat the claim omits (without contradicting it): the clear_tool_uses_20250919 strategy is emitted only when process.env.USER_TYPE === 'ant' AND env flags USE_API_CLEAR_TOOL_RESULTS or USE_API_CLEAR_TOOL_USES are truthy; the clear_thinking_20251015 strategy is emitted for all users whenever hasThinking && !isRedactThinkingActive (switching to keep:{type:'thinking_turns',value:1} when clearAllThinking is set).
## tool-exec-engine
- [refuted] Permission rule evaluation order is deny -> ask -> allow (first match wins, specificity does not change order); rules format 'Tool' or 'Tool(specifier)' with Bash wildcards where a space before * enforces a word boundary; oversized tool results persist to ~/.claude/tool-results/{hash}.txt and MCP default persist threshold is 25000 chars (hard ceiling 500000 via _meta anthropic/maxResultSizeChars)
  evidence: Most sub-claims are confirmed verbatim by https://code.claude.com/docs/en/permissions: "Rules are evaluated in order: deny, then ask, then allow. The first match in that order determines the outcome, 
  CORRECTION: Permission rule evaluation order is deny -> ask -> allow (first match wins, rule specificity does not change the order); rules use the format 'Tool' or 'Tool(specifier)'; Bash specifiers support glob wildcards where a space before a trailing * (e.g. Bash(ls *)) enforces a word boundary, while Bash(ls*) does not; the _meta["anthropic/maxResultSizeChars"] override has a hard ceiling of 500,000 characters. HOWEVER, the documented default MCP output cap is 25,000 TOKENS (via MAX_MCP_OUTPUT_TOKENS), not 25,000 chars — the docs do not publish a default char-based persist-to-disk threshold. Oversized results ARE persisted to disk and replaced with a file reference, but the official docs do not document the exact path ~/.claude/tool-results/{hash}.txt; that path/hash-scheme is implementation detail not stated in authoritative docs.
## session-transcript
- [confirmed] Every transcript line carries a parentUuid (not just uuid), forming a DAG/linked-list; compact_boundary records set parentUuid:null and carry logicalParentUuid referencing the now-erased pre-compaction last message, immediately followed by a user message with isCompactSummary:true whose content starts with "This session is being continued from a previous conversation that ran out of context."
  evidence: Primary source (blog.fsck.com technical guide, 2026-02-22) confirms every sub-assertion verbatim. (1) Linked-list: "The `parentUuid` field chains records into a linked list — each record points to the
## mcp
- [uncertain] MCP_TOOL_TIMEOUT default is ~28 hours; MAX_MCP_OUTPUT_TOKENS default is 25000 with a 10000-token warning threshold; per-server 'timeout' values below 1000 ms are ignored (fall through to MCP_TOOL_TIMEOUT) since v2.1.162 (before that they were floored to 1 second)
  evidence: All three behavioral facts are confirmed by the PRIMARY source (official Claude Code env-vars doc, https://code.claude.com/docs/en/env-vars), which states verbatim:

(1) MCP_TOOL_TIMEOUT: "Timeout in 
  CORRECTION: CONFIRMED: MCP_TOOL_TIMEOUT default is 100000000 ms (~28 hours); MAX_MCP_OUTPUT_TOKENS default is 25000 with a warning threshold at 10000 tokens; for the per-server `timeout` field in .mcp.json, values below 1000 ms are ignored (fall back to MCP_TOOL_TIMEOUT), while for the MCP_TOOL_TIMEOUT env var itself, values below 1000 ms are floored to 1 second. The official docs (code.claude.com/docs/en/env-vars) and changelog confirm both the behavioral change and that sub-1000 ms per-server values were previously floored to a 1-second watchdog. UNVERIFIED: the specific version "v2.1.162" — the official changelog does not let that version be cleanly pinned to this entry; treat the version number as approximate.
## skills
- [confirmed] Plugin skills are namespaced 'plugin-name:skill-name' and cannot conflict with enterprise/personal/project levels; the plugin root SKILL.md is the ONLY case where the frontmatter 'name' field sets the command name (otherwise directory name / filename governs).
  evidence: The official Claude Code Skills docs (https://code.claude.com/docs/en/skills) state verbatim: "Plugin skills use a plugin-name:skill-name namespace, so they cannot conflict with other levels."

On com
## permissions
- [confirmed] Rule syntax gotcha: Bash(ls *) requires the space and enforces a word-boundary (matches 'ls -la' not 'lsof'); Bash(ls*) without space matches both; trailing :* (Bash(ls:*)) is equivalent to trailing ' *' but is ONLY recognized at end of pattern; Read/Edit pattern anchors differ — //path=filesystem root, ~/path=home, /path=project root (NOT absolute!), path/./path=relative to cwd.
  evidence: Official Claude Code docs (code.claude.com/docs/en/permissions, retrieved 2026-06-14, v2.1.x) confirm every assertion verbatim:

(1) Bash word boundary: "The space before * matters: Bash(ls *) matches
## hooks
- [confirmed] PreToolUse uses hookSpecificOutput.permissionDecision (allow/deny/ask/defer) + permissionDecisionReason + updatedInput (NOT top-level decision/reason which is DEPRECATED for this event; legacy approve/block map to allow/deny). Other events (PostToolUse, Stop, UserPromptSubmit, PreCompact, ConfigChange) use TOP-LEVEL decision:'block' + reason. PermissionRequest uses hookSpecificOutput.decision.behavior (allow/deny). PreToolUse hooks fire BEFORE permission-mode checks and can deny even in bypassPermissions mode.
  evidence: The official Hooks reference (https://code.claude.com/docs/en/hooks) confirms every component:

(1) PreToolUse structure & deprecated top-level fields (line 1455, 1485): "Unlike other hooks that use a
  CORRECTION: (Optional precision, not a correction: the top-level decision:'block' events are exactly UserPromptSubmit, UserPromptExpansion, PostToolUse, PostToolUseFailure, PostToolBatch, Stop, SubagentStop, ConfigChange, and PreCompact — i.e., the claim's list (PostToolUse, Stop, UserPromptSubmit, PreCompact, ConfigChange) is correct but not exhaustive. Updatedinput for PreToolUse sits directly under hookSpecificOutput; for PermissionRequest it is inside the decision object.)
## slash-commands-plan
- [confirmed] The 5 ExitPlanMode approval options presented to the user are exactly: 'Approve and start in auto mode', 'Approve and accept edits', 'Approve and review each edit manually', 'Keep planning with feedback', 'Refine with Ultraplan'; each approve option switches the permission mode accordingly.
  evidence: The official Claude Code docs page "Choose a permission mode" (https://code.claude.com/docs/en/permission-modes) renders the ExitPlanMode prompt verbatim as an unordered list with these exact children
  CORRECTION: When Claude exits plan mode, the approval prompt presents exactly these 5 options, in this order: 'Approve and start in auto mode', 'Approve and accept edits', 'Approve and review each edit manually', 'Keep planning with feedback', and 'Refine with Ultraplan for browser-based review' (the full label; 'Ultraplan' links to /en/ultraplan). 'Keep planning with feedback' and the 'Refine...' option are not approvals (they keep you in plan mode). The three approve options switch the session to the permission mode each describes (auto, acceptEdits, default), as the docs state: 'Approving a plan exits plan mode and switches the session to the permission mode each approve option describes.'
## subagents-task
- [confirmed] The Agent tool prompt-only return contract: parent receives ONLY the subagent's final message verbatim as the tool_result (no intermediate tool calls/reasoning); built-in Explore and Plan are one-shot and return NO agentId so they cannot be resumed via SendMessage.
  evidence: Both halves are directly confirmed by official Claude Code docs.

PART 1 (verbatim final-message return, no intermediate tool calls): The SDK docs (code.claude.com/docs/en/agent-sdk/subagents) state v