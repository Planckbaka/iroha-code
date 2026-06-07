package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"iroha/pkg/agent"

	"github.com/charmbracelet/glamour"
	glamansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

// rendererCache caches glamour.TermRenderer instances by width so that
// RenderMarkdownWithWidth does not allocate a new renderer on every streaming
// tick. The cache is bounded in practice because terminal widths are stable.
var (
	rendererCache   = make(map[int]*glamour.TermRenderer)
	rendererCacheMu sync.Mutex
)

// ClearRendererCache discards all cached renderers. Call this when the
// terminal width changes or when a fresh style is desired.
func ClearRendererCache() {
	rendererCacheMu.Lock()
	rendererCache = make(map[int]*glamour.TermRenderer)
	rendererCacheMu.Unlock()
}

const defaultMarkdownWidth = 80

var compactMarkdownStyle = newCompactMarkdownStyle()

func newCompactMarkdownStyle() glamansi.StyleConfig {
	style := styles.DarkStyleConfig
	textColor := style.Document.StylePrimitive.Color
	style.Document.StylePrimitive.BlockPrefix = ""
	style.Document.StylePrimitive.BlockSuffix = ""
	style.Document.StylePrimitive.Color = nil
	style.Document.Margin = nil
	if style.Text.Color == nil {
		style.Text.Color = textColor
	}
	return style
}

// RenderMarkdown renders raw markdown into compact ANSI terminal text.
func RenderMarkdown(raw string) string {
	return RenderMarkdownWithWidth(raw, defaultMarkdownWidth)
}

// RenderMarkdownWithWidth renders markdown for a bounded TUI viewport. Glamour's
// default document style pads every line to the renderer width, which makes short
// chat replies look like large colored blank blocks in a differential renderer.
func RenderMarkdownWithWidth(raw string, width int) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimRight(raw, "\r\n")
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	width = sanitizedWidth(width)

	rendererCacheMu.Lock()
	r, ok := rendererCache[width]
	if !ok {
		var err error
		r, err = glamour.NewTermRenderer(
			glamour.WithStyles(compactMarkdownStyle),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			rendererCacheMu.Unlock()
			return raw
		}
		rendererCache[width] = r
	}
	rendererCacheMu.Unlock()
	rendered, err := r.Render(raw)
	if err != nil {
		return raw
	}

	// Post-process to highlight diff lines in terminal
	lines := compactMarkdownLines(rendered)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "+ ") || trimmed == "+" {
			lines[i] = lipgloss.NewStyle().Foreground(ColorSuccess).Render(line)
		} else if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			lines[i] = lipgloss.NewStyle().Foreground(ColorDanger).Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

func compactMarkdownLines(rendered string) []string {
	lines := strings.Split(strings.ReplaceAll(rendered, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = trimANSIRightSpace(line)
		if len(out) == 0 && strings.TrimSpace(xansi.Strip(line)) == "" {
			continue
		}
		out = append(out, line)
	}
	for len(out) > 0 && strings.TrimSpace(xansi.Strip(out[len(out)-1])) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func trimANSIRightSpace(line string) string {
	visible := strings.TrimRightFunc(xansi.Strip(line), unicode.IsSpace)
	if visible == "" {
		return ""
	}
	return xansi.Cut(line, 0, xansi.StringWidth(visible))
}

// RenderConfirmCard renders the Human-in-the-Loop inline confirmation prompt
func RenderConfirmCard(prompt string, selectedIndex int) string {
	return RenderConfirmCardWithDiff(prompt, selectedIndex, false, false)
}

// RenderConfirmCardWithDiff renders confirmation prompts and appends optional interactive Diff triggers.
func RenderConfirmCardWithDiff(prompt string, selectedIndex int, hasDiff bool, diffActive bool) string {
	var sb strings.Builder

	// Header
	sb.WriteString(lipgloss.NewStyle().
		Foreground(ColorWarning).Bold(true).
		Render("Authorization Required") + "\n\n")

	// Prompt content
	sb.WriteString(prompt)
	sb.WriteString("\n\n")

	// Key hints
	yStyle := lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(ColorSuccess)
	nStyle := lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(ColorDanger)
	aStyle := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(ColorWarning)
	eStyle := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(ColorPrimary)
	qStyle := lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(ColorSecondary)

	if selectedIndex == 0 {
		yStyle = yStyle.Background(ColorSuccess).Foreground(lipgloss.Color("#18181B"))
	} else if selectedIndex == 1 {
		nStyle = nStyle.Background(ColorDanger).Foreground(lipgloss.Color("#18181B"))
	} else if selectedIndex == 2 {
		aStyle = aStyle.Background(ColorWarning).Foreground(lipgloss.Color("#18181B"))
	} else if selectedIndex == 3 {
		eStyle = eStyle.Background(ColorPrimary).Foreground(lipgloss.Color("#18181B"))
	} else if selectedIndex == 4 {
		qStyle = qStyle.Background(ColorSecondary).Foreground(lipgloss.Color("#18181B"))
	}

	sb.WriteString("  ")
	sb.WriteString(yStyle.Render("Y Allow"))
	sb.WriteString("  ")
	sb.WriteString(nStyle.Render("N Deny"))
	sb.WriteString("  ")
	sb.WriteString(aStyle.Render("A Always Allow"))
	sb.WriteString("  ")
	sb.WriteString(eStyle.Render("E Edit"))
	sb.WriteString("  ")
	sb.WriteString(qStyle.Render("? Explain"))

	sb.WriteString("\n\n")

	hints := "← → / Tab Select   Enter Confirm   Shortcuts: Y / N / A / E / ?"
	if hasDiff {
		if diffActive {
			hints += "   [D] Hide Diff"
		} else {
			hints += "   [D] Show Diff"
		}
	}
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true).Render(hints))

	borderColor := ColorWarning
	if strings.Contains(prompt, "[file_write]") || strings.Contains(prompt, "[file_read]") {
		borderColor = ColorSecondary
	} else if strings.Contains(prompt, "[mcp]") {
		borderColor = ColorPrimary
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		MarginTop(1).
		MarginBottom(1)

	return boxStyle.Render(sb.String())
}

// RenderWelcomeCard renders a minimal welcome screen
func RenderWelcomeCard(runner *agent.CustomRunner) string {
	var sb strings.Builder

	modelName := "Unknown"
	if runner != nil {
		modelName = runner.ModelName()
	}

	modeStr := string(agent.GlobalPermissionManager.GetMode())

	sb.WriteString(StylePrompt.Render("Iroha Code") + StyleKeyHelp.Render("  terminal coding agent") + "\n\n")
	sb.WriteString("  " + StyleKeyHelp.Render("model") + "  " + StylePrompt.Render(modelName) + "\n")
	sb.WriteString("  " + StyleKeyHelp.Render("mode ") + "  " + StylePrompt.Render(modeStr) + "\n\n")
	sb.WriteString("  " + StyleKeyHelp.Render("Type a task, or use /help, /sessions, /permission") + "\n")

	return StyleWelcome.Render(sb.String())
}

var permModeNames = []struct {
	Mode  agent.PermissionMode
	Label string
	Desc  string
	Icon  string
}{
	{agent.ModePlan, "Plan Mode", "Read-only mode - blocks all write operations and Shell commands", ""},
	{agent.ModeDefault, "Default Mode", "Every sensitive operation requires manual user approval (recommended)", ""},
	{agent.ModeAcceptEdits, "AcceptEdits Mode", "File edits auto-approved, shell commands require authorization", ""},
	{agent.ModeAuto, "Auto Mode", "Read and low-risk operations auto-approved, write operations still require approval", ""},
	{agent.ModeBypass, "Bypass Mode", "YOLO mode - skips all confirmation prompts (dangerous)", ""},
}

// RenderPermissionSelect renders an inline permission selection card (used after /permission command)
func RenderPermissionSelect(currentMode agent.PermissionMode) string {
	var sb strings.Builder
	sb.WriteString(StyleKeyActive.Render("Permission Mode Select") + "\n\n")

	for i, entry := range permModeNames {
		marker := "  "
		if entry.Mode == currentMode {
			marker = lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render("▶ ")
		} else {
			marker = "  "
		}
		fmt.Fprintf(&sb, "%s%s. %s  —  %s\n",
			marker,
			fmt.Sprintf("%d", i+1),
			lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render(entry.Label),
			lipgloss.NewStyle().Foreground(ColorTextMuted).Render(entry.Desc),
		)

	}

	sb.WriteString("\n" + StyleKeyHelp.Render("  Up/Down select   Enter confirm"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		MarginTop(1).
		Render(sb.String())
}

// RenderTodoDashboard renders the task checklist - hidden by default when empty
func RenderTodoDashboard() string {
	todoRender := agent.GlobalTodoManager.Render()
	if todoRender == "" {
		return ""
	}

	return cardStyleSlim.Render("Tasks\n\n"+todoRender) + "\n"
}

// RenderTaskDashboard renders a compact task graph summary
func RenderTaskDashboard() string {
	tasks, err := agent.GlobalTaskManager.ListTasks()
	if err != nil || len(tasks) == 0 {
		return ""
	}

	var completed, inProgress, ready, blocked []string

	badgeCompleted := lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render("✓")
	badgeInProgress := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render("›")
	badgeReady := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("·")
	badgeBlocked := lipgloss.NewStyle().Foreground(ColorTextMuted).Bold(true).Render("-")

	for _, t := range tasks {
		ownerBadge := lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true).Render(fmt.Sprintf("@%s", t.Owner))

		var line string
		if t.Status == "completed" {
			line = fmt.Sprintf("  %s %s %s", badgeCompleted, StylePrompt.Render(t.ID), ownerBadge)
			completed = append(completed, line)
		} else if t.Status == "in_progress" {
			line = fmt.Sprintf("  %s %s %s", badgeInProgress, StylePrompt.Render(t.ID), ownerBadge)
			inProgress = append(inProgress, line)
		} else if len(t.BlockedBy) == 0 {
			line = fmt.Sprintf("  %s %s %s", badgeReady, StylePrompt.Render(t.ID), ownerBadge)
			ready = append(ready, line)
		} else {
			depStyle := lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true).Render(fmt.Sprintf("(need: %s)", strings.Join(t.BlockedBy, ", ")))
			line = fmt.Sprintf("  %s %s %s %s", badgeBlocked, StylePrompt.Render(t.ID), ownerBadge, depStyle)
			blocked = append(blocked, line)
		}
	}

	var sb strings.Builder
	sb.WriteString(StyleKeyActive.Render("Tasks") + "\n")

	var items []string
	if len(inProgress) > 0 {
		items = append(items, strings.Join(inProgress, "\n"))
	}
	if len(ready) > 0 {
		items = append(items, strings.Join(ready, "\n"))
	}
	if len(blocked) > 0 {
		items = append(items, strings.Join(blocked, "\n"))
	}
	if len(completed) > 0 {
		items = append(items, strings.Join(completed, "\n"))
	}

	sb.WriteString(strings.Join(items, "\n") + "\n")

	var total = len(tasks)
	var done = len(completed)
	progressPct := 0
	if total > 0 {
		progressPct = (done * 100) / total
	}
	fmt.Fprintf(&sb, "\n  %d%% complete  (%d/%d)", progressPct, done, total)

	return cardStyleSlim.Render(sb.String()) + "\n"
}

// RenderTaskDetails renders the full detailed task graph panel for /task command
func RenderTaskDetails() string {
	tasks, err := agent.GlobalTaskManager.ListTasks()
	if err != nil || len(tasks) == 0 {
		return StyleKeyHelp.Render("  no tasks found")
	}

	var completed, inProgress, ready, blocked []string

	badgeCompleted := lipgloss.NewStyle().Background(ColorSuccess).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1).Bold(true).Render("done")
	badgeInProgress := lipgloss.NewStyle().Background(ColorWarning).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1).Bold(true).Render("active")
	badgeReady := lipgloss.NewStyle().Background(ColorPrimary).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1).Bold(true).Render("ready")
	badgeBlocked := lipgloss.NewStyle().Background(ColorTextMuted).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1).Bold(true).Render("blocked")

	for _, t := range tasks {
		ownerBadge := lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true).Render(fmt.Sprintf("@%s", t.Owner))

		var line string
		if t.Status == "completed" {
			line = fmt.Sprintf("  %-10s %s %s", StylePrompt.Render(t.ID), t.Subject, ownerBadge)
			completed = append(completed, line)
		} else if t.Status == "in_progress" {
			line = fmt.Sprintf("  %-10s %s %s", StylePrompt.Render(t.ID), t.Subject, ownerBadge)
			inProgress = append(inProgress, line)
		} else if len(t.BlockedBy) == 0 {
			line = fmt.Sprintf("  %-10s %s %s", StylePrompt.Render(t.ID), t.Subject, ownerBadge)
			ready = append(ready, line)
		} else {
			depStyle := lipgloss.NewStyle().Foreground(ColorDanger).Italic(true).Render(fmt.Sprintf("need: %s", strings.Join(t.BlockedBy, ", ")))
			line = fmt.Sprintf("  %-10s %s %s  %s", StylePrompt.Render(t.ID), t.Subject, ownerBadge, depStyle)
			blocked = append(blocked, line)
		}
	}

	var sb strings.Builder
	sb.WriteString(StyleKeyActive.Render("Durable Work Graph") + "\n\n")

	if len(inProgress) > 0 {
		fmt.Fprintf(&sb, "  %s\n", badgeInProgress)
		sb.WriteString(strings.Join(inProgress, "\n") + "\n\n")
	}
	if len(ready) > 0 {
		fmt.Fprintf(&sb, "  %s\n", badgeReady)
		sb.WriteString(strings.Join(ready, "\n") + "\n\n")
	}
	if len(blocked) > 0 {
		fmt.Fprintf(&sb, "  %s\n", badgeBlocked)
		sb.WriteString(strings.Join(blocked, "\n") + "\n\n")
	}
	if len(completed) > 0 {
		fmt.Fprintf(&sb, "  %s\n", badgeCompleted)
		sb.WriteString(strings.Join(completed, "\n") + "\n\n")
	}

	var total = len(tasks)
	var done = len(completed)
	progressPct := 0
	if total > 0 {
		progressPct = (done * 100) / total
	}
	fmt.Fprintf(&sb, "  %d%% complete  (%d/%d)", progressPct, done, total)

	return cardStyleCompact.Render(sb.String()) + "\n"
}

// RenderErrorCard renders a clean error card wrapping unrecoverable execution errors
func RenderErrorCard(err error) string {
	if err == nil {
		return ""
	}

	var sb strings.Builder
	errMsg := err.Error()

	var tips []string
	if strings.Contains(errMsg, "API") || strings.Contains(errMsg, "Authorization") || strings.Contains(errMsg, "ApiKey") || strings.Contains(errMsg, "接口") || strings.Contains(errMsg, "http") || strings.Contains(errMsg, "调用") || strings.Contains(errMsg, "call") {
		tips = []string{
			"Please check your local network connection and whether the API endpoint (Base URL) is reachable",
			"Confirm you have configured the correct API Key in ~/.iroha.json or environment variables",
			"If you want offline read-only operations, switch to Plan mode by typing /mode plan",
		}
	} else if strings.Contains(errMsg, "权限") || strings.Contains(errMsg, "Permission") || strings.Contains(errMsg, "denied") {
		tips = []string{
			"Please check your system read/write permissions for the target directory or file",
			"Try to keep code changes and test commands within the current workspace directory",
		}
	} else {
		tips = []string{
			"Check if your command-line tools or local Go environment are configured correctly",
			"You can re-enter the command or try different parameters",
		}
	}

	sb.WriteString("  " + lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render("[error]") + " " + errMsg + "\n\n")
	sb.WriteString("  " + StyleKeyHelp.Render("Troubleshooting:") + "\n")
	for i, tip := range tips {
		fmt.Fprintf(&sb, "    %d. %s\n", i+1, StyleKeyHelp.Render(tip))
	}

	return cardStyleCompact.Render(sb.String())
}

// FormatToolArgs extracts and formats key arguments from a tool invocation.
func FormatToolArgs(args any) string {
	if args == nil {
		return ""
	}
	if m, ok := args.(map[string]any); ok {
		if len(m) == 0 {
			return ""
		}
		var parts []string
		for _, key := range []string{"path", "command", "pattern", "query", "text"} {
			if val, exists := m[key]; exists {
				parts = append(parts, fmt.Sprintf("%s: %q", key, val))
			}
		}
		for key, val := range m {
			if key == "path" || key == "command" || key == "pattern" || key == "query" || key == "text" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s: %v", key, val))
		}
		if len(parts) > 0 {
			return "(" + strings.Join(parts, ", ") + ")"
		}
		return ""
	}

	data, err := json.Marshal(args)
	if err == nil && len(data) > 2 {
		return string(data)
	}
	return fmt.Sprintf("%v", args)
}

// FormatToolActivity converts a tool name and arguments into a clear action description
func FormatToolActivity(name string, args any) string {
	var argMap map[string]any
	if m, ok := args.(map[string]any); ok {
		argMap = m
	}

	getStr := func(keys ...string) string {
		if argMap == nil {
			return ""
		}
		for _, k := range keys {
			if val, exists := argMap[k]; exists {
				if str, ok := val.(string); ok {
					return str
				}
				return fmt.Sprintf("%v", val)
			}
		}
		return ""
	}

	switch name {
	case "file_read":
		path := getStr("path", "AbsolutePath", "TargetFile")
		if path != "" {
			return fmt.Sprintf("Read file %s", path)
		}
		return "Read file"
	case "file_write":
		path := getStr("path", "TargetFile", "AbsolutePath")
		if path != "" {
			return fmt.Sprintf("Write file %s", path)
		}
		return "Write file"
	case "file_edit":
		path := getStr("path", "TargetFile", "AbsolutePath")
		if path != "" {
			return fmt.Sprintf("Edit file %s", path)
		}
		return "Edit file"
	case "file_edit_batch":
		return "Apply atomic batch file edits"
	case "list_directory":
		path := getStr("path", "DirectoryPath", "Cwd")
		if path != "" {
			return fmt.Sprintf("List directory %s", path)
		}
		return "List directory"
	case "search_grep":
		pattern := getStr("pattern", "query", "Query")
		if pattern != "" {
			return fmt.Sprintf("Search pattern %q", pattern)
		}
		return "Search pattern"
	case "find_files":
		pattern := getStr("pattern", "Query")
		if pattern != "" {
			return fmt.Sprintf("Find files matching %q", pattern)
		}
		return "Find files"
	case "shell_run":
		cmd := getStr("command", "CommandLine")
		if cmd != "" {
			return fmt.Sprintf("Run terminal command: %s", cmd)
		}
		return "Run terminal command"
	case "todo":
		text := getStr("text", "Text")
		if text != "" {
			return fmt.Sprintf("Update todo %q", text)
		}
		return "Update todo"
	case "memory_save":
		nameVal := getStr("name", "Name")
		if nameVal != "" {
			return fmt.Sprintf("Save cross-session memory %q", nameVal)
		}
		return "Save cross-session memory"
	case "memory_list":
		return "List cross-session memories"
	case "memory_search":
		query := getStr("query", "Query")
		if query != "" {
			return fmt.Sprintf("Search cross-session memories %q", query)
		}
		return "Search cross-session memories"
	case "memory_update":
		nameVal := getStr("name", "Name")
		if nameVal != "" {
			return fmt.Sprintf("Update cross-session memory %q", nameVal)
		}
		return "Update cross-session memory"
	case "memory_delete":
		nameVal := getStr("name", "Name")
		if nameVal != "" {
			return fmt.Sprintf("Delete cross-session memory %q", nameVal)
		}
		return "Delete cross-session memory"
	case "memory_dream":
		return "Consolidate persistent memories"
	case "task_create":
		id := getStr("id", "ID", "TaskId")
		if id != "" {
			return fmt.Sprintf("Create task %s", id)
		}
		return "Create task"
	case "task_update":
		id := getStr("id", "ID", "TaskId")
		if id != "" {
			return fmt.Sprintf("Update task %s", id)
		}
		return "Update task"
	case "task_list":
		return "List tasks"
	case "task_get":
		id := getStr("id", "ID", "TaskId")
		if id != "" {
			return fmt.Sprintf("Get task %s details", id)
		}
		return "Get task details"
	case "background_run":
		cmd := getStr("command", "CommandLine")
		if cmd != "" {
			return fmt.Sprintf("Run background command: %s", cmd)
		}
		return "Run background command"
	case "check_background":
		return "Check background tasks"
	case "schedule_create":
		return "Create scheduled task"
	case "schedule_list":
		return "List scheduled tasks"
	case "schedule_delete":
		return "Delete scheduled task"
	case "spawn_teammate":
		nameVal := getStr("name", "Name")
		if nameVal != "" {
			return fmt.Sprintf("Spawn agent teammate %s", nameVal)
		}
		return "Spawn agent teammate"
	case "list_teammates":
		return "Check agent team status"
	case "send_message":
		recipient := getStr("recipient", "Recipient")
		if recipient != "" {
			return fmt.Sprintf("Send message to agent %s", recipient)
		}
		return "Send message to agent team"
	case "read_inbox":
		return "Read agent inbox"
	case "broadcast":
		return "Broadcast to agent team"
	case "spawn_subagent":
		role := getStr("role", "Role")
		if role != "" {
			return fmt.Sprintf("Spawn subagent %s", role)
		}
		return "Spawn subagent"
	case "web_fetch":
		url := getStr("url", "Url")
		if url != "" {
			return fmt.Sprintf("Fetch web page %s", url)
		}
		return "Fetch web page"
	case "web_search":
		query := getStr("query", "Query")
		if query != "" {
			return fmt.Sprintf("Search the web for %q", query)
		}
		return "Search the web"
	case "worktree_create":
		nameVal := getStr("name", "Name")
		if nameVal != "" {
			return fmt.Sprintf("Create git worktree %s", nameVal)
		}
		return "Create git worktree"
	case "worktree_list":
		return "List git worktrees"
	case "worktree_status":
		return "Check git worktree status"
	case "worktree_enter":
		return "Enter git worktree"
	case "worktree_closeout":
		return "Close/clean up git worktree"
	case "mcp_server_list":
		return "List configured MCP servers"
	case "lsp_goto_definition":
		return "LSP: Go to definition"
	case "lsp_find_references":
		return "LSP: Find references"
	case "lsp_document_symbols":
		return "LSP: Extract document symbols"
	case "lsp_hover":
		return "LSP: Hover symbol"
	case "lsp_diagnostics":
		return "LSP: Fetch server diagnostics"
	default:
		argsStr := FormatToolArgs(args)
		if argsStr != "" {
			return fmt.Sprintf("Call tool %s%s", name, argsStr)
		}
		return fmt.Sprintf("Call tool %s", name)
	}
}

// maxVisibleStreamLines is the maximum number of lines to display in the shell stream area
const maxVisibleStreamLines = 15

// RenderShellStreamArea renders a flat console container showing real-time shell output
func RenderShellStreamArea(lines []string, cmd string, width int) string {
	if len(lines) == 0 {
		return ""
	}

	visibleLines := lines
	truncated := 0
	if len(lines) > maxVisibleStreamLines {
		truncated = len(lines) - maxVisibleStreamLines
		visibleLines = lines[len(lines)-maxVisibleStreamLines:]
	}

	var sb strings.Builder

	// Top boundary line
	sepLen := width - 4
	if sepLen <= 0 {
		sepLen = 40
	}
	separator := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", sepLen))
	sb.WriteString("  " + separator + "\n")

	// Header with command name
	cmdDisplay := cmd
	if len(cmdDisplay) > width-14 {
		cmdDisplay = cmdDisplay[:width-17] + "..."
	}
	sb.WriteString("  " + lipgloss.NewStyle().Foreground(ColorTextMuted).Render("console ") + lipgloss.NewStyle().Foreground(ColorText).Render("$ "+cmdDisplay) + "\n")

	if truncated > 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Italic(true).
			Render(fmt.Sprintf("    ... %d older lines hidden", truncated)))
		sb.WriteString("\n")
	}

	for _, line := range visibleLines {
		sb.WriteString("    " + lipgloss.NewStyle().Foreground(ColorText).Render(line) + "\n")
	}

	// Bottom boundary line
	sb.WriteString("  " + separator + "\n")

	return sb.String()
}

// getToolCategoryTheme returns style details and prefix icons for categorized tools.
func getToolCategoryTheme(name string) (lipgloss.Color, string, string) {
	switch name {
	case "file_read", "file_write", "file_edit", "file_edit_batch", "list_directory", "search_grep", "find_files",
		"lsp_goto_definition", "lsp_find_references", "lsp_document_symbols", "lsp_hover", "lsp_diagnostics":
		return ColorPrimary, "file", "File Operations"
	case "shell_run", "background_run", "check_background", "web_fetch", "web_search":
		return ColorWarning, "cmd", "Command Execution"
	case "spawn_teammate", "list_teammates", "send_message", "read_inbox", "broadcast", "spawn_subagent":
		return ColorSecondary, "agent", "Agent Collaboration"
	default:
		return ColorSecondary, "tool", "External Tools"
	}
}

// RenderToolErrorCard renders a minimal failure card for tool execution
func RenderToolErrorCard(name string, args any, duration time.Duration, err error) string {
	color, icon, _ := getToolCategoryTheme(name)
	activity := FormatToolActivity(name, args)

	failStyled := lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render("✗")
	iconStyled := lipgloss.NewStyle().Foreground(color).Render("[" + icon + "]")
	textStyled := lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render(activity)
	durStyled := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(fmt.Sprintf("(%s)", duration.Round(time.Millisecond).String()))

	var sb strings.Builder
	fmt.Fprintf(&sb, "  %s %s %s %s\n", failStyled, iconStyled, textStyled, durStyled)
	if err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Render(fmt.Sprintf("    ↳ Error: %s", err.Error())))
	} else {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Render("    ↳ Error: operation failed"))
	}

	return sb.String()
}

// RenderToolSuccessCard renders a minimal success log for tool execution
func RenderToolSuccessCard(name string, args any, duration time.Duration) string {
	color, icon, _ := getToolCategoryTheme(name)
	activity := FormatToolActivity(name, args)

	tickStyled := lipgloss.NewStyle().Foreground(ColorSuccess).Render("✓")
	iconStyled := lipgloss.NewStyle().Foreground(color).Render("[" + icon + "]")
	textStyled := lipgloss.NewStyle().Foreground(color).Bold(true).Render(activity)
	durStyled := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(fmt.Sprintf("(%s)", duration.Round(time.Millisecond).String()))

	return fmt.Sprintf("  %s %s %s %s", tickStyled, iconStyled, textStyled, durStyled)
}

// RenderCancelCard renders a compact cancellation notice.
func RenderCancelCard(duration time.Duration) string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render("aborted") + " ")
	sb.WriteString("Session aborted by user\n")
	fmt.Fprintf(&sb, "  duration: %s\n", duration.Round(time.Millisecond))

	sb.WriteString("  time:     " + time.Now().Format("15:04:05") + "\n")
	return sb.String()
}

// RenderHelpDashboard renders the command reference.
func RenderHelpDashboard() string {
	var sb strings.Builder

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render("Iroha Code help") + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 60)) + "\n\n")

	// Keyboard Shortcuts section
	sb.WriteString(lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render("Keyboard Shortcuts") + "\n")

	shortcuts := []struct {
		Keys string
		Desc string
	}{
		{"Ctrl + C", "Abort current thinking and tool calls, or exit idle state"},
		{"Ctrl + Y", "Copy last AI response to system clipboard"},
		{"Ctrl + D / /exit", "Safely save and exit current session"},
		{"PgUp / PgDn", "Scroll the conversation viewport"},
		{"Esc", "Exit session history picker or close slash command autocomplete"},
		{"↑ / ↓ (empty input)", "Browse or cycle through prompt history"},
		{" / + command (e.g. /doc)", "Trigger autocomplete, press Tab or Enter to select"},
	}

	for _, s := range shortcuts {
		fmt.Fprintf(&sb, "    %-18s : %s\n",
			lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render(s.Keys),
			lipgloss.NewStyle().Foreground(ColorTextMuted).Render(s.Desc))

	}
	sb.WriteString("\n")

	// Slash Commands section
	sb.WriteString(lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render("Slash commands") + "\n")
	for _, cmd := range AllSlashCommands {
		fmt.Fprintf(&sb, "    %-18s : %s\n",
			lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true).Render(cmd.Command),
			lipgloss.NewStyle().Foreground(ColorTextMuted).Render(cmd.Description))

	}

	sb.WriteString("\n" + StyleKeyHelp.Render("Type a task, /sessions to switch history, or /doctor to diagnose the environment.") + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 60)) + "\n")

	return cardStyleFlush.Render(sb.String()) + "\n"
}
