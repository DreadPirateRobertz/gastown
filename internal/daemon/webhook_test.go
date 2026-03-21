package daemon

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- verifyHMAC ---

func TestVerifyHMAC(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := "my-secret"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := hex.EncodeToString(mac.Sum(nil))

	if !verifyHMAC(body, validSig, secret) {
		t.Error("valid signature should verify")
	}
	if verifyHMAC(body, "badsig", secret) {
		t.Error("invalid signature should not verify")
	}
	if verifyHMAC(body, "", secret) {
		t.Error("empty signature should not verify")
	}
}

// --- normalizeRepoURL ---

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"https://github.com/owner/repo.git/", "https://github.com/owner/repo"},
		{"https://github.com/owner/repo", "https://github.com/owner/repo"},
		{"HTTPS://GITHUB.COM/Owner/Repo.git", "https://github.com/owner/repo"},
	}
	for _, tt := range tests {
		if got := normalizeRepoURL(tt.in); got != tt.want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- truncate ---

func TestTruncate(t *testing.T) {
	if got := truncate("hello"); got != "hello" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("x", 130)
	got := truncate(long)
	if len([]rune(got)) > 121 { // 120 runes + ellipsis
		t.Errorf("truncated string too long: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated %q doesn't end with ellipsis", got)
	}
}

// --- newTestWebhookServer: build a WebhookServer without starting it ---

func newTestWebhookServer(secret string) *WebhookServer {
	return &WebhookServer{
		cfg: &WebhookConfig{
			Enabled:  true,
			Port:     0,
			Secret:   secret,
			BindAddr: "127.0.0.1",
		},
		townRoot: "",
		log:      func(string, ...interface{}) {},
	}
}

// signBody returns the HMAC-SHA256 hex signature for body.
func signBody(t *testing.T, body []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// --- Forgejo handler ---

func TestHandleForgejo_PROpened(t *testing.T) {
	w := newTestWebhookServer("s3cr3t")

	// Override routeAndNudge to capture calls without hitting tmux.
	var nudged []string
	w.nudgeHook = func(rig, msg string) { nudged = append(nudged, rig+":"+msg) }

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"clone_url": "https://github.com/owner/repo.git",
		},
		"pull_request": map[string]interface{}{
			"number":   42,
			"title":    "Add feature",
			"html_url": "https://github.com/owner/repo/pull/42",
		},
		"sender": map[string]interface{}{"login": "alice"},
	}
	body, _ := json.Marshal(payload)
	sig := signBody(t, body, "s3cr3t")

	req := httptest.NewRequest(http.MethodPost, "/webhook/forgejo", bytes.NewReader(body))
	req.Header.Set("X-Gitea-Event", "pull_request")
	req.Header.Set("X-Gitea-Signature", sig)
	rec := httptest.NewRecorder()

	w.handleForgejo(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if len(nudged) != 1 || !strings.Contains(nudged[0], "PR #42 opened") {
		t.Errorf("unexpected nudges: %v", nudged)
	}
}

func TestHandleForgejo_SignatureMismatch(t *testing.T) {
	w := newTestWebhookServer("s3cr3t")
	w.nudgeHook = func(string, string) {}

	body := []byte(`{"action":"opened","repository":{"clone_url":"x"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/forgejo", bytes.NewReader(body))
	req.Header.Set("X-Gitea-Event", "pull_request")
	req.Header.Set("X-Gitea-Signature", "badsig")
	rec := httptest.NewRecorder()

	w.handleForgejo(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestHandleForgejo_NoSecret_SkipsVerification(t *testing.T) {
	w := newTestWebhookServer("") // no secret
	var nudged []string
	w.nudgeHook = func(rig, msg string) { nudged = append(nudged, msg) }

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"clone_url": "https://github.com/owner/repo.git",
		},
		"pull_request": map[string]interface{}{
			"number": 1, "title": "t", "html_url": "u",
		},
		"sender": map[string]interface{}{"login": "bob"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook/forgejo", bytes.NewReader(body))
	req.Header.Set("X-Gitea-Event", "pull_request")
	// deliberately no signature header
	rec := httptest.NewRecorder()

	w.handleForgejo(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no secret = no verification)", rec.Code)
	}
}

func TestHandleForgejo_IssueComment_PlainIssue_Ignored(t *testing.T) {
	w := newTestWebhookServer("")
	var nudged []string
	w.nudgeHook = func(rig, msg string) { nudged = append(nudged, msg) }

	// Issue comment on a plain issue (no pull_request field) — should be ignored.
	payload := map[string]interface{}{
		"action":     "created",
		"repository": map[string]interface{}{"clone_url": "https://x.com/r.git"},
		"issue":      map[string]interface{}{"number": 5, "title": "bug"}, // no pull_request subfield
		"comment":    map[string]interface{}{"body": "hi", "user": map[string]interface{}{"login": "u"}},
		"sender":     map[string]interface{}{"login": "u"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook/forgejo", bytes.NewReader(body))
	req.Header.Set("X-Gitea-Event", "issue_comment")
	rec := httptest.NewRecorder()

	w.handleForgejo(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if len(nudged) != 0 {
		t.Errorf("plain issue comment should not generate a nudge, got %v", nudged)
	}
}

func TestHandleForgejo_MethodNotAllowed(t *testing.T) {
	w := newTestWebhookServer("")
	req := httptest.NewRequest(http.MethodGet, "/webhook/forgejo", nil)
	rec := httptest.NewRecorder()
	w.handleForgejo(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// --- GitHub handler ---

func TestHandleGitHub_PROpened(t *testing.T) {
	w := newTestWebhookServer("gh-secret")
	var nudged []string
	w.nudgeHook = func(rig, msg string) { nudged = append(nudged, msg) }

	payload := map[string]interface{}{
		"action": "opened",
		"repository": map[string]interface{}{
			"clone_url": "https://github.com/org/project.git",
		},
		"pull_request": map[string]interface{}{
			"number":   7,
			"title":    "Fix thing",
			"html_url": "https://github.com/org/project/pull/7",
		},
		"sender": map[string]interface{}{"login": "dev"},
	}
	body, _ := json.Marshal(payload)
	sig := "sha256=" + signBody(t, body, "gh-secret")

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	w.handleGitHub(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if len(nudged) != 1 || !strings.Contains(nudged[0], "PR #7 opened") {
		t.Errorf("unexpected nudges: %v", nudged)
	}
}

// --- rigForURL ---

func TestRigForURL(t *testing.T) {
	// Write a minimal rigs.json with two rigs.
	townRoot := t.TempDir()
	mayorDir := filepath.Join(townRoot, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatal(err)
	}
	rigsJSON := `{
		"version": 1,
		"rigs": {
			"myrig": {"git_url": "https://github.com/owner/myrepo.git", "added_at": "2024-01-01T00:00:00Z"},
			"otherrig": {"git_url": "https://github.com/owner/other.git", "push_url": "https://github.com/fork/other.git", "added_at": "2024-01-01T00:00:00Z"}
		}
	}`
	if err := os.WriteFile(filepath.Join(mayorDir, "rigs.json"), []byte(rigsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	w := &WebhookServer{
		cfg:      &WebhookConfig{},
		townRoot: townRoot,
		log:      func(string, ...interface{}) {},
	}

	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/owner/myrepo.git", "myrig"},
		{"https://github.com/owner/myrepo", "myrig"},    // no .git suffix
		{"HTTPS://GITHUB.COM/OWNER/MYREPO.GIT", "myrig"}, // case insensitive
		{"https://github.com/fork/other.git", "otherrig"}, // push_url match
		{"https://github.com/owner/unknown.git", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := w.rigForURL(tt.url)
		if got != tt.want {
			t.Errorf("rigForURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// --- Start/Stop lifecycle (integration) ---

func TestWebhookServer_StartStop(t *testing.T) {
	cfg := &WebhookConfig{
		Enabled:  true,
		Port:     0, // We'll override with a free port below.
		BindAddr: "127.0.0.1",
	}

	// Pick a free port.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cfg.Port = port

	w := &WebhookServer{
		cfg:      cfg,
		townRoot: t.TempDir(),
		log:      func(f string, a ...interface{}) { t.Logf(f, a...) },
	}
	w.Start()
	defer w.Stop()

	// Give the goroutine a moment to bind.
	time.Sleep(50 * time.Millisecond)

	// Health endpoint should respond.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}
}

func TestNewWebhookServer_NilWhenDisabled(t *testing.T) {
	if NewWebhookServer(nil, "", nil) != nil {
		t.Error("nil config should return nil server")
	}
	if NewWebhookServer(&WebhookConfig{Enabled: false}, "", nil) != nil {
		t.Error("disabled config should return nil server")
	}
}
