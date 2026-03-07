package beads

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// beadsConfig represents the JSON config file for a beads directory.
type beadsConfig struct {
	Prefix      string `json:"prefix"`
	IssuePrefix string `json:"issue-prefix"`
}

// EnsureConfigYAML ensures config.json has both prefix keys set for the given
// beads namespace. Existing non-prefix settings are preserved.
// The name is kept for backward compatibility; new config is written as JSON.
func EnsureConfigYAML(beadsDir, prefix string) error {
	return ensureConfig(beadsDir, prefix, false)
}

// EnsureConfigYAMLIfMissing creates config.json with the required defaults when
// no config file (JSON or legacy YAML) exists. Existing files are left untouched.
func EnsureConfigYAMLIfMissing(beadsDir, prefix string) error {
	return ensureConfig(beadsDir, prefix, true)
}

// EnsureConfigYAMLFromMetadataIfMissing creates config.json when missing using
// metadata-derived defaults for prefix when available.
func EnsureConfigYAMLFromMetadataIfMissing(beadsDir, fallbackPrefix string) error {
	prefix := ConfigDefaultsFromMetadata(beadsDir, fallbackPrefix)
	return ensureConfig(beadsDir, prefix, true)
}

// ConfigDefaultsFromMetadata derives config defaults from metadata.json.
// Falls back to fallbackPrefix when fields are absent.
func ConfigDefaultsFromMetadata(beadsDir, fallbackPrefix string) string {
	prefix := strings.TrimSpace(strings.TrimSuffix(fallbackPrefix, "-"))
	if prefix == "" {
		prefix = fallbackPrefix
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		return prefix
	}

	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return prefix
	}

	if derived := firstString(meta, "issue_prefix", "issue-prefix", "prefix"); derived != "" {
		prefix = strings.TrimSpace(strings.TrimSuffix(derived, "-"))
	} else if doltDB := firstString(meta, "dolt_database"); doltDB != "" {
		prefix = normalizeDoltDatabasePrefix(doltDB)
	}

	return prefix
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" {
			return s
		}
	}
	return ""
}

func normalizeDoltDatabasePrefix(dbName string) string {
	name := strings.TrimSpace(strings.TrimSuffix(dbName, "-"))
	if strings.HasPrefix(name, "beads_") {
		trimmed := strings.TrimPrefix(name, "beads_")
		if trimmed != "" {
			return trimmed
		}
	}
	return name
}

// configJSONPath returns the path to config.json in a beads directory.
func configJSONPath(beadsDir string) string {
	return filepath.Join(beadsDir, "config.json")
}

// configYAMLPath returns the path to the legacy config.yaml in a beads directory.
func configYAMLPath(beadsDir string) string {
	return filepath.Join(beadsDir, "config.yaml")
}

// readConfig reads beads config from config.json, falling back to legacy config.yaml.
// Returns the config and whether a file was found.
func readConfig(beadsDir string) (beadsConfig, bool) {
	// Try config.json first
	if data, err := os.ReadFile(configJSONPath(beadsDir)); err == nil {
		var cfg beadsConfig
		if json.Unmarshal(data, &cfg) == nil {
			return cfg, true
		}
	}

	// Fall back to legacy config.yaml (manual parsing — no YAML library)
	if data, err := os.ReadFile(configYAMLPath(beadsDir)); err == nil {
		cfg := parseYAMLConfig(data)
		if cfg.Prefix != "" || cfg.IssuePrefix != "" {
			return cfg, true
		}
	}

	return beadsConfig{}, false
}

// parseYAMLConfig parses the legacy config.yaml format (simple key: value lines).
func parseYAMLConfig(data []byte) beadsConfig {
	var cfg beadsConfig
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = stripYAMLQuotes(val)
		switch key {
		case "prefix":
			cfg.Prefix = val
		case "issue-prefix":
			cfg.IssuePrefix = val
		}
	}
	return cfg
}

// ensureConfig writes config.json with prefix values.
// When onlyIfMissing is true, existing config (JSON or YAML) is preserved.
func ensureConfig(beadsDir, prefix string, onlyIfMissing bool) error {
	jsonPath := configJSONPath(beadsDir)
	yamlPath := configYAMLPath(beadsDir)

	// Check if any config exists
	jsonExists := fileExists(jsonPath)
	yamlExists := fileExists(yamlPath)

	if onlyIfMissing && (jsonExists || yamlExists) {
		return nil
	}

	// Write config.json
	cfg := beadsConfig{
		Prefix:      prefix,
		IssuePrefix: prefix,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		return err
	}

	// Also write legacy config.yaml for backward compatibility with bd
	yamlContent := "prefix: " + prefix + "\n" + "issue-prefix: " + prefix + "\n"
	return os.WriteFile(yamlPath, []byte(yamlContent), 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
