package reaper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDBName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"hq", false},
		{"beads", false},
		{"gt", false},
		{"test_db_123", false},
		{"", true},
		{"drop table", true},
		{"db;--", true},
		{"db`name", true},
		{"../etc/passwd", true},
	}
	for _, tt := range tests {
		err := ValidateDBName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateDBName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestDefaultDatabases(t *testing.T) {
	if len(DefaultDatabases) == 0 {
		t.Error("DefaultDatabases should not be empty")
	}
	for _, db := range DefaultDatabases {
		if err := ValidateDBName(db); err != nil {
			t.Errorf("DefaultDatabases contains invalid name %q: %v", db, err)
		}
	}
}

func TestFormatJSON(t *testing.T) {
	result := FormatJSON(map[string]int{"count": 42})
	if result == "" {
		t.Error("FormatJSON should not return empty string")
	}
	if result[0] != '{' {
		t.Errorf("FormatJSON should return JSON object, got %q", result[:10])
	}
}

func TestParentExcludeJoin(t *testing.T) {
	joinClause, whereCondition := parentExcludeJoin("testdb")

	// JOIN clause should reference the correct database.
	if joinClause == "" {
		t.Error("parentExcludeJoin joinClause should not be empty")
	}
	if !contains(joinClause, "`testdb`") {
		t.Error("parentExcludeJoin joinClause should reference the database")
	}

	// JOIN should select wisps with open parents from wisp_dependencies.
	if !contains(joinClause, "wisp_dependencies") {
		t.Error("parentExcludeJoin should query wisp_dependencies")
	}
	if !contains(joinClause, "parent-child") {
		t.Error("parentExcludeJoin should filter on parent-child type")
	}
	if !contains(joinClause, "'open', 'hooked', 'in_progress'") {
		t.Error("parentExcludeJoin should check for open parent statuses")
	}

	// WHERE condition should be an IS NULL anti-join filter.
	if whereCondition == "" {
		t.Error("parentExcludeJoin whereCondition should not be empty")
	}
	if !contains(whereCondition, "IS NULL") {
		t.Error("parentExcludeJoin whereCondition should use IS NULL for anti-join")
	}
}

func TestScanDoltDataDir(t *testing.T) {
	// Create a fake town root with .dolt-data directory
	townRoot := t.TempDir()
	dataDir := filepath.Join(townRoot, ".dolt-data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create valid database directories with .dolt/noms/manifest
	for _, dbName := range []string{"hq", "gastown", "beads", "wyvern", "sky"} {
		manifestDir := filepath.Join(dataDir, dbName, ".dolt", "noms")
		if err := os.MkdirAll(manifestDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(manifestDir, "manifest"), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a test pollution database (should be filtered)
	testDBDir := filepath.Join(dataDir, "testdb_abc", ".dolt", "noms")
	if err := os.MkdirAll(testDBDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testDBDir, "manifest"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a directory without .dolt/noms/manifest (should be skipped)
	if err := os.MkdirAll(filepath.Join(dataDir, "incomplete"), 0755); err != nil {
		t.Fatal(err)
	}

	dbs := scanDoltDataDir(townRoot)
	if len(dbs) != 5 {
		t.Errorf("expected 5 databases, got %d: %v", len(dbs), dbs)
	}

	// Verify test pollution was filtered
	for _, db := range dbs {
		if db == "testdb_abc" {
			t.Error("test pollution database should be filtered out")
		}
		if db == "incomplete" {
			t.Error("incomplete database should be filtered out")
		}
	}
}

func TestScanDoltDataDir_EmptyTownRoot(t *testing.T) {
	dbs := scanDoltDataDir("")
	if dbs != nil {
		t.Errorf("expected nil for empty townRoot, got %v", dbs)
	}
}

func TestScanDoltDataDir_MissingDir(t *testing.T) {
	dbs := scanDoltDataDir("/nonexistent/path")
	if dbs != nil {
		t.Errorf("expected nil for missing dir, got %v", dbs)
	}
}

func TestDiscoverDatabasesFromDir_FallbackToSQL(t *testing.T) {
	// With empty townRoot, should fall back to SQL discovery (which will
	// fail to connect and return DefaultDatabases)
	dbs := DiscoverDatabasesFromDir("", "127.0.0.1", 1)
	if len(dbs) == 0 {
		t.Error("should return at least DefaultDatabases as fallback")
	}
}

func TestDiscoverDatabasesFromDir_FilesystemFirst(t *testing.T) {
	// Create a fake town root with databases
	townRoot := t.TempDir()
	dataDir := filepath.Join(townRoot, ".dolt-data")
	for _, dbName := range []string{"myrig", "otherrig"} {
		manifestDir := filepath.Join(dataDir, dbName, ".dolt", "noms")
		if err := os.MkdirAll(manifestDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(manifestDir, "manifest"), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Should find databases via filesystem, not SQL (port 1 will fail)
	dbs := DiscoverDatabasesFromDir(townRoot, "127.0.0.1", 1)
	if len(dbs) != 2 {
		t.Errorf("expected 2 databases from filesystem, got %d: %v", len(dbs), dbs)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
