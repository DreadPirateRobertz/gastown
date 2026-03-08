package cmd

import "testing"

func TestBuildPRBranchName(t *testing.T) {
	tests := []struct {
		name           string
		polecatBranch  string
		issueID        string
		expectedBranch string
	}{
		{
			name:           "with issue ID",
			polecatBranch:  "polecat/goose/gt-abc@mmgwdeq3",
			issueID:        "gt-abc",
			expectedBranch: "gt/gt-abc",
		},
		{
			name:           "without issue ID, with timestamp",
			polecatBranch:  "polecat/goose/gt-xyz@12345",
			issueID:        "",
			expectedBranch: "gt/gt-xyz",
		},
		{
			name:           "without issue ID, no timestamp",
			polecatBranch:  "polecat/nux/gt-def",
			issueID:        "",
			expectedBranch: "gt/gt-def",
		},
		{
			name:           "different prefix",
			polecatBranch:  "polecat/toast/gp-123",
			issueID:        "gp-123",
			expectedBranch: "gt/gp-123",
		},
		{
			name:           "non-polecat branch",
			polecatBranch:  "feature/my-branch",
			issueID:        "gt-test",
			expectedBranch: "gt/gt-test",
		},
		{
			name:           "non-polecat branch without issue ID",
			polecatBranch:  "feature/my-branch",
			issueID:        "",
			expectedBranch: "gt/my-branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildPRBranchName(tt.polecatBranch, tt.issueID)
			if result != tt.expectedBranch {
				t.Errorf("buildPRBranchName(%q, %q) = %q, want %q",
					tt.polecatBranch, tt.issueID, result, tt.expectedBranch)
			}
		})
	}
}
