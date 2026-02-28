package cmd

import "testing"

func TestFirstNonFlagArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bead ID only", []string{"gt-abc123"}, "gt-abc123"},
		{"flags before bead", []string{"--json", "--allow-stale", "gt-abc123"}, "gt-abc123"},
		{"bead then flags", []string{"gt-abc123", "--json"}, "gt-abc123"},
		{"only flags", []string{"--json", "--verbose"}, ""},
		{"empty args", []string{}, ""},
		{"short flag before bead", []string{"-v", "hq-xyz"}, "hq-xyz"},
		{"mixed flags and bead", []string{"--json", "bd-456", "-v"}, "bd-456"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := firstNonFlagArg(tc.args)
			if got != tc.want {
				t.Errorf("firstNonFlagArg(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
