package slack

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/config"
)

const defaultPort = 9876

// Scopes required by the app (must match manifests/slack-app.yaml).
var botScopes = []string{
	"channels:history",
	"channels:read",
	"chat:write",
	"chat:write.public",
	"groups:history",
	"groups:read",
	"im:history",
	"im:read",
	"im:write",
	"mpim:history",
	"mpim:read",
	"mpim:write",
	"users:read",
	"users:read.email",
}

// User scopes grant the app permission to act on behalf of the installing user,
// giving access to their DMs and all conversations they can see.
var userScopes = []string{
	"channels:history",
	"channels:read",
	"chat:write",
	"groups:history",
	"groups:read",
	"im:history",
	"im:read",
	"im:write",
	"mpim:history",
	"mpim:read",
	"mpim:write",
	"users:read",
}

// OnInstall is called when a new workspace is successfully installed via OAuth.
type OnInstall func(entry config.SlackConfig)

// AuthServer runs a localhost HTTP server that handles the Slack OAuth redirect flow.
type AuthServer struct {
	clientID     string
	clientSecret string
	appToken     string
	port         int
	onInstall    OnInstall
	installed    chan config.SlackConfig
}

// NewAuthServer creates an OAuth server. appToken is saved into the resulting SlackConfig
// so the entry has all credentials needed to start a listener.
func NewAuthServer(clientID, clientSecret, appToken string, onInstall OnInstall) *AuthServer {
	return &AuthServer{
		clientID:     clientID,
		clientSecret: clientSecret,
		appToken:     appToken,
		port:         defaultPort,
		onInstall:    onInstall,
		installed:    make(chan config.SlackConfig, 1),
	}
}

// InstallURL returns the Slack authorize URL that the user should visit.
func (s *AuthServer) InstallURL() string {
	params := url.Values{
		"client_id":    {s.clientID},
		"scope":        {strings.Join(botScopes, ",")},
		"user_scope":   {strings.Join(userScopes, ",")},
		"redirect_uri": {s.redirectURI()},
	}
	return "https://slack.com/oauth/v2/authorize?" + params.Encode()
}

// Installed returns a channel that receives the new SlackConfig after a successful OAuth install.
func (s *AuthServer) Installed() <-chan config.SlackConfig {
	return s.installed
}

// Start starts the HTTP server. Blocks until ctx is cancelled.
func (s *AuthServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/slack/install", s.handleInstall)
	mux.HandleFunc("/slack/oauth/callback", s.handleCallback)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	slog.InfoContext(ctx, "slack oauth server started", "port", s.port)
	err := srv.ListenAndServe()
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
		Workspace:    resp.Team.Name,
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		AppToken:     s.appToken,
		BotToken:     resp.AccessToken,
		UserToken:    resp.AuthedUser.AccessToken,
		TeamID:       resp.Team.ID,
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

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><body>
<h2>Workspace "%s" installed successfully!</h2>
<p>You can close this tab and return to the terminal.</p>
</body></html>`, resp.Team.Name)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		if s.onInstall != nil {
			s.onInstall(entry)
		}
		select {
		case s.installed <- entry:
		default:
		}
	}()
}

func (s *AuthServer) redirectURI() string {
	return fmt.Sprintf("http://localhost:%d/slack/oauth/callback", s.port)
}
