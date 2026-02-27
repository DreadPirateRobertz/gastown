package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultJsonlGitBackupInterval = 15 * time.Minute
	jsonlExportTimeout            = 60 * time.Second
	gitPushTimeout                = 120 * time.Second
	gitCmdTimeout                 = 30 * time.Second
	maxConsecutivePushFailures    = 3
)

// validDBName matches safe database names (alphanumeric + underscore only).
var validDBName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// scrubQuery is the WHERE clause for filtering ephemeral data.
// Kept separate from Sprintf to avoid %% confusion.
// The query selects only durable work product (bugs, features, tasks, epics, chores).
const scrubWhereClause = ` WHERE (ephemeral IS NULL OR ephemeral != 1)` +
	` AND issue_type NOT IN ('message', 'event', 'agent', 'convoy', 'molecule', 'role', 'merge-request', 'rig')` +
	` AND id NOT LIKE '%-wisp-%'` +
	` AND id NOT LIKE '%-cv-%'` +
	` AND id NOT LIKE 'test%'` +
	` AND id NOT LIKE 'beads\_t%'` +
	` AND id NOT LIKE 'beads\_pt%'` +
	` AND id NOT LIKE 'doctest\_%'` +
	` ORDER BY id`

// jsonlGitBackupInterval returns the configured interval, or the default (15m).
func jsonlGitBackupInterval(config *DaemonPatrolConfig) time.Duration {
	if config != nil && config.Patrols != nil && config.Patrols.JsonlGitBackup != nil {
		if config.Patrols.JsonlGitBackup.IntervalStr != "" {
			if d, err := time.ParseDuration(config.Patrols.JsonlGitBackup.IntervalStr); err == nil && d > 0 {
				return d
			}
		}
	}
	return defaultJsonlGitBackupInterval
}

// syncJsonlGitBackup exports issues from each database to JSONL, scrubs ephemeral data,
// and commits/pushes to a git repository.
// Non-fatal: errors are logged but don't stop the daemon.
func (d *Daemon) syncJsonlGitBackup() {
	if !IsPatrolEnabled(d.patrolConfig, "jsonl_git_backup") {
		return
	}

	config := d.patrolConfig.Patrols.JsonlGitBackup

	// Resolve git repo path.
	gitRepo := config.GitRepo
	if gitRepo == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			d.logger.Printf("jsonl_git_backup: cannot determine home dir: %v", err)
			return
		}
		gitRepo = filepath.Join(homeDir, ".dolt-archive", "git")
	}

	// Verify git repo exists.
	if _, err := os.Stat(filepath.Join(gitRepo, ".git")); os.IsNotExist(err) {
		d.logger.Printf("jsonl_git_backup: git repo %s does not exist, skipping", gitRepo)
		return
	}

	// Determine whether to scrub (default true).
	scrub := true
	if config.Scrub != nil {
		scrub = *config.Scrub
	}

	// Get database list.
	databases := config.Databases
	if len(databases) == 0 {
		d.logger.Printf("jsonl_git_backup: no databases configured, skipping")
		return
	}

	// Resolve Dolt data dir for auto-discovery of running server.
	var dataDir string
	if d.doltServer != nil && d.doltServer.IsEnabled() && d.doltServer.config.DataDir != "" {
		dataDir = d.doltServer.config.DataDir
	} else {
		dataDir = filepath.Join(d.config.TownRoot, ".dolt-data")
	}
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		d.logger.Printf("jsonl_git_backup: data dir %s does not exist, skipping", dataDir)
		return
	}

	d.logger.Printf("jsonl_git_backup: exporting %d database(s) to %s (scrub=%v)", len(databases), gitRepo, scrub)

	exported := 0
	var failed []string
	counts := make(map[string]int)
	for _, db := range databases {
		n, err := d.exportDatabaseToJsonl(db, gitRepo, dataDir, scrub)
		if err != nil {
			d.logger.Printf("jsonl_git_backup: %s: export failed: %v", db, err)
			failed = append(failed, db)
		} else {
			counts[db] = n
			exported++
		}
	}

	if exported == 0 {
		d.logger.Printf("jsonl_git_backup: no databases exported successfully")
		return
	}

	// Commit and push if anything changed.
	// Include failed databases in commit message so staleness is visible.
	if err := d.commitAndPushJsonlBackup(gitRepo, databases, counts, failed); err != nil {
		d.logger.Printf("jsonl_git_backup: git operations failed: %v", err)
		d.jsonlPushFailures++
		if d.jsonlPushFailures >= maxConsecutivePushFailures {
			d.logger.Printf("jsonl_git_backup: ESCALATION: %d consecutive push failures", d.jsonlPushFailures)
			d.escalate("jsonl_git_backup", fmt.Sprintf("git push failed %d consecutive times", d.jsonlPushFailures))
			// Reset to avoid flooding escalations every tick.
			d.jsonlPushFailures = 0
		}
	} else {
		d.jsonlPushFailures = 0
	}

	d.logger.Printf("jsonl_git_backup: exported %d/%d database(s)", exported, len(databases))
}

// supplementalTables lists non-issues tables to include in JSONL backup.
// These contain structural data (dependencies, labels, config) that would be
// lost if we only backed up the issues table. Wisp tables are excluded — they
// contain high-volume ephemeral data handled by the Reaper Dog.
var supplementalTables = []string{
	"comments",
	"config",
	"dependencies",
	"events",
	"labels",
	"metadata",
}

// exportDatabaseToJsonl exports the issues table (with optional scrub) and all
// supplemental tables to JSONL files in {gitRepo}/{db}/ directory.
//
// Issues go to {db}/issues.jsonl (scrubbed). Other tables go to {db}/{table}.jsonl.
// Also writes a legacy {db}.jsonl (symlink to {db}/issues.jsonl) for backward compat.
//
// Returns the total number of records exported across all tables.
func (d *Daemon) exportDatabaseToJsonl(db, gitRepo, dataDir string, scrub bool) (int, error) {
	if !validDBName.MatchString(db) {
		return 0, fmt.Errorf("invalid database name: %q", db)
	}

	// Create per-database subdirectory.
	dbDir := filepath.Join(gitRepo, db)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return 0, fmt.Errorf("creating dir %s: %w", dbDir, err)
	}

	total := 0

	// 1. Export issues table (with scrub filter).
	var query string
	if scrub {
		query = "SELECT * FROM `" + db + "`.issues" + scrubWhereClause
	} else {
		query = "SELECT * FROM `" + db + "`.issues ORDER BY id"
	}
	n, err := d.exportTableToJsonl(db, "issues", query, dbDir, dataDir)
	if err != nil {
		return 0, fmt.Errorf("issues: %w", err)
	}
	total += n

	// Also write legacy flat file for backward compatibility.
	legacyPath := filepath.Join(gitRepo, db+".jsonl")
	newIssuesPath := filepath.Join(dbDir, "issues.jsonl")
	// Copy instead of symlink for git compatibility.
	if data, err := os.ReadFile(newIssuesPath); err == nil {
		_ = os.WriteFile(legacyPath, data, 0644)
	}

	// 2. Export supplemental tables (no scrub, full export).
	for _, table := range supplementalTables {
		tQuery := fmt.Sprintf("SELECT * FROM `%s`.`%s` ORDER BY 1", db, table)
		tn, err := d.exportTableToJsonl(db, table, tQuery, dbDir, dataDir)
		if err != nil {
			// Non-fatal for supplemental tables — log and continue.
			d.logger.Printf("jsonl_git_backup: %s/%s: export failed (non-fatal): %v", db, table, err)
			continue
		}
		total += tn
	}

	d.logger.Printf("jsonl_git_backup: %s: exported %d records across %d tables", db, total, 1+len(supplementalTables))
	return total, nil
}

// exportTableToJsonl runs a query and writes the result as JSONL to {dir}/{table}.jsonl.
// Returns the number of records exported.
func (d *Daemon) exportTableToJsonl(db, table, query, dir, dataDir string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), jsonlExportTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dolt", "sql", "-r", "json", "-q", query)
	cmd.Dir = dataDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return 0, fmt.Errorf("%s: %s", err, errMsg)
		}
		return 0, err
	}

	var result struct {
		Rows []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return 0, fmt.Errorf("parsing dolt output: %w", err)
	}

	outPath := filepath.Join(dir, table+".jsonl")
	tmpPath := outPath + ".tmp"

	var buf bytes.Buffer
	for _, row := range result.Rows {
		var compact bytes.Buffer
		if err := json.Compact(&compact, row); err != nil {
			return 0, fmt.Errorf("compacting JSON row: %w", err)
		}
		buf.Write(compact.Bytes())
		buf.WriteByte('\n')
	}

	if err := os.WriteFile(tmpPath, buf.Bytes(), 0644); err != nil {
		return 0, fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("renaming %s: %w", tmpPath, err)
	}

	return len(result.Rows), nil
}

// commitAndPushJsonlBackup stages, commits, and pushes JSONL files if changed.
// The commit message includes counts for successful exports AND names of failed
// databases, so partial failures are visible in git history.
func (d *Daemon) commitAndPushJsonlBackup(gitRepo string, databases []string, counts map[string]int, failed []string) error {
	// Stage all JSONL files (flat legacy files + subdirectory structure).
	if err := d.runGitCmd(gitRepo, gitCmdTimeout, "add", "-A", "*.jsonl", "*/"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there are staged changes.
	if err := d.runGitCmd(gitRepo, gitCmdTimeout, "diff", "--cached", "--quiet"); err == nil {
		d.logger.Printf("jsonl_git_backup: no changes to commit")
		return nil
	}

	// Build commit message with counts in deterministic order.
	timestamp := time.Now().Format("2006-01-02 15:04")
	var parts []string
	for _, db := range databases {
		if n, ok := counts[db]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", db, n))
		}
	}
	msg := fmt.Sprintf("backup %s: %s", timestamp, strings.Join(parts, " "))
	if len(failed) > 0 {
		sort.Strings(failed)
		msg += fmt.Sprintf(" [FAILED: %s]", strings.Join(failed, ", "))
	}

	// Commit.
	if err := d.runGitCmd(gitRepo, gitCmdTimeout, "commit", "-m", msg,
		"--author=Gas Town Daemon <daemon@gastown.local>"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// Push — use longer timeout since push involves network I/O.
	if err := d.runGitCmd(gitRepo, gitPushTimeout, "push", "origin", "main"); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	d.logger.Printf("jsonl_git_backup: committed and pushed: %s", msg)
	return nil
}

// runGitCmd runs a git command in the specified directory with the given timeout.
func (d *Daemon) runGitCmd(dir string, timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("%s", errMsg)
		}
		return err
	}
	return nil
}

// escalate sends an escalation message to the mayor via gt escalate.
func (d *Daemon) escalate(source, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gt", "escalate", "-s", "HIGH",
		fmt.Sprintf("%s: %s", source, message))
	cmd.Dir = d.config.TownRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		d.logger.Printf("jsonl_git_backup: escalation failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}
