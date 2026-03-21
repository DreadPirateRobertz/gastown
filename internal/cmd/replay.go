package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/agentlog"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Replay command flags
var (
	replayList      bool
	replaySession   string
	replayCount     int
	replaySince     string
	replayVerbose   bool
	replayShowUsage bool
)

var replayCmd = &cobra.Command{
	Use:     "replay [agent-path]",
	GroupID: GroupDiag,
	Short:   "View session audit trail for an agent",
	Long: `View the Claude Code session conversation history for an agent.

Reads JSONL session files from ~/.claude/projects/ and displays the
conversation as a readable audit trail, showing user messages, assistant
responses, and tool calls in chronological order.

AGENT PATH resolves the agent's working directory:
  - omitted: use the current directory
  - "mayor": <town-root>/mayor
  - "gastown/crew/deckard": <town-root>/gastown/crew/deckard

Examples:
  gt replay                              # Show last session for current agent
  gt replay mayor                        # Show last session for mayor
  gt replay gastown/crew/deckard         # Show last session for deckard
  gt replay --list                       # List all sessions for current agent
  gt replay --list mayor                 # List sessions for mayor
  gt replay --session <uuid>             # Show a specific session
  gt replay -n 3                         # Show last 3 sessions
  gt replay --since 24h                  # Sessions from last 24h
  gt replay -v                           # Verbose: include tool inputs/results`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReplay,
}

func init() {
	replayCmd.Flags().BoolVarP(&replayList, "list", "l", false, "List available sessions instead of showing content")
	replayCmd.Flags().StringVarP(&replaySession, "session", "s", "", "Show a specific session by UUID (prefix match)")
	replayCmd.Flags().IntVarP(&replayCount, "count", "n", 1, "Number of most recent sessions to show (0 = all)")
	replayCmd.Flags().StringVar(&replaySince, "since", "", "Show sessions from last duration (e.g., 1h, 24h, 7d)")
	replayCmd.Flags().BoolVarP(&replayVerbose, "verbose", "v", false, "Show tool inputs and results")
	replayCmd.Flags().BoolVar(&replayShowUsage, "usage", false, "Show token usage summary per session")
	rootCmd.AddCommand(replayCmd)
}

// replayRawEntry is the full JSONL entry structure for Claude Code sessions.
type replayRawEntry struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	UUID      string          `json:"uuid"`
	ParentUUID string         `json:"parentUuid"`
	Timestamp string          `json:"timestamp"`
	GitBranch string          `json:"gitBranch"`
	CWD       string          `json:"cwd"`
	Message   *replayMessage  `json:"message,omitempty"`
}

type replayMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model,omitempty"`
	Usage   *replayUsage    `json:"usage,omitempty"`
}

type replayUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type replayContentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	Thinking string         `json:"thinking,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

// sessionInfo holds metadata about a session.
type sessionInfo struct {
	UUID      string
	Path      string
	Start     time.Time
	End       time.Time
	Branch    string
	MsgCount  int
}

func runReplay(cmd *cobra.Command, args []string) error {
	workDir, err := resolveReplayWorkDir(args)
	if err != nil {
		return err
	}

	projectDir, err := agentlog.ProjectDirForWorkDir(workDir)
	if err != nil {
		return fmt.Errorf("resolving Claude project dir: %w", err)
	}

	// Parse --since
	var sinceTime time.Time
	if replaySince != "" {
		dur, err := parseDuration(replaySince)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		sinceTime = time.Now().Add(-dur)
	}

	// Collect sessions
	sessions, err := listSessions(projectDir, sinceTime)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Printf("%s No sessions found in %s\n", style.Dim.Render("○"), projectDir)
		return nil
	}

	// Sort newest first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Start.After(sessions[j].Start)
	})

	if replayList {
		return printSessionList(sessions)
	}

	// Find sessions to show
	var toShow []sessionInfo
	if replaySession != "" {
		// Show specific session by UUID prefix
		for _, s := range sessions {
			if strings.HasPrefix(s.UUID, replaySession) {
				toShow = append(toShow, s)
				break
			}
		}
		if len(toShow) == 0 {
			return fmt.Errorf("session %q not found", replaySession)
		}
	} else if replayCount == 0 {
		toShow = sessions
	} else {
		n := replayCount
		if n > len(sessions) {
			n = len(sessions)
		}
		toShow = sessions[:n]
	}

	// Show in chronological order (oldest first)
	sort.Slice(toShow, func(i, j int) bool {
		return toShow[i].Start.Before(toShow[j].Start)
	})

	for i, s := range toShow {
		if i > 0 {
			fmt.Println()
		}
		if err := printSession(s, replayVerbose, replayShowUsage); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read session %s: %v\n", s.UUID[:8], err)
		}
	}

	return nil
}

// resolveReplayWorkDir resolves the working directory for replay from args.
func resolveReplayWorkDir(args []string) (string, error) {
	if len(args) == 0 {
		// Use cwd
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting current directory: %w", err)
		}
		return cwd, nil
	}

	agentPath := args[0]

	// Resolve relative to town root
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		// If not in a workspace, try treating arg as absolute path
		if filepath.IsAbs(agentPath) {
			return agentPath, nil
		}
		return "", fmt.Errorf("not in a Gas Town workspace and path is not absolute: %w", err)
	}

	return filepath.Join(townRoot, agentPath), nil
}

// listSessions scans the project dir and collects session metadata.
func listSessions(projectDir string, since time.Time) ([]sessionInfo, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []sessionInfo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !since.IsZero() && info.ModTime().Before(since) {
			continue
		}

		uuid := strings.TrimSuffix(e.Name(), ".jsonl")
		path := filepath.Join(projectDir, e.Name())

		si := sessionInfo{
			UUID: uuid,
			Path: path,
			End:  info.ModTime(),
		}

		// Quick scan for metadata (first and last conversation entries)
		scanSessionMeta(&si)
		sessions = append(sessions, si)
	}

	return sessions, nil
}

// scanSessionMeta reads a JSONL file to extract session metadata without
// loading the entire file into memory.
func scanSessionMeta(si *sessionInfo) {
	f, err := os.Open(si.Path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	var first, last time.Time
	for scanner.Scan() {
		var entry replayRawEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}

		si.MsgCount++
		if entry.GitBranch != "" && si.Branch == "" {
			si.Branch = entry.GitBranch
		}

		ts := parseReplayTimestamp(entry.Timestamp)
		if !ts.IsZero() {
			if first.IsZero() || ts.Before(first) {
				first = ts
			}
			if last.IsZero() || ts.After(last) {
				last = ts
			}
		}
	}

	if !first.IsZero() {
		si.Start = first
	}
	if !last.IsZero() {
		si.End = last
	}
}

// printSessionList prints a table of available sessions.
func printSessionList(sessions []sessionInfo) error {
	fmt.Printf("%s %d sessions\n\n", style.Bold.Render("→"), len(sessions))

	for _, s := range sessions {
		uuid8 := s.UUID
		if len(uuid8) > 8 {
			uuid8 = uuid8[:8]
		}

		dateStr := ""
		if !s.Start.IsZero() {
			dateStr = s.Start.Local().Format("2006-01-02 15:04")
		}

		durationStr := ""
		if !s.Start.IsZero() && !s.End.IsZero() && s.End.After(s.Start) {
			dur := s.End.Sub(s.Start).Round(time.Minute)
			durationStr = fmt.Sprintf(" (%s)", dur)
		}

		branchStr := ""
		if s.Branch != "" {
			branchStr = style.Dim.Render(" · " + s.Branch)
		}

		fmt.Printf("  %s  %s%s  %s msgs%s\n",
			style.Bold.Render(uuid8),
			dateStr,
			durationStr,
			style.Dim.Render(fmt.Sprintf("%d", s.MsgCount)),
			branchStr,
		)
	}

	return nil
}

// printSession displays the full conversation for a session.
func printSession(si sessionInfo, verbose, showUsage bool) error {
	uuid8 := si.UUID
	if len(uuid8) > 8 {
		uuid8 = uuid8[:8]
	}

	// Header
	headerParts := []string{uuid8}
	if !si.Start.IsZero() {
		headerParts = append(headerParts, si.Start.Local().Format("2006-01-02 15:04"))
	}
	if si.Branch != "" {
		headerParts = append(headerParts, si.Branch)
	}
	fmt.Printf("%s\n", style.Bold.Render("── session "+strings.Join(headerParts, " · ")+" ──"))

	f, err := os.Open(si.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Token usage totals
	var totalIn, totalOut, totalCacheRead, totalCacheCreate int

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1*1024*1024), 1*1024*1024)

	for scanner.Scan() {
		var entry replayRawEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}
		if entry.Message == nil {
			continue
		}

		ts := parseReplayTimestamp(entry.Timestamp)
		tsStr := ""
		if !ts.IsZero() {
			tsStr = ts.Local().Format("15:04:05") + " "
		}

		// Accumulate token usage
		if entry.Message.Usage != nil {
			u := entry.Message.Usage
			totalIn += u.InputTokens
			totalOut += u.OutputTokens
			totalCacheRead += u.CacheReadInputTokens
			totalCacheCreate += u.CacheCreationInputTokens
		}

		role := strings.ToUpper(entry.Message.Role)
		roleStr := formatReplayRole(role)

		// Parse content
		lines := parseReplayContent(entry.Message.Content, verbose)
		for i, line := range lines {
			if i == 0 {
				fmt.Printf("%s%s %s\n", style.Dim.Render(tsStr), roleStr, line)
			} else {
				// Continuation lines are indented
				indent := strings.Repeat(" ", len(tsStr)+len(role)+3)
				fmt.Printf("%s%s\n", indent, line)
			}
		}
	}

	if showUsage && (totalIn > 0 || totalOut > 0 || totalCacheRead > 0) {
		fmt.Printf("\n%s in=%d out=%d cache_read=%d cache_create=%d\n",
			style.Dim.Render("  tokens:"),
			totalIn, totalOut, totalCacheRead, totalCacheCreate,
		)
	}

	return scanner.Err()
}

// formatReplayRole returns a styled role string.
func formatReplayRole(role string) string {
	switch role {
	case "USER":
		return style.Bold.Render("[USER]")
	case "ASSISTANT":
		return style.Success.Render("[ASST]")
	default:
		return fmt.Sprintf("[%s]", role)
	}
}

// parseReplayContent parses a message content field and returns display lines.
// Content can be a JSON string or array of content blocks.
func parseReplayContent(raw json.RawMessage, verbose bool) []string {
	if len(raw) == 0 {
		return nil
	}

	// Try plain string
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []string{truncateReplayText(text, 200)}
	}

	// Try array of blocks
	var blocks []replayContentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return []string{truncateReplayText(string(raw), 200)}
	}

	var lines []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				// Show first line + truncation indicator
				lines = append(lines, formatReplayText(b.Text))
			}
		case "thinking":
			if b.Thinking != "" {
				lines = append(lines, style.Dim.Render("<thinking> "+truncateReplayText(b.Thinking, 80)))
			}
		case "tool_use":
			if verbose && len(b.Input) > 0 {
				lines = append(lines, style.Dim.Render("tool: ")+b.Name+" "+truncateReplayText(string(b.Input), 120))
			} else {
				lines = append(lines, style.Dim.Render("tool: ")+b.Name)
			}
		case "tool_result":
			if verbose {
				content := extractToolResultContent(b.Content)
				if b.IsError {
					lines = append(lines, style.Error.Render("tool_result [error]: ")+truncateReplayText(content, 120))
				} else {
					lines = append(lines, style.Dim.Render("tool_result: ")+truncateReplayText(content, 120))
				}
			}
		}
	}

	return lines
}

// extractToolResultContent extracts readable text from tool_result content.
// Content may be a string, array, or null.
func extractToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}

	// Try array of content blocks
	var blocks []replayContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}

	return string(raw)
}

// formatReplayText formats a text block for display, handling multiline content.
func formatReplayText(text string) string {
	// Find first non-empty line
	lines := strings.Split(text, "\n")
	var firstLine string
	var remaining int
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			if firstLine == "" {
				firstLine = strings.TrimSpace(l)
			} else {
				remaining++
			}
		}
	}

	firstLine = truncateReplayText(firstLine, 160)

	if remaining > 0 {
		return firstLine + style.Dim.Render(fmt.Sprintf(" [+%d lines]", remaining))
	}
	return firstLine
}

// truncateReplayText truncates a string to maxLen characters.
func truncateReplayText(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// Collapse whitespace for inline display
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// parseReplayTimestamp parses a timestamp string from a JSONL entry.
func parseReplayTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Claude Code uses RFC3339 with milliseconds
	for _, format := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
