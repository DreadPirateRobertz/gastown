// herald_prototype.go — Herald agent prototype for the Wasteland federation.
//
// The Herald polls the upstream wasteland commons database for changes to the
// wanted board and notifies interested towns. It works by taking periodic
// snapshots of the wanted table, diffing consecutive snapshots, and routing
// change notifications through pluggable backends.
//
// This is a prototype deliverable for wasteland completion w-hop-005.
// For MVP, notifications go to stdout. The Notifier interface makes it
// straightforward to add Slack, Discord, or mail backends later.
//
// Usage:
//
//	go run herald_prototype.go
//	go run herald_prototype.go -db ~/.hop/commons/hop/wl-commons -interval 30s
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

// WantedEntry represents a single row from the wanted table.
type WantedEntry struct {
	ID          string
	Title       string
	Description string
	Project     string
	Type        string
	Priority    string
	Tags        string
	PostedBy    string
	ClaimedBy   string
	Status      string
	EffortLevel string
	EvidenceURL string
	CreatedAt   string
	UpdatedAt   string
}

// WantedSnapshot is a point-in-time capture of every row in the wanted table,
// keyed by wanted item ID for efficient lookup during diff.
type WantedSnapshot struct {
	Timestamp time.Time
	Items     map[string]*WantedEntry
}

// ChangeKind classifies the type of change detected between two snapshots.
type ChangeKind string

const (
	ChangeNewItem      ChangeKind = "new_item"
	ChangeStatusChange ChangeKind = "status_change"
	ChangeClaimed      ChangeKind = "claimed"
	ChangeCompleted    ChangeKind = "completed"
	ChangeRemoved      ChangeKind = "removed"
	ChangeUpdated      ChangeKind = "updated"
)

// WantedChange captures a single difference between two snapshots.
type WantedChange struct {
	Kind      ChangeKind
	ItemID    string
	Title     string
	OldStatus string
	NewStatus string
	ClaimedBy string
	Detail    string // human-readable context for the change
}

// WantedDiff represents the complete set of changes between two snapshots.
type WantedDiff struct {
	OldTimestamp time.Time
	NewTimestamp time.Time
	Changes      []WantedChange
}

// HasChanges returns true if the diff contains at least one change.
func (d *WantedDiff) HasChanges() bool {
	return len(d.Changes) > 0
}

// ByKind returns only changes matching the given kind.
func (d *WantedDiff) ByKind(kind ChangeKind) []WantedChange {
	var out []WantedChange
	for _, c := range d.Changes {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// HeraldConfig holds all configuration for the herald agent.
type HeraldConfig struct {
	// DBPath is the absolute path to the local Dolt database directory
	// containing the wl-commons wanted table.
	DBPath string

	// PollInterval controls how frequently the herald checks for changes.
	PollInterval time.Duration

	// Notifiers is the list of notification backends to dispatch to.
	// For MVP this contains a single StdoutNotifier.
	Notifiers []Notifier
}

// DefaultHeraldConfig returns a config pointing at the standard wl-commons
// location with a 60-second poll interval and stdout notifications.
func DefaultHeraldConfig() *HeraldConfig {
	home, _ := os.UserHomeDir()
	return &HeraldConfig{
		DBPath:       filepath.Join(home, ".hop", "commons", "hop", "wl-commons"),
		PollInterval: 60 * time.Second,
		Notifiers:    []Notifier{&StdoutNotifier{}},
	}
}

// ---------------------------------------------------------------------------
// Notification interface and stdout backend
// ---------------------------------------------------------------------------

// Notifier is the interface that notification backends must implement.
// Implementations should be safe for concurrent use if the herald is extended
// to support parallel dispatch.
type Notifier interface {
	// Notify sends a formatted diff to the notification channel.
	// The implementation decides how to render and deliver the message.
	Notify(diff *WantedDiff) error

	// Name returns a human-readable name for the backend (for logging).
	Name() string
}

// StdoutNotifier prints change notifications to stdout. This is the MVP
// backend; production deployments would swap in Slack, Discord, or mail.
type StdoutNotifier struct{}

// Name returns the notifier's display name.
func (s *StdoutNotifier) Name() string { return "stdout" }

// Notify formats and prints the diff to stdout.
func (s *StdoutNotifier) Notify(diff *WantedDiff) error {
	msg := FormatNotification(diff)
	fmt.Print(msg)
	return nil
}

// ---------------------------------------------------------------------------
// Snapshot: query the wanted table via dolt sql
// ---------------------------------------------------------------------------

// SnapshotWanted queries the wanted table in the given Dolt database directory
// and returns a point-in-time snapshot of all rows. It shells out to `dolt sql`
// because the prototype runs outside the Dolt server process.
func SnapshotWanted(dbPath string) (*WantedSnapshot, error) {
	query := `SELECT id, title, COALESCE(description,'') as description, ` +
		`COALESCE(project,'') as project, COALESCE(type,'') as type, ` +
		`COALESCE(priority,'0') as priority, COALESCE(tags,'') as tags, ` +
		`COALESCE(posted_by,'') as posted_by, COALESCE(claimed_by,'') as claimed_by, ` +
		`COALESCE(status,'open') as status, COALESCE(effort_level,'') as effort_level, ` +
		`COALESCE(evidence_url,'') as evidence_url, ` +
		`COALESCE(created_at,'') as created_at, COALESCE(updated_at,'') as updated_at ` +
		`FROM wanted ORDER BY id;`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dolt", "sql", "-r", "csv", "-q", query)
	cmd.Dir = dbPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("dolt sql query failed in %s: %w (%s)",
			dbPath, err, strings.TrimSpace(string(output)))
	}

	snap := &WantedSnapshot{
		Timestamp: time.Now().UTC(),
		Items:     make(map[string]*WantedEntry),
	}

	rows := parseCSV(string(output))
	for _, row := range rows {
		entry := &WantedEntry{
			ID:          row["id"],
			Title:       row["title"],
			Description: row["description"],
			Project:     row["project"],
			Type:        row["type"],
			Priority:    row["priority"],
			Tags:        row["tags"],
			PostedBy:    row["posted_by"],
			ClaimedBy:   row["claimed_by"],
			Status:      row["status"],
			EffortLevel: row["effort_level"],
			EvidenceURL: row["evidence_url"],
			CreatedAt:   row["created_at"],
			UpdatedAt:   row["updated_at"],
		}
		if entry.ID != "" {
			snap.Items[entry.ID] = entry
		}
	}

	return snap, nil
}

// ---------------------------------------------------------------------------
// Diff: compare two snapshots
// ---------------------------------------------------------------------------

// DiffWanted compares an old and new snapshot, returning all detected changes:
// new items, removed items, status transitions, new claims, and completions.
func DiffWanted(old, new *WantedSnapshot) *WantedDiff {
	diff := &WantedDiff{
		OldTimestamp: old.Timestamp,
		NewTimestamp: new.Timestamp,
	}

	// Detect new items and changes to existing items.
	for id, newEntry := range new.Items {
		oldEntry, existed := old.Items[id]
		if !existed {
			diff.Changes = append(diff.Changes, WantedChange{
				Kind:      ChangeNewItem,
				ItemID:    id,
				Title:     newEntry.Title,
				NewStatus: newEntry.Status,
				Detail:    fmt.Sprintf("posted by %s, priority %s, effort %s", newEntry.PostedBy, newEntry.Priority, newEntry.EffortLevel),
			})
			continue
		}

		// Status changed.
		if oldEntry.Status != newEntry.Status {
			kind := ChangeStatusChange

			// Classify specific transitions.
			switch {
			case newEntry.Status == "claimed":
				kind = ChangeClaimed
			case newEntry.Status == "in_review" || newEntry.Status == "completed" || newEntry.Status == "done":
				kind = ChangeCompleted
			}

			diff.Changes = append(diff.Changes, WantedChange{
				Kind:      kind,
				ItemID:    id,
				Title:     newEntry.Title,
				OldStatus: oldEntry.Status,
				NewStatus: newEntry.Status,
				ClaimedBy: newEntry.ClaimedBy,
				Detail:    transitionDetail(oldEntry, newEntry),
			})
			continue
		}

		// Claim changed without status change (shouldn't normally happen, but
		// guard against it).
		if oldEntry.ClaimedBy != newEntry.ClaimedBy && newEntry.ClaimedBy != "" {
			diff.Changes = append(diff.Changes, WantedChange{
				Kind:      ChangeClaimed,
				ItemID:    id,
				Title:     newEntry.Title,
				OldStatus: oldEntry.Status,
				NewStatus: newEntry.Status,
				ClaimedBy: newEntry.ClaimedBy,
				Detail:    fmt.Sprintf("claimed by %s", newEntry.ClaimedBy),
			})
			continue
		}

		// Check for other field updates (description, evidence, etc.).
		if oldEntry.UpdatedAt != newEntry.UpdatedAt {
			diff.Changes = append(diff.Changes, WantedChange{
				Kind:      ChangeUpdated,
				ItemID:    id,
				Title:     newEntry.Title,
				OldStatus: oldEntry.Status,
				NewStatus: newEntry.Status,
				Detail:    "metadata updated",
			})
		}
	}

	// Detect removed items (present in old but not in new).
	for id, oldEntry := range old.Items {
		if _, exists := new.Items[id]; !exists {
			diff.Changes = append(diff.Changes, WantedChange{
				Kind:      ChangeRemoved,
				ItemID:    id,
				Title:     oldEntry.Title,
				OldStatus: oldEntry.Status,
				Detail:    "item removed from wanted board",
			})
		}
	}

	return diff
}

// transitionDetail returns a human-readable description of a status transition.
func transitionDetail(old, new *WantedEntry) string {
	parts := []string{fmt.Sprintf("%s -> %s", old.Status, new.Status)}
	if new.ClaimedBy != "" && old.ClaimedBy != new.ClaimedBy {
		parts = append(parts, fmt.Sprintf("by %s", new.ClaimedBy))
	}
	if new.EvidenceURL != "" && old.EvidenceURL != new.EvidenceURL {
		parts = append(parts, fmt.Sprintf("evidence: %s", new.EvidenceURL))
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// Notification formatting
// ---------------------------------------------------------------------------

// FormatNotification renders a WantedDiff into a human-readable notification
// message suitable for console output. The format is designed to be scannable
// at a glance: a header line, then grouped changes by kind.
func FormatNotification(diff *WantedDiff) string {
	if !diff.HasChanges() {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n=== Wasteland Herald Report [%s] ===\n",
		diff.NewTimestamp.Format("2006-01-02 15:04:05 UTC")))
	b.WriteString(fmt.Sprintf("    %d change(s) detected since %s\n\n",
		len(diff.Changes), diff.OldTimestamp.Format("15:04:05")))

	// Group and print by kind in a logical order.
	groups := []struct {
		kind  ChangeKind
		emoji string
		label string
	}{
		{ChangeNewItem, "[NEW]", "New Wanted Items"},
		{ChangeClaimed, "[CLAIM]", "Newly Claimed"},
		{ChangeCompleted, "[DONE]", "Completions"},
		{ChangeStatusChange, "[STATUS]", "Status Changes"},
		{ChangeUpdated, "[UPDATE]", "Updates"},
		{ChangeRemoved, "[GONE]", "Removed"},
	}

	for _, g := range groups {
		changes := diff.ByKind(g.kind)
		if len(changes) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s %s (%d):\n", g.emoji, g.label, len(changes)))
		for _, c := range changes {
			b.WriteString(fmt.Sprintf("    - %s (%s): %s\n", c.Title, c.ItemID, c.Detail))
		}
		b.WriteString("\n")
	}

	b.WriteString("=== End Herald Report ===\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Poll loop
// ---------------------------------------------------------------------------

// PollLoop runs the main herald loop: snapshot, diff, notify, sleep.
// It respects context cancellation for clean shutdown.
func PollLoop(ctx context.Context, cfg *HeraldConfig) error {
	fmt.Printf("Herald starting: db=%s interval=%s notifiers=%d\n",
		cfg.DBPath, cfg.PollInterval, len(cfg.Notifiers))

	// Verify the database directory exists before entering the loop.
	if _, err := os.Stat(filepath.Join(cfg.DBPath, ".dolt")); os.IsNotExist(err) {
		return fmt.Errorf("no Dolt database at %s (missing .dolt directory)", cfg.DBPath)
	}

	// Take initial snapshot as the baseline.
	prev, err := SnapshotWanted(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("initial snapshot failed: %w", err)
	}
	fmt.Printf("Initial snapshot: %d wanted items\n", len(prev.Items))

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Herald shutting down.")
			return nil
		case <-ticker.C:
			current, err := SnapshotWanted(cfg.DBPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "herald: snapshot error: %v\n", err)
				continue // retry next tick rather than crashing
			}

			diff := DiffWanted(prev, current)
			if diff.HasChanges() {
				for _, n := range cfg.Notifiers {
					if notifyErr := n.Notify(diff); notifyErr != nil {
						fmt.Fprintf(os.Stderr, "herald: notifier %s error: %v\n",
							n.Name(), notifyErr)
					}
				}
			}

			prev = current
		}
	}
}

// ---------------------------------------------------------------------------
// CSV parsing (self-contained; avoids importing encoding/csv for simplicity)
// ---------------------------------------------------------------------------

// parseCSV parses dolt's CSV output into a slice of column-name-keyed maps.
// Handles quoted fields containing commas and escaped double-quotes.
func parseCSV(data string) []map[string]string {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) < 2 {
		return nil
	}

	headers := parseCSVLine(lines[0])
	var result []map[string]string

	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		fields := parseCSVLine(line)
		row := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(fields) {
				row[strings.TrimSpace(h)] = strings.TrimSpace(fields[i])
			}
		}
		result = append(result, row)
	}
	return result
}

// parseCSVLine splits a single CSV line into fields, respecting quoted values.
func parseCSVLine(line string) []string {
	var fields []string
	var field strings.Builder
	inQuote := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '"' && !inQuote:
			inQuote = true
		case ch == '"' && inQuote:
			if i+1 < len(line) && line[i+1] == '"' {
				field.WriteByte('"')
				i++
			} else {
				inQuote = false
			}
		case ch == ',' && !inQuote:
			fields = append(fields, field.String())
			field.Reset()
		default:
			field.WriteByte(ch)
		}
	}
	fields = append(fields, field.String())
	return fields
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	cfg := DefaultHeraldConfig()

	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath,
		"path to the Dolt wl-commons database directory")
	flag.DurationVar(&cfg.PollInterval, "interval", cfg.PollInterval,
		"how often to poll for changes (e.g., 30s, 1m, 5m)")
	flag.Parse()

	// Wire up graceful shutdown on SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := PollLoop(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "herald: fatal: %v\n", err)
		os.Exit(1)
	}
}
