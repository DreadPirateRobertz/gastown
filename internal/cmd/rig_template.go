// ABOUTME: Rig template system for quick project bootstrap.
// ABOUTME: Templates capture reusable rig configurations (AGENTS.md, memories, seed beads)
// ABOUTME: stored in the beads KV store and applied on 'gt rig add --template <name>'.

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

const rigTemplateKeyPrefix = "rig.template."

// RigTemplate is a reusable rig configuration saved in the beads KV store.
type RigTemplate struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Created     string           `json:"created"`
	AgentsMD    string           `json:"agents_md,omitempty"`
	Memories    []TemplateMemory `json:"memories,omitempty"`
	Beads       []TemplateBead   `json:"beads,omitempty"`
}

// TemplateMemory is a memory entry to inject when applying a template.
type TemplateMemory struct {
	Type  string `json:"type"`  // feedback, project, user, reference, general
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TemplateBead is a seed bead to create when applying a template.
type TemplateBead struct {
	Title    string `json:"title"`
	Priority int    `json:"priority"` // 1–5 (default 3)
	Type     string `json:"type"`     // feature, bug, chore, docs (default: feature)
	Body     string `json:"body,omitempty"`
}

// rig template subcommand flags
var (
	rigTemplateScope       string
	rigTemplateSaveDesc    string
	rigTemplateSaveAgents  string
	rigTemplateSaveBeads   []string
	rigTemplateSaveMem     []string
)

var rigTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage rig templates for quick project bootstrap",
	Long: `Manage rig templates — reusable project configurations applied at rig creation.

A template can capture:
  - AGENTS.md content injected at the rig root
  - Memories (feedback, project, user, reference) seeded into the rig's beads store
  - Seed beads (starter stories) created automatically

Templates are stored in the beads KV store and applied via 'gt rig add --template <name>'.

Subcommands:
  list    List all saved templates
  show    Show template details
  save    Save a new template
  apply   Apply a template to an existing rig
  delete  Delete a template

Examples:
  gt rig template save go-lib --desc "Standard Go library rig"
  gt rig template list
  gt rig template show go-lib
  gt rig add myrepo https://github.com/org/repo --template go-lib
  gt rig template apply go-lib myrepo
  gt rig template delete go-lib`,
	RunE: requireSubcommand,
}

var rigTemplateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved rig templates",
	Long: `List all rig templates stored in the beads KV store.

Use --scope to control which templates are shown:
  local  Only templates stored in this rig's beads store (default)
  city   Only town-wide templates visible to all agents
  all    Both local and city templates

Examples:
  gt rig template list
  gt rig template list --scope city
  gt rig template list --scope all`,
	Args: cobra.NoArgs,
	RunE: runRigTemplateList,
}

var rigTemplateShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show rig template details",
	Long: `Show the full details of a saved rig template.

Examples:
  gt rig template show go-lib
  gt rig template show go-lib --scope city`,
	Args: cobra.ExactArgs(1),
	RunE: runRigTemplateShow,
}

var rigTemplateSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save a new rig template",
	Long: `Save a new rig template to the beads KV store.

Templates capture reusable project configurations:
  --desc     Human-readable description of the template
  --agents   Path to an AGENTS.md file to embed (or '-' to read from stdin)
  --bead     Seed bead title (repeatable); format: "Title[:priority[:type]]"
             Priority: 1-5 (default 3), Type: feature|bug|chore|docs (default feature)
  --memory   Seed memory (repeatable); format: "type/key=value"
             Types: feedback, project, user, reference, general
  --scope    Where to store: local (default) or city

Examples:
  gt rig template save go-lib --desc "Standard Go library rig"
  gt rig template save go-lib --agents ./AGENTS.md
  gt rig template save go-lib --bead "Write README:3:docs" --bead "Add CI:2:chore"
  gt rig template save go-lib --memory "feedback/test-style=Use table-driven tests"
  gt rig template save shared-rules --scope city --desc "Town-wide coding standards"`,
	Args: cobra.ExactArgs(1),
	RunE: runRigTemplateSave,
}

var rigTemplateApplyCmd = &cobra.Command{
	Use:   "apply <template-name> <rig>",
	Short: "Apply a template to an existing rig",
	Long: `Apply a saved template to an existing rig.

This writes the template's AGENTS.md (if any) to the rig root and seeds
memories and beads into the rig's beads store.

Use --dry-run to preview what would be applied.

Examples:
  gt rig template apply go-lib myproject
  gt rig template apply go-lib myproject --dry-run
  gt rig template apply shared-rules myproject --scope city`,
	Args: cobra.ExactArgs(2),
	RunE: runRigTemplateApply,
}

var rigTemplateApplyDryRun bool

var rigTemplateDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a saved rig template",
	Long: `Delete a rig template from the beads KV store.

Examples:
  gt rig template delete go-lib
  gt rig template delete shared-rules --scope city`,
	Args: cobra.ExactArgs(1),
	RunE: runRigTemplateDelete,
}

func init() {
	rigCmd.AddCommand(rigTemplateCmd)
	rigTemplateCmd.AddCommand(rigTemplateListCmd)
	rigTemplateCmd.AddCommand(rigTemplateShowCmd)
	rigTemplateCmd.AddCommand(rigTemplateSaveCmd)
	rigTemplateCmd.AddCommand(rigTemplateApplyCmd)
	rigTemplateCmd.AddCommand(rigTemplateDeleteCmd)

	// Shared scope flag
	for _, cmd := range []*cobra.Command{
		rigTemplateListCmd, rigTemplateShowCmd,
		rigTemplateSaveCmd, rigTemplateApplyCmd, rigTemplateDeleteCmd,
	} {
		cmd.Flags().StringVar(&rigTemplateScope, "scope", memoryScopeLocal,
			"Storage scope: local (default) or city")
	}

	// list --scope default is "all" for listing
	rigTemplateListCmd.Flags().Lookup("scope").DefValue = "all"

	// save flags
	rigTemplateSaveCmd.Flags().StringVar(&rigTemplateSaveDesc, "desc", "", "Template description")
	rigTemplateSaveCmd.Flags().StringVar(&rigTemplateSaveAgents, "agents", "", "Path to AGENTS.md file to embed (or '-' for stdin)")
	rigTemplateSaveCmd.Flags().StringArrayVar(&rigTemplateSaveBeads, "bead", nil, "Seed bead: \"Title[:priority[:type]]\" (repeatable)")
	rigTemplateSaveCmd.Flags().StringArrayVar(&rigTemplateSaveMem, "memory", nil, "Seed memory: \"type/key=value\" (repeatable)")

	// apply --dry-run
	rigTemplateApplyCmd.Flags().BoolVar(&rigTemplateApplyDryRun, "dry-run", false, "Preview changes without applying")
}

// --- storage helpers ---

func templateKVKey(name string) string {
	return rigTemplateKeyPrefix + sanitizeKey(name)
}

func loadTemplate(name, scope string) (*RigTemplate, error) {
	key := templateKVKey(name)
	var val string
	var err error

	switch scope {
	case memoryScopeCity:
		cityDB := cityBeadsPath()
		if cityDB == "" {
			return nil, fmt.Errorf("--scope city requires $GT_ROOT or $GT_TOWN_ROOT to be set")
		}
		val, err = bdKvGetDB(cityDB, key)
	default: // local first, then city
		val, err = bdKvGet(key)
		if err != nil || val == "" {
			if cityDB := cityBeadsPath(); cityDB != "" {
				val, err = bdKvGetDB(cityDB, key)
			}
		}
	}
	if err != nil || val == "" {
		return nil, fmt.Errorf("template %q not found", name)
	}

	var tmpl RigTemplate
	if err := json.Unmarshal([]byte(val), &tmpl); err != nil {
		return nil, fmt.Errorf("parsing template %q: %w", name, err)
	}
	return &tmpl, nil
}

func saveTemplate(tmpl *RigTemplate, scope string) error {
	data, err := json.Marshal(tmpl)
	if err != nil {
		return fmt.Errorf("encoding template: %w", err)
	}
	key := templateKVKey(tmpl.Name)
	switch scope {
	case memoryScopeCity:
		cityDB := cityBeadsPath()
		if cityDB == "" {
			return fmt.Errorf("--scope city requires $GT_ROOT or $GT_TOWN_ROOT to be set")
		}
		return bdKvSetDB(cityDB, key, string(data))
	default:
		return bdKvSet(key, string(data))
	}
}

func deleteTemplate(name, scope string) error {
	key := templateKVKey(name)
	switch scope {
	case memoryScopeCity:
		cityDB := cityBeadsPath()
		if cityDB == "" {
			return fmt.Errorf("--scope city requires $GT_ROOT or $GT_TOWN_ROOT to be set")
		}
		return bdKvClearDB(cityDB, key)
	default:
		return bdKvClear(key)
	}
}

// templateEntry holds a template and its storage origin.
type templateEntry struct {
	tmpl   RigTemplate
	isCity bool
}

// collectTemplates gathers templates from local and/or city stores.
func collectTemplates(scope string) ([]templateEntry, error) {
	var results []templateEntry

	collect := func(kvs map[string]string, isCity bool) {
		for k, v := range kvs {
			if !strings.HasPrefix(k, rigTemplateKeyPrefix) {
				continue
			}
			var tmpl RigTemplate
			if err := json.Unmarshal([]byte(v), &tmpl); err != nil {
				continue
			}
			results = append(results, templateEntry{tmpl, isCity})
		}
	}

	if scope == memoryScopeLocal || scope == "all" {
		kvs, err := bdKvListJSON()
		if err != nil {
			return nil, fmt.Errorf("listing local templates: %w", err)
		}
		collect(kvs, false)
	}

	if scope == memoryScopeCity || scope == "all" {
		if cityDB := cityBeadsPath(); cityDB != "" {
			if kvs, err := bdKvListJSONDB(cityDB); err == nil {
				collect(kvs, true)
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].isCity != results[j].isCity {
			return !results[i].isCity
		}
		return results[i].tmpl.Name < results[j].tmpl.Name
	})
	return results, nil
}

// --- command implementations ---

func runRigTemplateList(cmd *cobra.Command, args []string) error {
	scope := strings.ToLower(strings.TrimSpace(rigTemplateScope))
	if scope == "" || scope == memoryScopeLocal {
		scope = "all" // default list shows all
	}

	entries, err := collectTemplates(scope)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No rig templates saved. Use 'gt rig template save <name>' to create one.")
		return nil
	}

	fmt.Printf("%s (%d):\n\n", style.Bold.Render("Rig Templates"), len(entries))
	for _, e := range entries {
		scopeLabel := ""
		if e.isCity {
			scopeLabel = style.Dim.Render(" [city]")
		}
		memCount := len(e.tmpl.Memories)
		beadCount := len(e.tmpl.Beads)
		hasAgents := ""
		if e.tmpl.AgentsMD != "" {
			hasAgents = " agents_md"
		}
		meta := fmt.Sprintf("%d memories, %d beads%s", memCount, beadCount, hasAgents)
		fmt.Printf("  %s%s\n", style.Bold.Render(e.tmpl.Name), scopeLabel)
		if e.tmpl.Description != "" {
			fmt.Printf("    %s\n", e.tmpl.Description)
		}
		fmt.Printf("    %s\n\n", style.Dim.Render(meta))
	}
	return nil
}

func runRigTemplateShow(cmd *cobra.Command, args []string) error {
	scope := strings.ToLower(strings.TrimSpace(rigTemplateScope))

	tmpl, err := loadTemplate(args[0], scope)
	if err != nil {
		return err
	}

	fmt.Printf("%s", style.Bold.Render("Template: "+tmpl.Name))
	if tmpl.Description != "" {
		fmt.Printf(" — %s", tmpl.Description)
	}
	fmt.Println()
	if tmpl.Created != "" {
		fmt.Printf("  Created: %s\n", tmpl.Created)
	}

	if tmpl.AgentsMD != "" {
		fmt.Printf("\n  %s\n", style.Dim.Render("[agents_md]"))
		lines := strings.Split(tmpl.AgentsMD, "\n")
		preview := lines
		if len(preview) > 6 {
			preview = append(lines[:6], fmt.Sprintf("... (%d lines total)", len(lines)))
		}
		for _, l := range preview {
			fmt.Printf("    %s\n", l)
		}
	}

	if len(tmpl.Memories) > 0 {
		fmt.Printf("\n  %s (%d)\n", style.Dim.Render("[memories]"), len(tmpl.Memories))
		for _, m := range tmpl.Memories {
			fmt.Printf("    %s/%s  %s\n", m.Type, style.Bold.Render(m.Key), m.Value)
		}
	}

	if len(tmpl.Beads) > 0 {
		fmt.Printf("\n  %s (%d)\n", style.Dim.Render("[beads]"), len(tmpl.Beads))
		for _, b := range tmpl.Beads {
			t := b.Type
			if t == "" {
				t = "feature"
			}
			p := b.Priority
			if p == 0 {
				p = 3
			}
			fmt.Printf("    P%d [%s] %s\n", p, t, style.Bold.Render(b.Title))
		}
	}
	return nil
}

func runRigTemplateSave(cmd *cobra.Command, args []string) error {
	name := sanitizeKey(args[0])
	scope := strings.ToLower(strings.TrimSpace(rigTemplateScope))

	tmpl := &RigTemplate{
		Name:        name,
		Description: rigTemplateSaveDesc,
		Created:     time.Now().UTC().Format(time.RFC3339),
	}

	// Embed AGENTS.md if provided
	if rigTemplateSaveAgents != "" {
		var content []byte
		var err error
		if rigTemplateSaveAgents == "-" {
			content, err = io.ReadAll(os.Stdin)
		} else {
			content, err = os.ReadFile(rigTemplateSaveAgents)
		}
		if err != nil {
			return fmt.Errorf("reading agents file: %w", err)
		}
		tmpl.AgentsMD = string(content)
	}

	// Parse --memory entries: "type/key=value"
	for _, m := range rigTemplateSaveMem {
		mem, err := parseTemplateMemory(m)
		if err != nil {
			return fmt.Errorf("invalid --memory %q: %w", m, err)
		}
		tmpl.Memories = append(tmpl.Memories, mem)
	}

	// Parse --bead entries: "Title[:priority[:type]]"
	for _, b := range rigTemplateSaveBeads {
		bead, err := parseTemplateBead(b)
		if err != nil {
			return fmt.Errorf("invalid --bead %q: %w", b, err)
		}
		tmpl.Beads = append(tmpl.Beads, bead)
	}

	// Check if updating existing
	existing, _ := loadTemplate(name, scope)
	verb := "Saved"
	if existing != nil {
		verb = "Updated"
	}

	if err := saveTemplate(tmpl, scope); err != nil {
		return fmt.Errorf("saving template: %w", err)
	}

	scopeLabel := ""
	if scope == memoryScopeCity {
		scopeLabel = " [city]"
	}
	fmt.Printf("%s %s template%s: %s\n",
		style.Success.Render("✓"), verb, scopeLabel, style.Bold.Render(name))

	if tmpl.AgentsMD != "" {
		fmt.Printf("  agents_md: %d chars\n", len(tmpl.AgentsMD))
	}
	if len(tmpl.Memories) > 0 {
		fmt.Printf("  memories: %d\n", len(tmpl.Memories))
	}
	if len(tmpl.Beads) > 0 {
		fmt.Printf("  seed beads: %d\n", len(tmpl.Beads))
	}
	return nil
}

func runRigTemplateApply(cmd *cobra.Command, args []string) error {
	templateName := args[0]
	rigName := args[1]
	scope := strings.ToLower(strings.TrimSpace(rigTemplateScope))

	// IDOR guard: if the caller is running inside a rig session, they may only
	// apply templates to their own rig. Without this check any agent could
	// write AGENTS.md and seed memories into a rig it doesn't own.
	if callerRig := os.Getenv("GT_RIG"); callerRig != "" && rigName != callerRig {
		return fmt.Errorf("rig ownership check failed: session rig is %q, cannot apply template to %q", callerRig, rigName)
	}

	tmpl, err := loadTemplate(templateName, scope)
	if err != nil {
		return err
	}

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	rigPath := filepath.Join(townRoot, rigName)
	if _, err := os.Stat(rigPath); os.IsNotExist(err) {
		return fmt.Errorf("rig %q not found at %s", rigName, rigPath)
	}

	if rigTemplateApplyDryRun {
		fmt.Printf("%s Dry-run: applying template %s to rig %s\n",
			style.Dim.Render("→"), style.Bold.Render(templateName), style.Bold.Render(rigName))
	} else {
		fmt.Printf("%s Applying template %s to rig %s\n",
			style.Success.Render("✓"), style.Bold.Render(templateName), style.Bold.Render(rigName))
	}

	return applyTemplate(tmpl, rigPath, rigName, rigTemplateApplyDryRun)
}

func runRigTemplateDelete(cmd *cobra.Command, args []string) error {
	name := sanitizeKey(args[0])
	scope := strings.ToLower(strings.TrimSpace(rigTemplateScope))

	// Verify exists before deleting
	if _, err := loadTemplate(name, scope); err != nil {
		return fmt.Errorf("template %q not found", name)
	}

	if err := deleteTemplate(name, scope); err != nil {
		return fmt.Errorf("deleting template: %w", err)
	}

	fmt.Printf("%s Deleted template: %s\n", style.Success.Render("✓"), style.Bold.Render(name))
	return nil
}

// applyTemplate writes the template's AGENTS.md, seeds memories, and creates seed beads
// in the given rig directory. If dryRun is true, it only prints what would be done.
func applyTemplate(tmpl *RigTemplate, rigPath, rigName string, dryRun bool) error {
	// 1. Write AGENTS.md at rig root (or mayor/rig/ if it exists)
	if tmpl.AgentsMD != "" {
		// Prefer the refinery clone as canonical root for instructions
		targetDir := rigPath
		refineryRig := filepath.Join(rigPath, "refinery", "rig")
		if _, err := os.Stat(refineryRig); err == nil {
			targetDir = refineryRig
		}
		agentsPath := filepath.Join(targetDir, "AGENTS.md")
		if dryRun {
			fmt.Printf("  [dry-run] write AGENTS.md → %s\n", agentsPath)
		} else {
			if err := os.WriteFile(agentsPath, []byte(tmpl.AgentsMD), 0644); err != nil {
				return fmt.Errorf("writing AGENTS.md: %w", err)
			}
			fmt.Printf("  Wrote AGENTS.md → %s\n", agentsPath)
		}
	}

	// Determine the beads workdir for this rig
	beadsWorkDir := rigPath
	mayorRigBeads := filepath.Join(rigPath, "mayor", "rig", ".beads")
	if _, err := os.Stat(mayorRigBeads); err == nil {
		beadsWorkDir = filepath.Join(rigPath, "mayor", "rig")
	}
	_ = beadsWorkDir // used below when bd commands are issued

	// 2. Seed memories into rig's beads store (using bd kv set with beads routing)
	for _, m := range tmpl.Memories {
		fullKey := memoryKeyPrefix + m.Type + "." + m.Key
		if dryRun {
			fmt.Printf("  [dry-run] remember %s/%s = %q\n", m.Type, m.Key, truncate(m.Value, 60))
		} else {
			// Use bdKvSet — memories route to the rig's local beads via GT_RIG env
			// which should be set when working from within the rig.
			// For apply, we use bd --db directly to target the rig's beads.
			beadsDBPath := filepath.Join(rigPath, ".beads")
			if _, err := os.Stat(beadsDBPath); os.IsNotExist(err) {
				// Try mayor/rig/.beads
				beadsDBPath = filepath.Join(rigPath, "mayor", "rig", ".beads")
			}
			if _, err := os.Stat(beadsDBPath); err == nil {
				if err := bdKvSetDB(beadsDBPath, fullKey, m.Value); err != nil {
					fmt.Printf("  %s Could not seed memory %s/%s: %v\n",
						style.Warning.Render("!"), m.Type, m.Key, err)
				} else {
					fmt.Printf("  Seeded memory %s/%s\n", m.Type, m.Key)
				}
			} else {
				fmt.Printf("  %s Skipping memories: .beads not found at %s\n",
					style.Warning.Render("!"), rigPath)
			}
		}
	}

	// 3. Create seed beads
	for _, b := range tmpl.Beads {
		priority := b.Priority
		if priority == 0 {
			priority = 3
		}
		bType := b.Type
		if bType == "" {
			bType = "feature"
		}
		if dryRun {
			fmt.Printf("  [dry-run] bd create P%d [%s] %q\n", priority, bType, b.Title)
		} else {
			// Create bead in rig context using bd create
			if err := createSeedBead(rigPath, b.Title, priority, bType, b.Body); err != nil {
				fmt.Printf("  %s Could not create seed bead %q: %v\n",
					style.Warning.Render("!"), b.Title, err)
			} else {
				fmt.Printf("  Created seed bead [P%d]: %s\n", priority, b.Title)
			}
		}
	}

	if dryRun {
		fmt.Printf("\n  (dry-run complete — no changes made)\n")
	}
	return nil
}

// ApplyTemplateToRig applies a named template to a rig during rig creation.
// Called from runRigAdd after the rig directory is set up.
// Silently skips if template not found (add --template is best-effort).
func ApplyTemplateToRig(templateName, rigPath, rigName string) error {
	tmpl, err := loadTemplate(templateName, memoryScopeLocal)
	if err != nil {
		return err
	}
	return applyTemplate(tmpl, rigPath, rigName, false)
}

// --- parsing helpers ---

// parseTemplateMemory parses "type/key=value" format.
func parseTemplateMemory(s string) (TemplateMemory, error) {
	eqIdx := strings.Index(s, "=")
	if eqIdx < 0 {
		return TemplateMemory{}, fmt.Errorf("expected format type/key=value")
	}
	keyPart := s[:eqIdx]
	value := s[eqIdx+1:]

	slashIdx := strings.Index(keyPart, "/")
	if slashIdx < 0 {
		return TemplateMemory{}, fmt.Errorf("expected format type/key=value (missing '/')")
	}
	memType := strings.ToLower(keyPart[:slashIdx])
	key := keyPart[slashIdx+1:]

	if _, ok := validMemoryTypes[memType]; !ok {
		return TemplateMemory{}, fmt.Errorf("unknown memory type %q", memType)
	}
	return TemplateMemory{Type: memType, Key: sanitizeKey(key), Value: value}, nil
}

// parseTemplateBead parses "Title[:priority[:type]]" format.
func parseTemplateBead(s string) (TemplateBead, error) {
	parts := strings.SplitN(s, ":", 3)
	b := TemplateBead{Title: parts[0], Priority: 3, Type: "feature"}
	if len(parts) >= 2 && parts[1] != "" {
		var p int
		if _, err := fmt.Sscanf(parts[1], "%d", &p); err != nil || p < 1 || p > 5 {
			return TemplateBead{}, fmt.Errorf("priority must be 1–5 (got %q)", parts[1])
		}
		b.Priority = p
	}
	if len(parts) >= 3 && parts[2] != "" {
		b.Type = strings.ToLower(parts[2])
	}
	return b, nil
}

// createSeedBead creates a bead in the rig using the bd CLI.
func createSeedBead(rigPath, title string, priority int, bType, body string) error {
	// Build bd create command
	args := []string{"create", "--title", title, "--priority", fmt.Sprintf("%d", priority)}
	if body != "" {
		args = append(args, "--body", body)
	}
	// Try to find beads dir for the rig
	beadsDB := filepath.Join(rigPath, ".beads")
	if _, err := os.Stat(beadsDB); os.IsNotExist(err) {
		beadsDB = filepath.Join(rigPath, "mayor", "rig", ".beads")
	}
	if _, err := os.Stat(beadsDB); err == nil {
		args = append([]string{"--db", beadsDB}, args...)
	}

	cmd := exec.Command("bd", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
