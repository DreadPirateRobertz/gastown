package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRigTemplateApply_IDOROwnershipCheck verifies that applying a template to a
// rig requires the caller to own that rig (GT_RIG must match the target rig name).
func TestRigTemplateApply_IDOROwnershipCheck(t *testing.T) {
	// Save and restore GT_RIG
	origGTRig := os.Getenv("GT_RIG")
	defer func() {
		if origGTRig == "" {
			os.Unsetenv("GT_RIG")
		} else {
			os.Setenv("GT_RIG", origGTRig)
		}
	}()

	t.Run("caller cannot apply template to a rig they don't own", func(t *testing.T) {
		os.Setenv("GT_RIG", "gastown")

		// Simulate: caller is in gastown, tries to apply to cfutons
		err := runRigTemplateApply(nil, []string{"mytemplate", "cfutons"})

		if err == nil {
			t.Fatal("expected error when applying template to foreign rig, got nil")
		}
		if want := "rig ownership check failed"; !contains(err.Error(), want) {
			t.Errorf("expected error containing %q, got %q", want, err.Error())
		}
	})

	t.Run("caller can apply template to their own rig", func(t *testing.T) {
		os.Setenv("GT_RIG", "gastown")

		// Build a minimal town root with a gastown directory so the rig-exists
		// check passes, and a matching template in the kv store.
		tmpDir := t.TempDir()
		rigDir := filepath.Join(tmpDir, "gastown")
		if err := os.MkdirAll(rigDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Without a real KV store the loadTemplate call will fail after the
		// ownership check passes — that's fine; we only need to confirm the
		// ownership check itself does NOT reject the caller.
		origCwd, _ := os.Getwd()
		defer os.Chdir(origCwd)
		os.Chdir(tmpDir)

		err := runRigTemplateApply(nil, []string{"mytemplate", "gastown"})

		// The call will fail (no template in kv store), but it must NOT fail
		// with an ownership error.
		if err != nil && contains(err.Error(), "rig ownership check failed") {
			t.Errorf("ownership check must not block caller's own rig: %v", err)
		}
	})

	t.Run("no GT_RIG set (mayor context) allows any rig", func(t *testing.T) {
		os.Unsetenv("GT_RIG")

		// Without GT_RIG the ownership guard is skipped. The call will still
		// fail (no workspace / template), but NOT with an ownership error.
		err := runRigTemplateApply(nil, []string{"mytemplate", "any-rig"})

		if err != nil && contains(err.Error(), "rig ownership check failed") {
			t.Errorf("ownership check must not fire when GT_RIG is unset: %v", err)
		}
	})
}
