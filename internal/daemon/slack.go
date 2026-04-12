package daemon

import (
	"context"
	"fmt"
	"log/slog"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/lifecycle"
	slacklistener "github.com/anish749/pigeon/internal/listener/slack"
	"github.com/anish749/pigeon/internal/store"
)

// slackFactory builds a lifecycle.Listener for one Slack workspace from a
// config entry plus the shared dependencies the listener needs.
type slackFactory struct {
	cfg       config.SlackConfig
	store     store.Store
	apiServer *api.Server
	identity  *identity.Service
	onMessage hub.MessageNotifyFunc
}

// Key satisfies lifecycle.Factory. The team_id is the stable identifier
// for a workspace across config reloads.
func (f *slackFactory) Key() lifecycle.Key {
	return lifecycle.Key{Kind: "slack", ID: f.cfg.TeamID}
}

// Build validates the config entry and returns a fresh slackListener.
// Token validation happens here so a misconfigured workspace fails fast
// on every restart attempt instead of silently staying dead.
func (f *slackFactory) Build(_ context.Context) (lifecycle.Listener, error) {
	if f.cfg.AppToken == "" || f.cfg.BotToken == "" || f.cfg.UserToken == "" {
		return nil, fmt.Errorf("slack workspace %s missing required tokens (app=%t bot=%t user=%t)",
			f.cfg.Workspace,
			f.cfg.AppToken != "", f.cfg.BotToken != "", f.cfg.UserToken != "")
	}
	return &slackListener{
		cfg:       f.cfg,
		store:     f.store,
		apiServer: f.apiServer,
		identity:  f.identity,
		onMessage: f.onMessage,
	}, nil
}

// slackListener owns the full lifecycle of one Slack workspace's Socket
// Mode connection. It is built fresh on every (re)start by slackFactory;
// all per-run state lives here so nothing leaks between incarnations.
type slackListener struct {
	cfg       config.SlackConfig
	store     store.Store
	apiServer *api.Server
	identity  *identity.Service
	onMessage hub.MessageNotifyFunc
}

// Run drives a single Slack listener until ctx is cancelled or one of its
// underlying goroutines (Socket Mode dispatcher, event handler) exits.
// The sender is registered with the API server for the duration of the
// run and unregistered on exit so stale senders never leak.
func (l *slackListener) Run(ctx context.Context) error {
	acct := account.New("slack", l.cfg.Workspace)

	botAPI := goslack.New(l.cfg.BotToken, goslack.OptionAppLevelToken(l.cfg.AppToken))
	smClient := socketmode.New(botAPI)
	userAPI := goslack.New(l.cfg.UserToken)

	resolver := slacklistener.NewResolver(userAPI, l.identity, l.cfg.Workspace)
	users, channels, err := resolver.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names", "account", acct, "error", err)
	}

	var userName, userID string
	if authResp, err := userAPI.AuthTestContext(ctx); err == nil {
		userID = authResp.UserID
		name, err := resolver.UserName(ctx, userID)
		if err != nil {
			slog.WarnContext(ctx, "failed to resolve Slack user name", "account", acct, "user_id", userID, "error", err)
		} else {
			userName = name
		}
	} else {
		slog.WarnContext(ctx, "failed to get Slack auth info", "account", acct, "error", err)
	}

	var botName, botUserID string
	if authResp, err := botAPI.AuthTestContext(ctx); err == nil {
		botName = authResp.User
		botUserID = authResp.UserID
	} else {
		slog.WarnContext(ctx, "failed to get bot auth info", "account", acct, "error", err)
	}

	messages := slacklistener.NewMessageStore(acct, l.store)
	eventLoop := slacklistener.NewListener(smClient, resolver, messages,
		l.cfg.UserToken, l.cfg.BotToken, acct, l.cfg.TeamID, botUserID, l.onMessage)

	// Register for the lifetime of this incarnation. On exit (crash or
	// clean shutdown) the sender is removed so callers never hit a stale
	// one during the restart window.
	l.apiServer.RegisterSlack(&api.SlackSender{
		BotAPI:    botAPI,
		UserAPI:   userAPI,
		Resolver:  resolver,
		Messages:  messages,
		Acct:      acct,
		BotName:   botName,
		BotUserID: botUserID,
		UserName:  userName,
		UserID:    userID,
	})
	defer l.apiServer.UnregisterSlack(acct.NameSlug())

	slog.InfoContext(ctx, "slack listener started",
		"account", acct, "users", users, "channels", channels)

	// Two goroutines block on the same ctx: if either exits, cancel the
	// sibling and return the first error. The supervisor above decides
	// whether to restart.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	socketErr := make(chan error, 1)
	go func() {
		socketErr <- smClient.RunContext(runCtx)
	}()

	eventErr := make(chan error, 1)
	go func() {
		// slacklistener.Listener.Run does not return an error today — it
		// blocks until runCtx is done or the events channel closes.
		// We translate exits into a sentinel so the supervisor treats
		// early termination of the event loop as a crash.
		eventLoop.Run(runCtx)
		if runCtx.Err() != nil {
			eventErr <- nil
		} else {
			eventErr <- fmt.Errorf("slack event loop exited unexpectedly")
		}
	}()

	select {
	case err := <-socketErr:
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("slack socket mode: %w", err)
		}
		return err
	case err := <-eventErr:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// slackFactories returns one Factory per configured Slack workspace.
func slackFactories(cfgs []config.SlackConfig, s store.Store, apiServer *api.Server, id *identity.Service, onMessage hub.MessageNotifyFunc) []lifecycle.Factory {
	out := make([]lifecycle.Factory, 0, len(cfgs))
	for _, sl := range cfgs {
		if sl.TeamID == "" {
			slog.Error("slack workspace missing team_id, skipping", "workspace", sl.Workspace)
			continue
		}
		out = append(out, &slackFactory{
			cfg:       sl,
			store:     s,
			apiServer: apiServer,
			identity:  id,
			onMessage: onMessage,
		})
	}
	return out
}
