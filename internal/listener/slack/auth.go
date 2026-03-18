package slack

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	goslack "github.com/slack-go/slack"

	"github.com/anish/claude-msg-utils/internal/config"
)

const defaultPort = 9876

// Scopes required by the app (must match manifests/slack-app.yaml).
var botScopes = []string{
	"channels:history",
	"channels:read",
	"groups:history",
	"groups:read",
	"im:history",
	"im:read",
	"mpim:history",
	"mpim:read",
	"users:read",
}

// OnInstall is called when a new workspace is successfully installed via OAuth.
type OnInstall func(entry config.SlackConfig)

// AuthServer runs a localhost HTTPS server that handles the Slack OAuth redirect flow.
// Requires mkcert-generated TLS certificates in the config directory.
type AuthServer struct {
	clientID     string
	clientSecret string
	port         int
	onInstall    OnInstall
	installed    chan config.SlackConfig
}

// NewAuthServer creates an OAuth server. onInstall is optional (used by daemon to start new listeners).
func NewAuthServer(clientID, clientSecret string, onInstall OnInstall) *AuthServer {
	return &AuthServer{
		clientID:     clientID,
		clientSecret: clientSecret,
		port:         defaultPort,
		onInstall:    onInstall,
		installed:    make(chan config.SlackConfig, 1),
	}
}

// CertPath returns the expected path for the TLS certificate.
func CertPath() string {
	return filepath.Join(config.ConfigDir(), "localhost.pem")
}

// KeyPath returns the expected path for the TLS private key.
func KeyPath() string {
	return filepath.Join(config.ConfigDir(), "localhost-key.pem")
}

// HasTLSCerts checks whether the mkcert certificates exist.
func HasTLSCerts() bool {
	_, errCert := os.Stat(CertPath())
	_, errKey := os.Stat(KeyPath())
	return errCert == nil && errKey == nil
}

// InstallURL returns the Slack authorize URL that the user should visit.
func (s *AuthServer) InstallURL() string {
	params := url.Values{
		"client_id":    {s.clientID},
		"scope":        {strings.Join(botScopes, ",")},
		"redirect_uri": {s.redirectURI()},
	}
	return "https://slack.com/oauth/v2/authorize?" + params.Encode()
}

// Installed returns a channel that receives the new SlackConfig after a successful OAuth install.
func (s *AuthServer) Installed() <-chan config.SlackConfig {
	return s.installed
}

// Start starts the HTTPS server. Blocks until ctx is cancelled.
func (s *AuthServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/slack/install", s.handleInstall)
	mux.HandleFunc("/slack/oauth/callback", s.handleCallback)

	cert, err := tls.LoadX509KeyPair(CertPath(), KeyPath())
	if err != nil {
		return fmt.Errorf("load TLS certs: %w\n\nGenerate them with:\n  mkcert -cert-file %s -key-file %s localhost",
			err, CertPath(), KeyPath())
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	slog.InfoContext(ctx, "slack oauth server started (HTTPS)", "port", s.port)
	err = srv.ListenAndServeTLS("", "")
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *AuthServer) handleInstall(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.InstallURL(), http.StatusFound)
}

func (s *AuthServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		errMsg := r.URL.Query().Get("error")
		http.Error(w, fmt.Sprintf("OAuth error: %s", errMsg), http.StatusBadRequest)
		return
	}

	resp, err := goslack.GetOAuthV2ResponseContext(r.Context(), http.DefaultClient,
		s.clientID, s.clientSecret, code, s.redirectURI())
	if err != nil {
		slog.ErrorContext(r.Context(), "slack oauth token exchange failed", "error", err)
		http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
		return
	}

	entry := config.SlackConfig{
		Workspace: resp.Team.Name,
		BotToken:  resp.AccessToken,
		TeamID:    resp.Team.ID,
	}

	// Save to config
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}
	cfg.AddSlack(entry)
	if err := config.Save(cfg); err != nil {
		slog.ErrorContext(r.Context(), "failed to save config after oauth", "error", err)
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "slack workspace installed",
		"workspace", entry.Workspace, "team_id", entry.TeamID)

	// Write response to browser BEFORE signaling, so the server stays alive
	// long enough for the browser to receive the success page.
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><body>
<h2>Workspace "%s" installed successfully!</h2>
<p>You can close this tab and return to the terminal.</p>
</body></html>`, resp.Team.Name)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	if s.onInstall != nil {
		s.onInstall(entry)
	}
	select {
	case s.installed <- entry:
	default:
	}
}

func (s *AuthServer) redirectURI() string {
	return fmt.Sprintf("https://localhost:%d/slack/oauth/callback", s.port)
}
