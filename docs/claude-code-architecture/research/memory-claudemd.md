# Research: memory-claudemd

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's memory subsystem has two parallel, complementary mechanisms. (1) CLAUDE.md files are human-authored instruction files loaded into every session as context (NOT enforced config) via a strict precedence hierarchy: managed-policy → user (~/.claude/CLAUDE.md) → project (./CLAUDE.md or ./.claude/CLAUDE.md) → local (./CLAUDE.local.md), all concatenated root-to-cwd and never overriding each other. CLAUDE.md supports `@path` import syntax (relative resolves against the importing file, not cwd; recursion capped at max depth 4 hops; HTML comments stripped before injection). (2) Auto memory (Claude-written, requires v2.1.59+) lives in ~/.claude/projects/&lt;project&gt;/memory/ keyed by git repo root (shared across worktrees), with MEMORY.md as a pointer-index (first 200 lines OR 25KB loaded into context) and topic .md files surfaced on-demand by a Sonnet side-query. A separate generic API "memory" tool (tool_type memory_20250818, name "memory") exists for SDK clients operating a /memories directory. The `#` prefix in the REPL quick-adds a memory to the relevant CLAUDE.md. CLAUDE.md content is injected as a USER message after the system prompt, and the InstructionsLoaded hook fires whenever any CLAUDE.md or .claude/rules/*.md enters context.

## Components
### CLAUDE.md directory-walk + concatenation order
**Purpose:** Resolve and assemble all CLAUDE.md/CLAUDE.local.md into one context blob, root-to-cwd, no overriding.

**Mechanism:** Claude Code walks up from cwd to (but not including) filesystem root, checking each dir for CLAUDE.md + CLAUDE.local.md. All discovered files are concatenated (not overridden), ordered root-down so cwd-level is read LAST. At each level CLAUDE.local.md is appended after CLAUDE.md. Subdirectory files load lazily on demand when Claude reads files there. Managed-policy + user + project-root files survive /compact (re-read from disk); nested subdir files do NOT auto-reinject.

**Data model:** Files: CLAUDE.md, CLAUDE.local.md. Target size <200 lines (guideline).

**Config:** Path: ./CLAUDE.md (lower precedence) then ./CLAUDE.local.md appended after at same level. Excludable via claudeMdExcludes.

### Settings-scope precedence (managed → user → project → local)
**Purpose:** Determines which scope wins and how CLAUDE.md content is sourced from settings vs files.

**Mechanism:** Managed-policy CLAUDE.md is highest precedence (above CLI args), loaded BEFORE user and project CLAUDE.md, and CANNOT be excluded by claudeMdExcludes. Three delivery mechanisms: server-managed (Claude.ai admin console), MDM/OS plist (macOS com.anthropic.claudecode domain / Windows HKLM\SOFTWARE\Policies\ClaudeCode registry 'Settings' JSON value), file-based managed-settings.json + drop-in managed-settings.d/. Settings precedence overall: Managed > CLI args > Local > Project > User. Permissions MERGE across scopes; most other settings OVERRIDE.

**Data model:** managed-settings.json: {"claudeMd": "Always run make lint\nNever push to main"}. managed-settings.d/*.json merged systemd-style (alphabetical, arrays concat+dedup, objects deep-merged, dotfiles ignored).

**Config:** OS-specific managed paths: macOS /Library/Application Support/ClaudeCode/CLAUDE.md; Linux/WSL /etc/claude-code/CLAUDE.md; Windows C:\Program Files\ClaudeCode\CLAUDE.md. Or in managed-settings.json via the `claudeMd` key (managed/policy scope only; ignored in user/project/local).

### @import expansion + --add-dir
**Purpose:** Compose memory from multiple files; load memory from additional directories.

**Mechanism:** Regex/token expansion of @-prefixed paths inside CLAUDE.md. First-encounter of EXTERNAL imports in a project triggers an approval dialog listing files; if declined, imports stay disabled and dialog does not reappear. AGENTS.md is NOT read natively — bridge via `@AGENTS.md` import or symlink.

**Data model:** Loaded files: CLAUDE.md, .claude/CLAUDE.md, .claude/rules/*.md, CLAUDE.local.md (skipped if local excluded via --setting-sources).

**Config:** Set CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1.

### Auto memory (MEMORY.md index + topic files)
**Purpose:** Claude-written scratchpad: index always loaded, topic files surfaced on-demand.

**Mechanism:** At session start, first 200 lines OR first 25KB of MEMORY.md (whichever first) is loaded into system prompt. Topic files are NOT loaded at startup. Per-turn, a Sonnet side-query scans up to 200 .md files (excluding MEMORY.md), extracts filename/mtime/description/type, returns JSON {selected_memories:[]} (max 256 tokens, up to 5 files), which are injected as `relevant_memories` attachments (NOT FileReadTool calls). Topic files use 2-step save: (1) write file with YAML frontmatter name/description/type, (2) add one-line pointer to MEMORY.md. Background autoDream consolidation fires after >=24h since last consolidation AND >=5 sessions, runs as forked agent, protected by .consolidate-lock PID file with 60-min stale guard.

**Data model:** Files: MEMORY.md (index, <200 lines / 25KB), topic files with frontmatter name/description/type(one of: user, feedback, project, reference). Line format: '- [Title](file.md) — hook' (~150 chars).

**Config:** Settings: autoMemoryEnabled (bool, default true), autoMemoryDirectory (absolute or ~/). Env: CLAUDE_CODE_DISABLE_AUTO_MEMORY=1.

### memory tool (API tool_type memory_20250818)
**Purpose:** Generic file-based memory CRUD primitive (API/SDK clients), distinct from Claude Code's built-in auto-memory.

**Mechanism:** Client-side tool; the app implements handlers. Claude auto-views /memories before tasks. Tool returns: directories listed 2-deep with human sizes (tab-separated, excluding dotfiles + node_modules); files returned with line numbers (6-char right-aligned, tab sep, 1-indexed, max 999,999 lines). Auto system-prompt injection: 'IMPORTANT: ALWAYS VIEW YOUR MEMORY DIRECTORY BEFORE DOING ANYTHING ELSE. MEMORY PROTOCOL...'. NOTE: this is the API/SDK memory tool, distinct from Claude Code's built-in auto-memory subsystem — Claude Code's auto-memory does not expose this tool by default; the CLI uses its own filesystem-based memory instead.

**Data model:** Tool type 'memory_20250818', name 'memory'. Commands: view{path,view_range?}, create{path,file_text}, str_replace{path,old_str,new_str}, insert{path,insert_line,insert_text}, delete{path}, rename{old_path,new_path}. Paths confined to /memories/.

**Config:** Subclass betaMemoryTool (TS) / BetaAbstractMemoryTool (Python/C#) / BetaMemoryToolHandler (Java). Tool name='memory'. Must restrict to /memories dir, validate canonical paths, reject ../ sequences and URL-encoded traversal.

### InstructionsLoaded hook
**Purpose:** Observability for memory/rules loading.

**Mechanism:** Fires at session start AND when files lazily load mid-session (e.g. subdir CLAUDE.md read, path-glob rule triggered, @import include resolved, /compact re-inject). Matcher field = load reason. Non-blocking (exit code ignored), cannot decision-control; useful for logging which files load and why.

**Data model:** Hook stdin JSON includes load_reason field. JSON output via exit 0 stdout. hookSpecificOutput.hookEventName='InstructionsLoaded'.

**Config:** Hooks key: InstructionsLoaded with matcher values session_start|nested_traversal|path_glob_match|include|compact. Exit code ignored (non-blocking). Output capped 10,000 chars.

### .claude/rules/ path-scoped rules
**Purpose:** Modular, conditional memory injection scoped to file globs.

**Mechanism:** Rules in .claude/rules/*.md are discovered recursively. Those with a `paths:` frontmatter field only inject when Claude reads a file matching the glob. User-level rules load before project rules (lower precedence). Trigger on file read, not every tool use. Symlinks supported, circular handled. Loaded on demand when matching files opened. Also loadable from --add-dir dirs when CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1.

**Data model:** YAML frontmatter `paths: ["src/api/**/*.ts"]`. Rules WITHOUT paths frontmatter load unconditionally at launch at .claude/CLAUDE.md priority.

**Config:** Rule files in .claude/rules/ (recursive) or ~/.claude/rules/. frontmatter: paths: [globs].

## Key behaviors
- CLAUDE.md is CONTEXT, NOT config — injected as a user message AFTER the system prompt, never guaranteed to be followed. To hard-enforce behavior use PreToolUse hooks or managed settings permissions.deny.
- Concatenation is root-to-cwd, cwd-level read LAST; per level CLAUDE.local.md appended after CLAUDE.md. Files never override each other across the tree.
- Block-level HTML comments <!-- --> are STRIPPED before context injection (saves tokens). Comments INSIDE code fences are preserved. Read tool shows comments unstripped.
- @import relative paths resolve relative to the file CONTAINING the import, NOT cwd. Both relative and absolute paths allowed. Home-dir imports (@~/.claude/x.md) for cross-worktree sharing.
- @import recursion MAX DEPTH = 4 hops (per current official docs code.claude.com/docs/en/memory). NOTE: several third-party write-ups and some mirror sites say 5; the canonical Anthropic doc states 4 — verify against live docs before hardcoding.
- Auto memory needs Claude Code v2.1.59+. MEMORY.md load cap: first 200 lines OR first 25KB, whichever first; content beyond NOT loaded at start. CLAUDE.md is loaded in FULL regardless of length (no 200-line hard cap, but adherence degrades).
- Project path <project> in ~/.claude/projects/<project>/memory/ is derived from the GIT REPO root, so all worktrees + subdirs in one repo share ONE auto-memory dir. Outside a git repo, project root is used.
- autoMemoryDirectory must be absolute or start with ~/. When set in .claude/settings.json or settings.local.json, honored only AFTER workspace trust dialog accepted (same gate as hooks).
- claudeMdExcludes matches ABSOLUTE file paths via glob, configurable at any settings layer, arrays MERGE across layers. Managed-policy CLAUDE.md is NEVER excludable.
- Subagents can maintain their own auto memory (per-subagent memory dirs).
- Topic files surfaced by a Sonnet side-query (NOT FileReadTool): up to 5 files/turn, returned as JSON {selected_memories:string[]} max 256 tokens, injected as relevant_memories attachments, already-surfaced filtered out.
- autoDream background consolidation: triggers after >=24h since last consolidation AND >=5 sessions, forked subagent, 4 phases (orient/gather/consolidate/prune), PID lock file .consolidate-lock with 60-min stale guard, rollback rewinds mtime on failure.
- Topic file 4 types: user, feedback, project, reference. YAML frontmatter name/description/type. description is what Sonnet selector reads for relevance — vague = never surfaced.
- What NOT to save: code patterns/architecture/paths (derivable), git history (git log authoritative), debugging fixes (in commit msg), anything already in CLAUDE.md, ephemeral task details.
- Managed settings parse tolerantly since v2.1.169: invalid entries stripped with warning, rest enforced. Security fields (allowedMcpServers, enforceAvailableModels, forceLoginOrgUUID, etc.) have per-field fail-closed behavior.
- Legacy Windows managed path C:\ProgramData\ClaudeCode\managed-settings.json removed in v2.1.75; must migrate to C:\Program Files\ClaudeCode\.
- Settings files are watched and hot-reloaded mid-session (permissions, hooks, apiKeyHelper) firing ConfigChange hook; but `model` and outputStyle are read-once at start (use /model or restart).
- # quick-add memory: typing '#' prefix in prompt triggers Claude Code to write the memory into the relevant CLAUDE.md file (had a regression bug on Windows, issue #14868, Dec 2025).

## External interfaces
- File paths: ./CLAUDE.md, ./.claude/CLAUDE.md, ./CLAUDE.local.md, ~/.claude/CLAUDE.md, ~/.claude/rules/*.md, .claude/rules/*.md, ~/.claude/projects/<project>/memory/MEMORY.md + topic .md files
- Managed CLAUDE.md paths: macOS /Library/Application Support/ClaudeCode/CLAUDE.md | Linux/WSL /etc/claude-code/CLAUDE.md | Windows C:\Program Files\ClaudeCode\CLAUDE.md
- managed-settings.json + managed-settings.d/*.json drop-in dir in same system dir (drop-in requires v2.1.x+)
- Settings keys: claudeMd (managed-only), claudeMdExcludes (glob array, mergeable), autoMemoryEnabled (bool), autoMemoryDirectory (abs or ~/), --setting-sources, --add-dir flag
- Env vars: CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1, CLAUDE_CODE_DISABLE_AUTO_MEMORY=1, CLAUDE_CODE_NEW_INIT=1
- API memory tool: tools=[{"type":"memory_20250818","name":"memory"}], path root /memories/, commands view/create/str_replace/insert/delete/rename
- CLI commands: /init, /memory
- Hook event: InstructionsLoaded (matcher values: session_start, nested_traversal, path_glob_match, include, compact)
- UI keybinding: '#' prefix in prompt = quick-add memory to CLAUDE.md

## Open questions
- EXACT import recursion depth: official docs say max 4 hops, but several mirrors/third-party deep-dives say 5 — needs live re-verification against code.claude.com/docs/en/memory and the actual MAX_IMPORT_DEPTH constant in source.
- Exact JSON schema of the InstructionsLoaded hook stdin payload (full field list, not just load_reason) — not fully captured; would need the hooks reference #hook-events section.
- Whether Claude Code's built-in auto-memory uses the SAME memory_20250818 tool under the hood or a separate proprietary filesystem layer (manavgup deep-dive implies a separate subsystem: memdir/autoDream/extractMemories services, NOT the API memory tool).
- Exact '<project>' directory-name hashing/encoding scheme used under ~/.claude/projects/<project>/memory/ (how repo path -> folder name).
- Whether the Sonnet-side-query memory surfacing (up to 5 files, 256-token JSON) is documented officially or only reverse-engineered — official docs only state 'first 200 lines/25KB loaded'.

## Sources
- [How Claude remembers your project — Claude Code Docs (code.claude.com/docs/en/memory)](https://code.claude.com/docs/en/memory) — Canonical source for the full memory subsystem: CLAUDE.md hierarchy table, @import 4-hop limit, walk-up resolution order, CLAUDE.local.md appending, auto memory (MEMORY.md 200-line/25KB cap, ~/.claude/projects/<project>/memory/, autoMemoryEnabled/Directory/CLAUDE_CODE_DISABLE_AUTO_MEMORY, v2.1.59+ requirement, compaction survival, claudeMd managed key, claudeMdExcludes, --add-dir env, InstructionsLoaded hook reference, .claude/rules/ path-scoping.
- [Claude Code settings — Claude Code Docs (code.claude.com/docs/en/settings)](https://code.claude.com/docs/en/settings) — Authoritative settings-scope precedence (Managed > CLI > Local > Project > User), managed-settings.json locations per OS, managed-settings.d/ drop-in systemd-style merge, managed CLAUDE.md path equivalence, v2.1.75 Windows legacy-path removal, v2.1.169 tolerant parsing, hot-reload + ConfigChange hook, model/outputStyle read-once.
- [Memory tool — Claude API Docs (platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool)](https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool) — Defines the API memory tool (type memory_20250818, name memory, commands view/create/str_replace/insert/delete/rename, /memories dir, path-traversal security, return formats, auto MEMORY PROTOCOL prompt). Distinct from Claude Code's built-in auto-memory.
- [Hooks reference — Claude Code Docs (code.claude.com/docs/en/hooks)](https://code.claude.com/docs/en/hooks) — Confirms InstructionsLoaded event exists, fires at session start + lazy load, matcher = load reason (session_start, nested_traversal, path_glob_match, include, compact), exit code ignored (non-blocking), plus full hook lifecycle including PreCompact/PostCompact relevant to memory re-injection.
- [09 — Memory System · Inside Claude Code (manavgup.github.io/shipai)](https://manavgup.github.io/shipai/deep-dives/claude-code/09-memory.html) — Reverse-engineered internals: src/memdir/autoDream/extractMemories services, MEMORY.md pointer-index format, 4 memory types (user/feedback/project/reference), Sonnet side-query surfacing (up to 5 files, 256-token JSON), autoDream 24h+5-session trigger with .consolidate-lock 60-min stale guard, 200-line/25KB truncation detail. Useful for a faithful reimplementation even though it's community-sourced.
- [[BUG] # memory shortcut no longer saves to CLAUDE.md — anthropics/claude-code#14868](https://github.com/anthropics/claude-code/issues/14868) — Confirms the '#' prefix quick-add-memory-to-CLAUDE.md behavior is a real, official feature (and documents a Dec 2025 Windows regression).
- [Boris Cherny Threads post — '#' quick-add memory announcement](https://www.threads.com/@boris_cherny/post/DHq60G7vkNz) — Anthropic staff announcement confirming '#' prefix writes memories to CLAUDE.md files.
