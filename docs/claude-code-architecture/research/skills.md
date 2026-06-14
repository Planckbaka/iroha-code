# Research: skills

**Confidence:** high  
**As-of:** 2026-06

## Summary

The Skills system lets Claude Code (and the Agent SDK) extend itself via directories each containing a SKILL.md with YAML frontmatter (metadata) + markdown body (instructions). It implements THREE levels of progressive disclosure: (1) at startup only each skill's name+description+when_to_use are loaded into the Skill tool's dynamically-generated description (not the system prompt), bounded by a char budget; (2) when the model (or user) invokes a skill the full SKILL.md body is read and injected as a hidden user message (isMeta:true) plus a visible loading-status message; (3) supporting files (scripts/, references/, assets/) are loaded on demand by Claude. Skills are NOT executable code — they are prompt templates that modify conversation + execution context (allowed-tools, model, effort). The model invokes them through a single meta-tool named "Skill" (capital S) whose input is just {command:"<skill-name>"}; Claude decides which skill to call via pure LLM reasoning over the description list, with no algorithmic routing. Custom commands (legacy .claude/commands/) have been merged into skills: both produce /name and behave identically. Skills follow the open Agent Skills standard (agentskills.io) extended by Claude Code with invocation-control frontmatter, subagent execution (context:fork), and dynamic shell-context injection.

## Components
### Skill definition file (SKILL.md)
**Purpose:** The single required entrypoint for each skill; carries metadata frontmatter + markdown body instructions.

**Mechanism:** Startup scan loads skills/commands from user (~/.claude/skills/), project (.claude/skills/), parent dirs up to repo root, nested .claude/skills/ on demand (monorepo), --add-dir directories' .claude/skills/, plugins, and bundled set. Each SKILL.md parsed: frontmatter (between --- markers) becomes metadata; remainder is promptContent. Directory name (or plugin:dir name for plugins, or filename for legacy commands) becomes the command name typed after /. The frontmatter 'name' is the DISPLAY label only, EXCEPT for a plugin root SKILL.md where name (or plugin dir name fallback) sets the command. Live change detection watches SKILL.md text only (hooks/MCP/agents need /reload-plugins).

**Data model:** YAML frontmatter block delimited by --- at file start. Fields use kebab-case (name, description, allowed-tools, disable-model-invocation, user-invocable, disallowed-tools, model, effort, context, agent, hooks, paths, shell, argument-hint, arguments, when_to_use). Note the snake_case when_to_use is the YAML-source key, mapped internally to whenToUse. JSON tool schema entry: { type:'skill', name, description, allowedTools:[...], disallowedTools:[...], model, isSkill:true, disableModelInvocation, userInvocable, context, agent, hooks, paths, promptContent }.

**Config:** Frontmatter keys (all optional unless noted): name (defaults to dir name), description (recommended; default = first markdown paragraph), when_to_use (appended to description with ' - ', counts toward 1,536 cap), disable-model-invocation (bool, default false), user-invocable (bool, default true), allowed-tools (space/comma string or YAML list; supports Bash(git add *) / Skill(name *) syntax), disallowed-tools (same format, clears on next user message), model, effort, context (set to 'fork'), agent (Explore/Plan/general-purpose/custom), hooks, paths (globs limiting auto-activation), argument-hint, arguments (space string or YAML list), shell (bash default | powershell, requires CLAUDE_CODE_USE_POWERSHELL_TOOL=1).

### Skill tool (model-invoked meta-tool)
**Purpose:** The single meta-tool exposed to the model that dispatches to any individual skill; implements progressive disclosure level 1.

**Mechanism:** Unlike static tools (Read/Bash), the Skill tool's 'description' field is a dynamic async generator. At each API request it aggregates ALL skills eligible for model invocation, formats each as `"name": description - when_to_use` (when_to_use appended with ' - ' separator), and wraps them in <skills_instructions> + <available_skills> XML inside the description. Claude picks a skill via tool_use with input {command:'skill-name'}. Validation: errorCode 1 empty, 2 unknown, 3+ can't-load/permission/already-running. The Skill tool is gated by permission rules Skill / Skill(name) / Skill(name *) and the skills filter; when set, 'Skill' is auto-added to allowedTools.

**Data model:** Tool schema: name='Skill', input_schema={command:string (skill name, no args)}, output_schema={success:boolean, commandName:string}. Prompt generated via async prompt() function.

**Config:** Filter predicate: type==='prompt' && isSkill===true && !disableModelInvocation && (source!=='builtin' || isModeCommand===true) && (description || when_to_use present). Format: `"<name>": <description> - <when_to_use>`.

### Progressive disclosure + listing budget
**Purpose:** Keep token cost near-zero until a skill is actually needed; bound the always-loaded metadata.

**Mechanism:** Level 1 = name+description preloaded into Skill tool description every turn (subject to char budget: scales at 1% of context window, least-invoked skills' descriptions dropped first when overflow, run /doctor to see). Level 2 = full SKILL.md body loaded only when Claude/user invokes the skill, injected as a single message persisting for the session. Level 3+ = supporting files (scripts/, references/, assets/) read on demand via Read/Bash by Claude. On auto-compaction: most recent invocation of each skill re-attached keeping first 5,000 tokens each, sharing a 25,000-token combined budget, filled most-recent-first so older skills can be dropped.

**Data model:** ContextWindow = systemPrompt + [skill listing inside Skill tool desc] + conversation. Budget = 1% of model context window (default) OR SLASH_COMMAND_TOOL_CHAR_BUDGET fixed chars.

**Config:** budget knobs: skillListingBudgetFraction (fraction of context, default 0.01), SLASH_COMMAND_TOOL_CHAR_BUDGET (fixed char env var), maxSkillDescriptionChars (per-entry cap, default 1536). skillOverrides states: on / name-only / user-invocable-only / off (written to settings.local.json via /skills menu; absent = on; does NOT affect plugin skills).

### Argument + shell-context injection
**Purpose:** Pass user/model args into the skill and inline live command output before Claude sees the body.

**Mechanism:** Before the body reaches Claude, substitutions run ONCE over the original file (command output is plain text, not re-scanned). Inline !`cmd` recognized only when ! starts a line or follows whitespace (KEY=!`cmd` is left literal). Multi-line via ```! fenced block. shell frontmatter selects bash (default) or powershell. Arguments: $ARGUMENTS (or appended as 'ARGUMENTS: <value>' if absent), $ARGUMENTS[N]/$N positional, $name from arguments list. \$ escapes a literal $. On invocation Claude receives base dir path so bundled resources are reachable.

**Data model:** Skill invocation = metadata message + isMeta:true prompt message + optional command_permissions message ({type:'command_permissions', allowedTools, model}).

**Config:** Strings honored: $ARGUMENTS, $ARGUMENTS[N] / $N (0-based, shell-style quoting), $name (declared via arguments: list), ${CLAUDE_SESSION_ID}, ${CLAUDE_EFFORT} (low/medium/high/xhigh/max; ultracode reports as xhigh), ${CLAUDE_SKILL_DIR} (skill's own dir, not plugin root). disableSkillShellExecution:true in settings replaces !`cmd` with '[shell command execution disabled by policy]' (bundled/managed unaffected).

### Discovery precedence + SDK integration
**Purpose:** Resolve which skill wins when names collide across scopes; expose skills programmatically in the Agent SDK.

**Mechanism:** Precedence enterprise > personal > project; plugin skills namespaced plugin-name:skill-name so they never conflict. SDK: settingSources/setting_sources controls loading (must include 'user'/'project'); skills option on query() is a filter ('all' | [names] | [] disable all).

**Data model:** Sources: enterprise/managed (all users) > personal (~/.claude) > project (.claude) — same-name overrides in that order. Plugins are namespaced plugin:skill and never collide. Skill takes precedence over same-named command.

**Config:** skills filter accepts: omitted (all discovered on + Skill tool auto-added), 'all', [name,...] (only those; plugin skills as plugin:skill), or [] (disable all). Unlisted skills' files remain reachable via Read/Bash (filter, not sandbox).

## Key behaviors
- DEFAULTS: user-invocable=true, disable-model-invocation=false; a skill with neither description nor when_to_use is FILTERED OUT of the Skill tool entirely (won't be model-invoked).
- allowed-tools GRANTS approval-without-prompt for listed tools while skill is active but does NOT restrict the callable set; disallowed-tools REMOVES tools from the pool but CLEARS on the next user message (transient). Both support space/comma strings or YAML lists and Bash(git add *) wildcard syntax.
- Commands were MERGED into skills: .claude/commands/deploy.md and .claude/skills/deploy/SKILL.md both produce /deploy identically; a skill wins over a same-named command. legacy commands keep working and support the same frontmatter.
- In the SDK, SKILL.md allowed-tools is IGNORED — control tool access via the query() allowedTools option; passing skills=[...] adds 'Skill' to allowedTools automatically, but if you pass an explicit tools list you must include 'Skill' yourself.
- Plugin skills use namespace plugin-name:skill-name and CANNOT conflict with other levels; they are NOT affected by skillOverrides (manage via /plugin). Plugin root SKILL.md is the only place frontmatter name sets the command name.
- disable-model-invocation:true removes the skill's description from Claude's context entirely (level-0 disclosure) AND blocks preloading into subagents; user-invocable:false only hides from the / menu, NOT from Skill-tool access.
- context: fork runs the skill body as the subagent TASK prompt (no conversation history); agent: defaults to general-purpose; Explore/Plan agents skip CLAUDE.md+git status so a forked skill using them sees only SKILL.md + agent system prompt.
- Live change detection covers SKILL.md text only; if the skill folder is also a plugin, hooks/MCP/agents/output-styles changes need /reload-plugins. Creating a NEW top-level skills dir that didn't exist at startup requires a restart.
- Skill descriptions must be SINGLE-LINE in the YAML (multi-line breaks discovery — known gotcha). Keep SKILL.md body <500 lines; recommend <5,000 words.
- Security: project skills' allowed-tools take effect only after workspace trust dialog; bundled skills can be globally disabled via disableBundledSkills; malicious skills can exfiltrate data so audit before use.
- A few built-in commands (/init, /review, /security-review) are reachable via the Skill tool, but /compact and /help are NOT.
- ultrathink keyword in skill body requests deeper reasoning when the skill runs.

## External interfaces
- Skill tool (model-invoked meta-tool): name='Skill', input_schema={command:string}, output_schema={success,commandName}
- CLI flag --add-dir and command /add-dir load .claude/skills from extra dirs (NOT permissions.additionalDirectories)
- Settings.json keys: disableBundledSkills, skillOverrides (object: skill->{on|name-only|user-invocable-only|off}), skillListingBudgetFraction, maxSkillDescriptionChars, disableSkillShellExecution
- Env vars: SLASH_COMMAND_TOOL_CHAR_BUDGET, CLAUDE_CODE_USE_POWERSHELL_TOOL=1, CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1
- Built-in vars injected into skill body: $ARGUMENTS, $ARGUMENTS[N]/$N, $name, ${CLAUDE_SESSION_ID}, ${CLAUDE_EFFORT}, ${CLAUDE_SKILL_DIR}
- Slash menus: /skill-name, /skills (Space=cycle state, Enter=save), /doctor (budget overflow), /reload-plugins, /plugin (plugin skills)
- Permission rule syntax: Skill, Skill(name), Skill(name *)
- Agent SDK (Python/TS): setting_sources, skills option, allowed_tools; auto-adds 'Skill' to allowed_tools when skills set
- Plugin manifest: .claude-plugin/plugin.json; plugin root SKILL.md single-skill fallback uses name field or install-dir fallback

## Open questions
- Exact precedence ordering when enterprise/managed vs plugin vs MCP-provided skills collide (docs say enterprise>personal>project and plugins can't conflict, but MCP-server-provided skill precedence relative to these is under-specified).
- Whether disallowed-tools clearing is strictly 'next user message' or 'end of turn' — docs say 'next message you send' which needs confirming against harness behavior.
- Precise behavior of effort override (low/medium/high/xhigh/max) interaction with model-specific level availability and the ultracode=>xhigh mapping.

## Sources
- [Extend Claude with skills - Claude Code Docs](https://code.claude.com/docs/en/skills) — Primary authoritative spec: full frontmatter field reference, precedence, budget knobs (skillListingBudgetFraction/SLASH_COMMAND_TOOL_CHAR_BUDGET/maxSkillDescriptionChars/1536 cap), skillOverrides states, live change detection, bundled skills, lifecycle/compaction (5k/25k budgets), substitution vars.
- [Agent Skills in the SDK - Claude Code Docs](https://code.claude.com/docs/en/agent-sdk/skills) — Authoritative SDK behavior: skills option ('all'|list|[]), auto-add of Skill to allowedTools, setting_sources gating, allowed-tools IGNORED in SDK, filesystem-only registration (no programmatic API).
- [Plugins reference - Claude Code Docs](https://code.claude.com/docs/en/plugins-reference) — Plugin skill location/format, plugin-root SKILL.md fallback using name field vs install-dir fallback, plugin agent frontmatter fields, hook event list (SubagentStart etc.)
- [Equipping agents for the real world with Agent Skills - Anthropic Engineering](https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills) — Design rationale: three-level progressive disclosure (metadata -> SKILL.md -> bundled files), name+description preloaded into system prompt at startup, SKILL.md body loaded via Bash/Read on demand, Agent Skills open standard (Dec 18 2025).
- [Claude Agent Skills: A First Principles Deep Dive - Han, Not Solo](https://leehanchung.github.io/blogs/2025/10/26/claude-skills-deep-dive/) — Reverse-engineered internals: Skill tool input_schema {command}/output_schema {success,commandName}, dynamic async prompt() generator, isMeta dual-message injection (visible <command-message>/<command-name>/<command-args> + hidden full prompt), when_to_use->whenToUse mapping, filter predicate requiring description|when_to_use, plugin name format plugin:skill and (plugin:name) suffix.
- [Create custom subagents - Claude Code Docs](https://code.claude.com/docs/en/sub-agents) — Subagent skills: preload field, cannot preload skills with disable-model-invocation:true, Explore/Plan skip CLAUDE.md.
