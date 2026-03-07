package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/wasteland"
	"github.com/steveyegge/gastown/internal/workspace"
)

var wlRepJSON bool

var wlRepCmd = &cobra.Command{
	Use:   "rep [handle]",
	Short: "Show reputation score for a rig",
	Long: `Display the reputation score for a rig based on their stamps.

Computes a weighted reputation from stamp dimensions (quality, reliability,
creativity), weighted by confidence and severity (root > branch > leaf).

If no handle is given, shows your own reputation.

Examples:
  gt wl rep                  # Your reputation
  gt wl rep alice-dev        # Alice's reputation
  gt wl rep --json           # JSON output`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWLRep,
}

func init() {
	wlRepCmd.Flags().BoolVar(&wlRepJSON, "json", false, "Output as JSON")
	wlCmd.AddCommand(wlRepCmd)
}

func runWLRep(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		return fmt.Errorf("dolt not found in PATH — install from https://docs.dolthub.com/introduction/installation")
	}

	// Determine handle
	handle := ""
	if len(args) > 0 {
		handle = args[0]
	} else {
		cfg, err := wasteland.LoadConfig(townRoot)
		if err != nil {
			return fmt.Errorf("loading wasteland config (pass a handle or join first): %w", err)
		}
		handle = cfg.RigHandle
	}

	// Find the wl-commons data source
	cloneDir, tmpDir, err := resolveWLCommonsDir(townRoot, doltPath)
	if err != nil {
		return err
	}
	if tmpDir != "" {
		defer os.RemoveAll(tmpDir)
	}

	stamps, err := queryStampsForRig(doltPath, cloneDir, handle)
	if err != nil {
		return err
	}

	completionCount, err := queryCompletionCount(doltPath, cloneDir, handle)
	if err != nil {
		return err
	}

	rep := wasteland.ComputeReputation(handle, stamps)

	if wlRepJSON {
		return renderRepJSON(rep, completionCount)
	}
	renderRepTable(rep, completionCount)
	return nil
}

// resolveWLCommonsDir finds the wl-commons database, either from a local fork
// or by cloning from DoltHub. Returns (dir, tmpDir, error) where tmpDir is
// non-empty if a temp clone was created (caller must clean up).
func resolveWLCommonsDir(townRoot, doltPath string) (string, string, error) {
	// Try wasteland config first
	if cfg, err := wasteland.LoadConfig(townRoot); err == nil && cfg.LocalDir != "" {
		if _, err := os.Stat(filepath.Join(cfg.LocalDir, ".dolt")); err == nil {
			return cfg.LocalDir, "", nil
		}
	}

	// Try standard locations
	if dir := findWLCommonsFork(townRoot); dir != "" {
		return dir, "", nil
	}

	// Clone to temp dir
	tmpDir, err := os.MkdirTemp("", "wl-rep-*")
	if err != nil {
		return "", "", fmt.Errorf("creating temp directory: %w", err)
	}

	cloneDir := filepath.Join(tmpDir, "wl-commons")
	remote := "hop/wl-commons"
	fmt.Printf("Cloning %s...\n", style.Bold.Render(remote))

	cloneCmd := exec.Command(doltPath, "clone", remote, cloneDir)
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("cloning %s: %w", remote, err)
	}

	return cloneDir, tmpDir, nil
}

func queryStampsForRig(doltPath, cloneDir, handle string) ([]wasteland.Stamp, error) {
	query := fmt.Sprintf(
		`SELECT id, author, subject, valence, confidence, COALESCE(severity, 'leaf') as severity, COALESCE(skill_tags, '[]') as skill_tags FROM stamps WHERE subject = '%s' ORDER BY created_at DESC`,
		escapeSQLArg(handle))

	sqlCmd := exec.Command(doltPath, "sql", "-q", query, "-r", "csv")
	sqlCmd.Dir = cloneDir
	output, err := sqlCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("query stamps: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("query stamps: %w", err)
	}

	rows := wlParseCSV(string(output))
	if len(rows) <= 1 {
		return nil, nil
	}

	var stamps []wasteland.Stamp
	for _, row := range rows[1:] {
		if len(row) < 7 {
			continue
		}

		valence, err := wasteland.ParseValence(row[3])
		if err != nil {
			continue // skip malformed stamps
		}

		confidence := 1.0
		if row[4] != "" {
			fmt.Sscanf(row[4], "%f", &confidence)
		}

		var tags []string
		if row[6] != "" && row[6] != "[]" {
			_ = json.Unmarshal([]byte(row[6]), &tags)
		}

		stamps = append(stamps, wasteland.Stamp{
			ID:         row[0],
			Author:     row[1],
			Subject:    row[2],
			Valence:    valence,
			Confidence: confidence,
			Severity:   row[5],
			SkillTags:  tags,
		})
	}

	return stamps, nil
}

func queryCompletionCount(doltPath, cloneDir, handle string) (int, error) {
	query := fmt.Sprintf(
		`SELECT COUNT(*) as cnt FROM completions WHERE completed_by = '%s'`,
		escapeSQLArg(handle))

	sqlCmd := exec.Command(doltPath, "sql", "-q", query, "-r", "csv")
	sqlCmd.Dir = cloneDir
	output, err := sqlCmd.Output()
	if err != nil {
		return 0, nil // non-fatal
	}

	rows := wlParseCSV(string(output))
	if len(rows) >= 2 && len(rows[1]) >= 1 {
		var count int
		fmt.Sscanf(rows[1][0], "%d", &count)
		return count, nil
	}
	return 0, nil
}

func escapeSQLArg(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "'", "''")
}

func renderRepTable(rep *wasteland.ReputationScore, completions int) {
	fmt.Printf("\n%s Reputation: %s\n\n", style.Bold.Render("~"), style.Bold.Render(rep.Handle))

	if rep.StampCount == 0 {
		fmt.Printf("  No stamps received yet.\n")
		if completions > 0 {
			fmt.Printf("  Completions: %d (awaiting validation)\n", completions)
		}
		fmt.Printf("\n  Earn stamps by completing wanted items and getting validated.\n")
		return
	}

	fmt.Printf("  Composite Score:  %s / 5.00\n\n", style.Bold.Render(fmt.Sprintf("%.2f", rep.Composite)))

	fmt.Printf("  %-14s  %s  %s\n", "DIMENSION", "SCORE", "STAMPS")
	fmt.Printf("  %-14s  %s  %s\n", strings.Repeat("-", 14), strings.Repeat("-", 5), strings.Repeat("-", 6))
	fmt.Printf("  %-14s  %5.2f  %6d\n", "Quality", rep.Quality.Score, rep.Quality.Count)
	fmt.Printf("  %-14s  %5.2f  %6d\n", "Reliability", rep.Reliability.Score, rep.Reliability.Count)
	fmt.Printf("  %-14s  %5.2f  %6d\n", "Creativity", rep.Creativity.Score, rep.Creativity.Count)

	fmt.Printf("\n  Total stamps:      %d\n", rep.StampCount)
	fmt.Printf("  Total completions: %d\n", completions)

	if len(rep.SkillMap) > 0 {
		fmt.Printf("\n  Skills: %s\n", formatSkillMap(rep.SkillMap))
	}
}

func renderRepJSON(rep *wasteland.ReputationScore, completions int) error {
	out := map[string]interface{}{
		"handle":    rep.Handle,
		"composite": rep.Composite,
		"quality": map[string]interface{}{
			"score": rep.Quality.Score,
			"count": rep.Quality.Count,
		},
		"reliability": map[string]interface{}{
			"score": rep.Reliability.Score,
			"count": rep.Reliability.Count,
		},
		"creativity": map[string]interface{}{
			"score": rep.Creativity.Score,
			"count": rep.Creativity.Count,
		},
		"stamp_count":      rep.StampCount,
		"completion_count":  completions,
		"skills":           rep.SkillMap,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func formatSkillMap(skills map[string]int) string {
	type kv struct {
		Key   string
		Count int
	}
	var sorted []kv
	for k, v := range skills {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})

	var parts []string
	for _, s := range sorted {
		if s.Count > 1 {
			parts = append(parts, fmt.Sprintf("%s(%d)", s.Key, s.Count))
		} else {
			parts = append(parts, s.Key)
		}
	}
	return strings.Join(parts, ", ")
}
