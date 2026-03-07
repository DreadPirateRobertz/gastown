package beads

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureConfigYAMLIfMissing_DoesNotOverwriteExistingYAML(t *testing.T) {
	beadsDir := t.TempDir()
	// Pre-existing legacy config.yaml should be preserved
	configPath := filepath.Join(beadsDir, "config.yaml")
	original := "prefix: keep\nissue-prefix: keep\n"
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	if err := EnsureConfigYAMLIfMissing(beadsDir, "hq"); err != nil {
		t.Fatalf("EnsureConfigYAMLIfMissing: %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if string(after) != original {
		t.Fatalf("config.yaml changed:\n got: %q\nwant: %q", string(after), original)
	}
}

func TestEnsureConfigYAMLIfMissing_DoesNotOverwriteExistingJSON(t *testing.T) {
	beadsDir := t.TempDir()
	configPath := filepath.Join(beadsDir, "config.json")
	original := `{"prefix":"keep","issue-prefix":"keep"}` + "\n"
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	if err := EnsureConfigYAMLIfMissing(beadsDir, "hq"); err != nil {
		t.Fatalf("EnsureConfigYAMLIfMissing: %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if string(after) != original {
		t.Fatalf("config.json changed:\n got: %q\nwant: %q", string(after), original)
	}
}

func TestEnsureConfig_WritesJSON(t *testing.T) {
	beadsDir := t.TempDir()

	if err := EnsureConfigYAML(beadsDir, "myrig"); err != nil {
		t.Fatalf("EnsureConfigYAML: %v", err)
	}

	// config.json should exist with correct content
	data, err := os.ReadFile(filepath.Join(beadsDir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var cfg beadsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	if cfg.Prefix != "myrig" {
		t.Errorf("prefix = %q, want %q", cfg.Prefix, "myrig")
	}
	if cfg.IssuePrefix != "myrig" {
		t.Errorf("issue-prefix = %q, want %q", cfg.IssuePrefix, "myrig")
	}

	// Legacy config.yaml should also exist for backward compat with bd
	yamlData, err := os.ReadFile(filepath.Join(beadsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if got := string(yamlData); got != "prefix: myrig\nissue-prefix: myrig\n" {
		t.Errorf("config.yaml = %q, want prefix+issue-prefix lines", got)
	}
}

func TestReadConfig_PrefersJSON(t *testing.T) {
	beadsDir := t.TempDir()
	// Write both JSON and YAML with different values
	os.WriteFile(filepath.Join(beadsDir, "config.json"), []byte(`{"prefix":"fromjson","issue-prefix":"fromjson"}`), 0644)
	os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("prefix: fromyaml\nissue-prefix: fromyaml\n"), 0644)

	cfg, found := readConfig(beadsDir)
	if !found {
		t.Fatal("readConfig found no config")
	}
	if cfg.Prefix != "fromjson" {
		t.Errorf("prefix = %q, want %q (should prefer JSON)", cfg.Prefix, "fromjson")
	}
}

func TestReadConfig_FallsBackToYAML(t *testing.T) {
	beadsDir := t.TempDir()
	// Only legacy YAML
	os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("prefix: fromyaml\nissue-prefix: fromyaml\n"), 0644)

	cfg, found := readConfig(beadsDir)
	if !found {
		t.Fatal("readConfig found no config")
	}
	if cfg.Prefix != "fromyaml" {
		t.Errorf("prefix = %q, want %q", cfg.Prefix, "fromyaml")
	}
}

func TestEnsureConfigYAMLFromMetadataIfMissing_UsesMetadataPrefix(t *testing.T) {
	beadsDir := t.TempDir()
	metadata := `{"backend":"dolt","dolt_mode":"server","dolt_database":"hq","issue_prefix":"foo"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	if err := EnsureConfigYAMLFromMetadataIfMissing(beadsDir, "hq"); err != nil {
		t.Fatalf("EnsureConfigYAMLFromMetadataIfMissing: %v", err)
	}

	// Check config.json
	data, err := os.ReadFile(filepath.Join(beadsDir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var cfg beadsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	if cfg.Prefix != "foo" {
		t.Errorf("prefix = %q, want %q", cfg.Prefix, "foo")
	}
}

func TestConfigDefaultsFromMetadata_FallsBackToDoltDatabase(t *testing.T) {
	beadsDir := t.TempDir()
	metadata := `{"backend":"dolt","dolt_mode":"server","dolt_database":"hq-custom"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	prefix := ConfigDefaultsFromMetadata(beadsDir, "hq")
	if prefix != "hq-custom" {
		t.Fatalf("prefix = %q, want %q", prefix, "hq-custom")
	}
}

func TestConfigDefaultsFromMetadata_StripsLegacyBeadsPrefixFromDoltDatabase(t *testing.T) {
	beadsDir := t.TempDir()
	metadata := `{"backend":"dolt","dolt_mode":"server","dolt_database":"beads_hq"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	prefix := ConfigDefaultsFromMetadata(beadsDir, "fallback")
	if prefix != "hq" {
		t.Fatalf("prefix = %q, want %q", prefix, "hq")
	}
}

func TestEnsureConfigYAMLFromMetadataIfMissing_StripsLegacyBeadsPrefixFromDoltDatabase(t *testing.T) {
	beadsDir := t.TempDir()
	metadata := `{"backend":"dolt","dolt_mode":"server","dolt_database":"beads_hq"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	if err := EnsureConfigYAMLFromMetadataIfMissing(beadsDir, "fallback"); err != nil {
		t.Fatalf("EnsureConfigYAMLFromMetadataIfMissing: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var cfg beadsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	if cfg.Prefix != "hq" {
		t.Fatalf("prefix = %q, want %q", cfg.Prefix, "hq")
	}
	if cfg.IssuePrefix != "hq" {
		t.Fatalf("issue-prefix = %q, want %q", cfg.IssuePrefix, "hq")
	}
}
