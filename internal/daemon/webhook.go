package daemon

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

const (
	webhookDefaultPort     = 8742
	webhookDefaultBindAddr = "127.0.0.1"
	webhookMaxBodyBytes    = 1 << 20 // 1 MB
)

// WebhookServer listens for HTTP webhook events from Forgejo/Gitea or GitHub
// and routes them to the appropriate active agent session via nudge.
type WebhookServer struct {
	cfg      *WebhookConfig
	townRoot string
	log      func(string, ...interface{})
	server   *http.Server

	// nudgeHook is called instead of the real tmux nudge when set (tests only).
	nudgeHook func(rigName, message string)
}

// NewWebhookServer creates a WebhookServer using the provided config.
// Returns nil if the config is nil or disabled.
func NewWebhookServer(cfg *WebhookConfig, townRoot string, log func(string, ...interface{})) *WebhookServer {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	return &WebhookServer{cfg: cfg, townRoot: townRoot, log: log}
}

// Start starts the HTTP listener in a background goroutine.
// It is non-blocking and returns immediately.
func (w *WebhookServer) Start() {
	port := w.cfg.Port
	if port == 0 {
		port = webhookDefaultPort
	}
	addr := w.cfg.BindAddr
	if addr == "" {
		addr = webhookDefaultBindAddr
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/forgejo", w.handleForgejo)
	mux.HandleFunc("/webhook/gitea", w.handleForgejo) // Gitea uses identical format
	mux.HandleFunc("/webhook/github", w.handleGitHub)
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	w.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", addr, port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		w.log("Webhook listener starting on %s", w.server.Addr)
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			w.log("Webhook listener error: %v", err)
		}
	}()
}

// Stop gracefully shuts down the HTTP listener with a 5-second deadline.
func (w *WebhookServer) Stop() {
	if w.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.server.Shutdown(ctx); err != nil {
		w.log("Webhook listener shutdown error: %v", err)
	} else {
		w.log("Webhook listener stopped")
	}
}

// Addr returns the listener address (for tests).
func (w *WebhookServer) Addr() string {
	if w.server == nil {
		return ""
	}
	return w.server.Addr
}

// --- Forgejo/Gitea handler ---

// forgejoPayload captures the fields we need from Forgejo/Gitea webhook payloads.
// Both PR and issue_comment events share the same top-level structure.
type forgejoPayload struct {
	Action     string `json:"action"`
	Repository struct {
		CloneURL string `json:"clone_url"`
		HTMLURL  string `json:"html_url"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"html_url"`
	} `json:"pull_request"`
	Comment *struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Issue *struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	} `json:"issue"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (w *WebhookServer) handleForgejo(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, ok := w.readAndVerify(rw, r, r.Header.Get("X-Gitea-Signature"))
	if !ok {
		return
	}

	eventType := r.Header.Get("X-Gitea-Event")
	if eventType == "" {
		eventType = r.Header.Get("X-Forgejo-Event")
	}

	var payload forgejoPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		w.log("Webhook: failed to parse Forgejo payload: %v", err)
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}

	repoURL := payload.Repository.CloneURL
	if repoURL == "" {
		repoURL = payload.Repository.HTMLURL
	}

	msg := w.formatForgejoMessage(eventType, &payload)
	if msg == "" {
		rw.WriteHeader(http.StatusNoContent)
		return
	}

	w.routeAndNudge(repoURL, msg)
	rw.WriteHeader(http.StatusOK)
}

func (w *WebhookServer) formatForgejoMessage(eventType string, p *forgejoPayload) string {
	switch eventType {
	case "pull_request":
		if p.PullRequest == nil {
			return ""
		}
		switch p.Action {
		case "opened", "reopened", "synchronize":
			return fmt.Sprintf("[webhook] PR #%d %s: %q (%s) by %s",
				p.PullRequest.Number, p.Action, p.PullRequest.Title,
				p.PullRequest.URL, p.Sender.Login)
		case "closed":
			return fmt.Sprintf("[webhook] PR #%d closed by %s",
				p.PullRequest.Number, p.Sender.Login)
		}
	case "issue_comment":
		if p.Comment == nil {
			return ""
		}
		// Only route comments on pull requests, not plain issues.
		if p.Issue != nil && p.Issue.PullRequest == nil {
			return ""
		}
		issueNum := 0
		if p.Issue != nil {
			issueNum = p.Issue.Number
		}
		return fmt.Sprintf("[webhook] PR #%d comment by %s: %s",
			issueNum, p.Comment.User.Login, truncate(p.Comment.Body, 120))
	case "pull_request_review_comment":
		if p.Comment == nil {
			return ""
		}
		prNum := 0
		if p.PullRequest != nil {
			prNum = p.PullRequest.Number
		}
		return fmt.Sprintf("[webhook] PR #%d review comment by %s: %s",
			prNum, p.Comment.User.Login, truncate(p.Comment.Body, 120))
	}
	return ""
}

// --- GitHub handler ---

// githubPayload captures the fields we need from GitHub webhook payloads.
type githubPayload struct {
	Action     string `json:"action"`
	Repository struct {
		CloneURL string `json:"clone_url"`
		HTMLURL  string `json:"html_url"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		HTMLURL string `json:"html_url"`
	} `json:"pull_request"`
	Comment *struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Issue *struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	} `json:"issue"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (w *WebhookServer) handleGitHub(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// GitHub uses "sha256=<hex>" format in X-Hub-Signature-256.
	rawSig := r.Header.Get("X-Hub-Signature-256")
	sig := strings.TrimPrefix(rawSig, "sha256=")
	body, ok := w.readAndVerify(rw, r, sig)
	if !ok {
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")

	var payload githubPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		w.log("Webhook: failed to parse GitHub payload: %v", err)
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}

	repoURL := payload.Repository.CloneURL
	if repoURL == "" {
		repoURL = payload.Repository.HTMLURL
	}

	msg := w.formatGitHubMessage(eventType, &payload)
	if msg == "" {
		rw.WriteHeader(http.StatusNoContent)
		return
	}

	w.routeAndNudge(repoURL, msg)
	rw.WriteHeader(http.StatusOK)
}

func (w *WebhookServer) formatGitHubMessage(eventType string, p *githubPayload) string {
	switch eventType {
	case "pull_request":
		if p.PullRequest == nil {
			return ""
		}
		switch p.Action {
		case "opened", "reopened", "synchronize":
			return fmt.Sprintf("[webhook] PR #%d %s: %q (%s) by %s",
				p.PullRequest.Number, p.Action, p.PullRequest.Title,
				p.PullRequest.HTMLURL, p.Sender.Login)
		case "closed":
			return fmt.Sprintf("[webhook] PR #%d closed by %s",
				p.PullRequest.Number, p.Sender.Login)
		}
	case "issue_comment":
		if p.Comment == nil {
			return ""
		}
		// Only route comments on pull requests, not plain issues.
		if p.Issue != nil && p.Issue.PullRequest == nil {
			return ""
		}
		issueNum := 0
		if p.Issue != nil {
			issueNum = p.Issue.Number
		}
		return fmt.Sprintf("[webhook] PR #%d comment by %s: %s",
			issueNum, p.Comment.User.Login, truncate(p.Comment.Body, 120))
	case "pull_request_review_comment":
		if p.Comment == nil {
			return ""
		}
		prNum := 0
		if p.PullRequest != nil {
			prNum = p.PullRequest.Number
		}
		return fmt.Sprintf("[webhook] PR #%d review comment by %s: %s",
			prNum, p.Comment.User.Login, truncate(p.Comment.Body, 120))
	}
	return ""
}

// --- Shared helpers ---

// readAndVerify reads the request body and verifies the HMAC-SHA256 signature.
// If Secret is empty, signature verification is skipped.
// Returns (body, true) on success; writes an HTTP error and returns (nil, false) on failure.
func (w *WebhookServer) readAndVerify(rw http.ResponseWriter, r *http.Request, sig string) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, webhookMaxBodyBytes))
	if err != nil {
		http.Error(rw, "failed to read body", http.StatusInternalServerError)
		return nil, false
	}

	if w.cfg.Secret != "" {
		if !verifyHMAC(body, sig, w.cfg.Secret) {
			w.log("Webhook: signature mismatch (remote: %s)", r.RemoteAddr)
			http.Error(rw, "forbidden", http.StatusForbidden)
			return nil, false
		}
	}
	return body, true
}

// verifyHMAC checks that sig is the HMAC-SHA256 of body using secret.
func verifyHMAC(body []byte, sig, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// routeAndNudge finds the rig that owns repoURL and nudges its witness session.
func (w *WebhookServer) routeAndNudge(repoURL, message string) {
	rigName := w.rigForURL(repoURL)
	if rigName == "" {
		w.log("Webhook: no rig found for repo URL %s — ignoring event", repoURL)
		return
	}

	// Inject test hook if set (avoids hitting real tmux in unit tests).
	if w.nudgeHook != nil {
		w.nudgeHook(rigName, message)
		return
	}

	prefix := session.PrefixFor(rigName)
	witnessSession := session.WitnessSessionName(prefix)

	t := tmux.NewTmux()
	if err := t.NudgeSession(witnessSession, message); err != nil {
		w.log("Webhook: failed to nudge %s: %v", witnessSession, err)
	} else {
		w.log("Webhook: nudged %s with event for %s", witnessSession, repoURL)
	}
}

// rigForURL scans rigs.json and returns the rig name whose git_url (or push_url)
// matches repoURL. Returns "" if no match is found.
func (w *WebhookServer) rigForURL(repoURL string) string {
	rigsConfigPath := fmt.Sprintf("%s/mayor/rigs.json", w.townRoot)
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		w.log("Webhook: failed to load rigs.json: %v", err)
		return ""
	}
	canonical := normalizeRepoURL(repoURL)
	for name, entry := range rigsConfig.Rigs {
		if normalizeRepoURL(entry.GitURL) == canonical ||
			(entry.PushURL != "" && normalizeRepoURL(entry.PushURL) == canonical) {
			return name
		}
	}
	return ""
}

// normalizeRepoURL strips trailing slashes and the ".git" suffix for comparison.
func normalizeRepoURL(u string) string {
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return strings.ToLower(u)
}

// truncate shortens s to at most n runes, appending "…" if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

