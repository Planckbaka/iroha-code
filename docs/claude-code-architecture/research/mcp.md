# Research: mcp

**Confidence:** high  
**As-of:** 2026-06

## Summary

Claude Code's MCP integration (src/services/mcp/) connects to external MCP servers over four transports (stdio, SSE [deprecated], HTTP/streamable-HTTP, WebSocket), discovers their tools/resources/prompts, and exposes them to the model with prefixed names. Servers are configured at three scopes (local, project via .mcp.json, user via ~/.claude.json) plus plugins and claude.ai connectors, with a strict precedence (Local > Project > User > Plugins > claude.ai) that connects to a server once using the single highest-precedence entry (no field merging). MCP tools are named mcp__<server>__<tool> (plugin-bundled tools use mcp__plugin_<plugin>_<server>__<tool>), and by default are NOT loaded upfront — Tool Search defers tool definitions until Claude invokes a ToolSearch call, so context usage stays low. HTTP/SSE servers support OAuth 2.0 (with dynamic client registration, CIMD, or pre-configured credentials), automatic token refresh via keychain, and dynamic headersHelper scripts; stdio servers run as child processes with CLAUDE_PROJECT_DIR injected. Enterprise control is layered on via managed-mcp.json (exclusive fixed set), allowedMcpServers/deniedMcpServers allow/denylists, and managed settings. The /mcp slash command and `claude mcp list/get/add/remove` CLI manage the lifecycle, connection status, and OAuth flows.

## Components
### Transports
**Purpose:** The 4 wire transports Claude Code uses to talk to MCP servers.

**Mechanism:** stdio: spawn child process, JSON-RPC over stdin/stdout, CLAUDE_PROJECT_DIR injected into child env, lifecycle = full session, NOT auto-reconnected. http: streamable-HTTP per MCP 2025-03-26 spec; POST for JSON-RPC, optional GET for SSE stream; supports OAuth; auto-reconnect with exponential backoff (up to 5 attempts, start 1s doubling). sse: deprecated legacy HTTP+SSE; same reconnection. ws: persistent bidirectional WebSocket (wss), header-only auth, no OAuth, configurable only via .mcp.json/add-json (NOT via --transport flag). Initial connection (v2.1.121+) retries up to 3 times on transient errors (5xx/refused/timeout); auth/404 errors not retried.

**Data model:** { "type":"http", "url":"https://...", "headers":{...}, "timeout":600000, "alwaysLoad":true, "headersHelper":"...", "oauth":{...} }

**Config:** type: 'http' | 'streamable-http' (alias) | 'sse' | 'stdio' | 'ws'. Only http/sse/ws take 'url'. Only stdio takes 'command'+'args'+'env'. 'timeout' (ms, per-server hard tool-call wall-clock) and 'alwaysLoad' (bool) apply to all types.

### Configuration scopes
**Purpose:** Where server definitions live and how precedence resolves duplicates.

**Mechanism:** Local: stored in ~/.claude.json under the current project's path key; private to user+project; DEFAULT scope (was named 'project' in old versions). Project: written to <project-root>/.mcp.json; shared via VCS; requires per-user approval (prompt on load; reset via `claude mcp reset-project-choices`). User: stored in ~/.claude.json; cross-project; private to user (was named 'global' in old versions). On name collision across scopes, Claude Code connects ONCE using the single highest-precedence entry — entire entry wins, fields are NOT merged. Plugins and claude.ai connectors dedupe by endpoint (URL/command), the three scopes dedupe by name.

**Data model:** ~/.claude.json: { "projects": { "/abs/project/path": { "mcpServers": { "<name>": {<serverdef>} } } } } (local & user scopes). project .mcp.json: { "mcpServers": { "<name>": {<serverdef>} } }.

**Config:** --scope flag on `claude mcp add` (local default / project / user). Precedence highest-first: Local > Project > User > Plugins > claude.ai connectors.

### OAuth / Auth
**Purpose:** Authenticating remote (HTTP/SSE) servers.

**Mechanism:** Triggered when server returns 401/403 (or WWW-Authenticate header). Flow: Claude opens browser -> user authorizes -> callback to http://localhost:PORT/callback (random port unless --callback-port pins it) -> token stored securely in OS keychain (macOS) or credentials file, auto-refreshed. oauth.scopes pins requested scopes (space-separated, overrides discovery); offline_access auto-appended if advertised. A configured headers.Authorization that the server rejects is a hard failure (no OAuth fallback). headersHelper runs arbitrary shell command at connect time, stdout = JSON object of string headers, 10s timeout, env vars CLAUDE_CODE_MCP_SERVER_NAME + CLAUDE_CODE_MCP_SERVER_URL injected; overrides static headers; requires workspace-trust dialog at project/local scope.

**Data model:** OAuth discovery: GET /.well-known/oauth-protected-resource (RFC 9728) -> fallback /.well-known/oauth-authorization-server (RFC 8414). Supports Dynamic Client Registration, CIMD (Client ID Metadata Document), and pre-configured credentials.

**Config:** Serverdef optional oauth: { clientId, callbackPort, clientSecret(stored in keychain only), authServerMetadataUrl (v2.1.64+, must be https), scopes (space-separated string, RFC 6749 format) }. CLI: --client-id, --client-secret (masked prompt; or MCP_CLIENT_SECRET env), --callback-port.

### Tool exposure & Tool Search
**Purpose:** How MCP tools become callable by the model.

**Mechanism:** MCP tools are NOT all loaded into the system prompt upfront. By default Tool Search is ON: only tool NAMES + server instructions load at session start; Claude calls a `ToolSearch` tool to pull a specific tool's schema on demand (uses beta `tool_reference` blocks). Fallback (no tool search, e.g. Vertex, custom ANTHROPIC_BASE_URL, ENABLE_TOOL_SEARCH=false): a `WaitForMcpServers` tool makes Claude wait for connecting servers. Haiku models do NOT support tool_reference. ENABLE_TOOL_SEARCH=auto loads tools upfront if they fit within 10% of context window, defers overflow. `alwaysLoad:true` on a server forces all its tools upfront regardless of setting and blocks startup until connect (capped at 5s connect timeout). Server instructions and tool descriptions truncated at 2KB each.

**Data model:** Tool exposed to model: name `mcp__<server>__<tool>`. tool_reference block (beta) carries deferred defs. alwaysLoad: true on server OR _meta['anthropic/alwaysLoad']=true on a tool forces upfront load.

**Config:** ENABLE_TOOL_SEARCH env: unset=default(defer), true=force defer+send beta header, auto / auto:N = threshold (<=10% context upfront), false=load all upfront.

### Output limits
**Purpose:** Bounding MCP tool output token usage.

**Mechanism:** When an MCP tool returns >10000 tokens, Claude Code warns. Default hard cap 25000 tokens (MAX_MCP_OUTPUT_TOKENS). Oversized text results persisted to disk and replaced with a file reference in the conversation. A tool can opt into a larger threshold via _meta['anthropic/maxResultSizeChars'] in its tools/list entry (hard ceiling 500000 chars) — applies to text content only.

**Data model:** Result text content subject to MAX_MCP_OUTPUT_TOKENS unless _meta['anthropic/maxResultSizeChars'] set (max 500000 chars). Image content ALWAYS subject to token limit regardless of annotation.

**Config:** MAX_MCP_OUTPUT_TOKENS env (default 25000). Warning fires >10000 tokens. MCP_TIMEOUT env = startup timeout. MCP_TOOL_TIMEOUT env = global per-call default (~28h).

### /mcp command & CLI surface
**Purpose:** User-facing management UI and commands.

**Mechanism:** `/mcp` (in-session): lists servers with connection status (connected/pending/failed), tool count, flags servers advertising tools capability but exposing none, OAuth 'Clear authentication', approve pending project servers, retry failed. `claude mcp list` shows ⏸ Pending approval for unapproved project servers; `claude mcp get <name>` shows pending/rejected status. `claude mcp serve` turns Claude Code itself into a stdio MCP server exposing View/Edit/LS etc. Reserved server name `workspace` is skipped at load with a warning.

**Data model:** /mcp shows: per-server tool count, pending/failed/rejected status, 'Show unused connectors' row (v2.1.161+).

**Config:** Commands: claude mcp add, add-json, add-from-claude-desktop, list, get, remove, reset-project-choices, serve.

### Enterprise policy (managed MCP)
**Purpose:** Centralized control over which MCP servers users may connect to.

**Mechanism:** managed-mcp.json (system path: macOS /Library/Application Support/ClaudeCode/, Linux /etc/claude-code/, Windows C:\Program Files\ClaudeCode\; same format as .mcp.json; deploy via MDM/GPO, NOT server-managed settings): if present, ONLY those servers load (exclusive mode), user adds blocked with 'enterprise MCP configuration is active'. Evaluation order: merge allow/deny from all sources -> denylist match blocks unconditionally -> allowlist: remote needs serverUrl (or serverName only if no serverUrl entries exist), stdio needs serverCommand (or serverName only if no serverCommand entries). Commands match EXACTLY (all args in order). URLs support * wildcards anywhere incl scheme; hostname case-insensitive ignoring trailing dot; path case-sensitive.

**Data model:** Entry = { "serverUrl": "https://*" } | { "serverCommand": ["npx","-y","pkg"] } | { "serverName": "label" }. managed-mcp.json empty mcpServers => MCP disabled.

**Config:** Settings keys: allowedMcpServers, deniedMcpServers, allowManagedMcpServersOnly (managed-source-only), allowAllClaudeAiMcps (v2.1.149+, managed-source-only).

### claude.ai connectors
**Purpose:** MCP servers configured in the claude.ai web app.

**Mechanism:** Connectors added at claude.ai/customize/connectors auto-appear in CC when active auth method is Claude.ai subscription (NOT loaded if ANTHROPIC_API_KEY/AUTH_TOKEN/apiKeyHelper/Bedrock/Vertex active). Fetched at runtime, shown with claude.ai indicator. Unused connectors collapsed behind 'Show unused connectors' (v2.1.161+).

**Data model:** claude.ai connector precedence: lowest. A CC-configured server pointing at same URL hides the connector.

**Config:** ENABLE_CLAUDEAI_MCP_SERVERS=false disables. Anthropic-hosted connectors (Microsoft 365, Gmail, Google Calendar) require claude.ai-side connect (v2.1.162+).

## Key behaviors
- Scope name history: current 'local' was 'project'; current 'user' was 'global'. 'project' scope now means the shared .mcp.json file. Do not confuse MCP local scope (lives in ~/.claude.json) with general local settings (live in .claude/settings.local.json).
- Precedence on duplicate is winner-take-all per entire server entry (Local > Project > User > Plugins > claude.ai); fields are NOT merged. The 3 scopes dedupe by name; plugins and connectors dedupe by endpoint (URL/command).
- Project-scoped servers from .mcp.json REQUIRE interactive approval before use; status shows ⏸ Pending approval until approved / ✗ Rejected. Reset via `claude mcp reset-project-choices`.
- Server name `workspace` is reserved/skipped at load with a rename warning.
- streamable-http is an alias for http in the `type` field (so configs copied from MCP docs work unchanged). SSE is deprecated; http preferred.
- WebSocket (`type: ws`) cannot be added via `claude mcp add --transport` — only via .mcp.json or add-json. WS has no OAuth (header-only). HTTP is the only transport supporting OAuth + the --transport flag.
- Stdio servers are NOT auto-reconnected (local processes); http/sse auto-reconnect up to 5 attempts, 1s->doubling backoff. Initial connect retries up to 3x on transient errors since v2.1.121.
- Per-server `timeout` (ms) is a hard per-call wall-clock; progress notifications do NOT extend it. Values <1000 are IGNORED (fall through to MCP_TOOL_TIMEOUT default ~28h) since v2.1.162; before v2.1.162 they were floored to 1 second. HTTP/SSE first-byte budget min 60s.
- MAX_MCP_OUTPUT_TOKENS default 25000; warning at >10000 tokens. Oversized text persisted to disk + replaced by file ref unless tool sets _meta['anthropic/maxResultSizeChars'] (ceiling 500000). Image content always subject to token cap regardless.
- Tool Search ON by default: tools deferred, discovered via `ToolSearch` tool using beta `tool_reference` blocks. Disabled by default on Vertex AI and when ANTHROPIC_BASE_URL is non-first-party. Haiku lacks tool_reference support. ENABLE_TOOL_SEARCH=auto = upfront if <=10% context. alwaysLoad:true forces upfront + blocks startup (5s cap).
- Env var expansion `${VAR}` and `${VAR:-default}` works in command/args/env/url/headers of .mcp.json. Missing var with no default = config parse failure. CLAUDE_PROJECT_DIR must use a default like ${CLAUDE_PROJECT_DIR:-.} in project/user .mcp.json (plugin configs substitute it directly).
- MCP resources: `@server:protocol://path` @-mention; Claude Code auto-provides tools to list/read resources when server supports them; fuzzy-searched in @ autocomplete. MCP prompts: surface as `/mcp__<server>__<prompt> [args]` slash commands; names normalized (spaces->_).
- Dynamic updates: servers sending MCP `list_changed` notification cause auto-refresh of tools/prompts/resources without reconnect.
- Elicitation: servers can request structured input mid-task (form or URL mode) via MCP elicitation; auto-displayed; auto-respond via Elicitation hook.
- OAuth precedence: oauth.scopes > authServerMetadataUrl > discovered /.well-known scopes. offline_access auto-appended if advertised. 403 insufficient_scope triggers re-auth with same pinned scopes. headersHelper runs fresh each connect (no caching), overrides static headers, needs workspace trust at project/local scope.
- claude.ai connectors only load when active auth = Claude.ai subscription; disabled by ANTHROPIC_API_KEY/AUTH_TOKEN/apiKeyHelper/Bedrock/Vertex. ENABLE_CLAUDEAI_MCP_SERVERS=false disables. Some Anthropic-hosted connectors (MS 365, Gmail, Google Calendar) require claude.ai-side connect (v2.1.162+).
- Enterprise allowlist semantics: allowlist with only serverName entries is NOT a security control (user can name any server 'github'). serverUrl/serverCommand entries make name entries stop matching. Denylist always wins, always merges from all sources.
- managed-mcp.json empty mcpServers = MCP fully disabled; suppresses claude.ai connectors unless allowAllClaudeAiMcps:true (managed-source-only, v2.1.149+).

## External interfaces
- CLI: claude mcp add [--transport http|sse|stdio] [--scope local|project|user] [--header "K: V"] [--env K=V] [--client-id] [--client-secret] [--callback-port N] [--channels] <name> <url|-- <command> [args...]>
- CLI: claude mcp add-json <name> '<json>' [--scope user] [--client-secret]
- CLI: claude mcp add-from-claude-desktop
- CLI: claude mcp list | get <name> | remove <name> | reset-project-choices | serve
- In-session slash command: /mcp (status panel, OAuth, retry, clear auth)
- MCP prompt as slash command: /mcp__<server>__<prompt> [args]
- Resource @-mention: @<server>:<protocol>://<resource/path>
- Config files: .mcp.json (project root), ~/.claude.json (local+user), managed-mcp.json (system path)
- Env vars: MCP_TIMEOUT, MCP_TOOL_TIMEOUT, MAX_MCP_OUTPUT_TOKENS, ENABLE_TOOL_SEARCH, ENABLE_CLAUDEAI_MCP_SERVERS, MCP_CLIENT_SECRET, CLAUDE_PROJECT_DIR (injected into stdio child), CLAUDE_CODE_MCP_SERVER_NAME/URL (injected into headersHelper)
- Agent SDK: options.mcpServers{...}, options.allowedTools=["mcp__<server>__*"]
- Tool name surface: mcp__<server>__<tool> ; plugin: mcp__plugin_<plugin>_<server>__<tool>

## Open questions
- Exact internal JSON-RPC initialize negotiation params and protocol version string Claude Code sends (likely '2025-03-26' or '2025-06-18'); not in public docs.
- Precise file/key format of the OAuth token store on disk and per-OS keychain service name.
- Whether `headersHelper` JSON merge is shallow-only and exact precedence vs `headers` beyond 'same name overrides'.
- Exact behavior of `WaitForMcpServers` internal tool name and its output schema when tool search is disabled.

## Sources
- [Connect Claude Code to tools via MCP — official docs](https://code.claude.com/docs/en/mcp) — Primary source: transports, scopes, tool naming, OAuth, output limits, tool search, resources, prompts, elicitation, channels — the entire MCP subsystem reference.
- [Control MCP server access for your organization (managed-mcp) — official docs](https://code.claude.com/docs/en/managed-mcp) — Authoritative on managed-mcp.json paths/format, allowedMcpServers/deniedMcpServers matching rules, allowManagedMcpServersOnly, evaluation order, allowAllClaudeAiMcps.
- [MCP server-types deep dive — anthropics/claude-code repo](https://github.com/anthropics/claude-code/blob/main/plugins/plugin-dev/skills/mcp-integration/references/server-types.md) — First-party repo reference documenting stdio/sse/http/ws config shapes, lifecycles, ${CLAUDE_PLUGIN_ROOT} expansion, and comparison matrix.
- [Connect to external tools with MCP (Agent SDK) — official docs](https://code.claude.com/docs/en/agent-sdk/mcp) — Confirms exact tool naming convention mcp__<server>__<tool>, mcpServers option, allowedTools wildcard, .mcp.json loading via settingSources.
- [MCP Transports specification — modelcontextprotocol.io](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports) — Underlying protocol spec for stdio, HTTP+SSE, and streamable-HTTP semantics that Claude Code implements.
- [Streamable HTTP specification (2025-03-26 / draft) — modelcontextprotocol.io](https://modelcontextprotocol.io/specification/draft/basic/transports/streamable-http) — Confirms streamable-http replaced HTTP+SSE in protocol version 2025-03-26, which Claude Code aliases to http.
