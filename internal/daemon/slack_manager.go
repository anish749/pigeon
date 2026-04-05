package daemon

import (
	"context"
	"log/slog"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub"
	slacklistener "github.com/anish749/pigeon/internal/listener/slack"
	"github.com/anish749/pigeon/internal/store"
)

// SlackManager owns the lifecycle of all Slack workspace listeners.
// It starts initial workspaces, watches for config changes, and
// starts/stops workspaces as they are added or removed.
type SlackManager struct {
	apiServer *api.Server
	onMessage hub.MessageNotifyFunc
	store     store.Store
	running   map[string]*runningWorkspace // teamID → workspace
}

type runningWorkspace struct {
	cancel context.CancelFunc
}

// NewSlackManager creates a manager that registers Slack senders with the
// given API server. onMessage is called when a routable message arrives
// (DMs, MPDMs, private channels, bot mentions). May be nil.
func NewSlackManager(apiServer *api.Server, s store.Store, onMessage hub.MessageNotifyFunc) *SlackManager {
	return &SlackManager{
		apiServer: apiServer,
		onMessage: onMessage,
		store:     s,
		running:   make(map[string]*runningWorkspace),
	}
}

// Run starts listeners for the initial config, then watches for changes.
// Blocks until ctx is cancelled.
func (m *SlackManager) Run(ctx context.Context, initial []config.SlackConfig) {
	for _, sl := range initial {
		m.startWorkspace(ctx, sl)
	}

	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Slack)
	}
}

// Count returns the number of running workspaces.
func (m *SlackManager) Count() int {
	return len(m.running)
}

// reconcile diffs the desired config against running workspaces,
// starting new ones and stopping removed ones.
func (m *SlackManager) reconcile(ctx context.Context, desired []config.SlackConfig) {
	desiredIDs := make(map[string]config.SlackConfig)
	for _, sl := range desired {
		desiredIDs[sl.TeamID] = sl
	}

	// Stop workspaces that were removed from config.
	for teamID, ws := range m.running {
		if _, ok := desiredIDs[teamID]; !ok {
			slog.InfoContext(ctx, "slack workspace removed from config, stopping", "team_id", teamID)
			ws.cancel()
			delete(m.running, teamID)
		}
	}

	// Start workspaces that are new in config.
	for _, sl := range desired {
		if _, ok := m.running[sl.TeamID]; ok {
			continue
		}
		m.startWorkspace(ctx, sl)
	}
}

func (m *SlackManager) startWorkspace(ctx context.Context, sl config.SlackConfig) {
	if sl.AppToken == "" || sl.BotToken == "" || sl.UserToken == "" {
		slog.ErrorContext(ctx, "slack workspace missing required token(s), skipping",
			"workspace", sl.Workspace,
			"has_app_token", sl.AppToken != "",
			"has_bot_token", sl.BotToken != "",
			"has_user_token", sl.UserToken != "")
		return
	}

	wsCtx, cancel := context.WithCancel(ctx)

	sender := startSlackListener(wsCtx, sl, m.store, m.onMessage)
	if sender == nil {
		cancel()
		return
	}

	m.apiServer.RegisterSlack(sender)
	m.running[sl.TeamID] = &runningWorkspace{cancel: cancel}
}

// startSlackListener creates an independent Socket Mode connection, resolver,
// listener, and sync for a single workspace.
func startSlackListener(ctx context.Context, sl config.SlackConfig, s store.Store, onMessage hub.MessageNotifyFunc) *api.SlackSender {
	acct := account.New("slack", sl.Workspace)

	botAPI := goslack.New(sl.BotToken, goslack.OptionAppLevelToken(sl.AppToken))
	smClient := socketmode.New(botAPI)

	userAPI := goslack.New(sl.UserToken)
	resolver := slacklistener.NewResolver(userAPI)
	users, channels, err := resolver.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names", "account", acct, "error", err)
	}

	var userName string
	if authResp, err := userAPI.AuthTestContext(ctx); err == nil {
		userName = resolver.UserName(ctx, authResp.UserID)
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

	messages := slacklistener.NewMessageStore(acct, s)
	listener := slacklistener.NewListener(smClient, resolver, messages, sl.UserToken, sl.BotToken, acct, sl.TeamID, botUserID, onMessage)
	go listener.Run(ctx)

	go func() {
		if err := smClient.RunContext(ctx); err != nil {
			slog.ErrorContext(ctx, "slack socket mode error", "account", acct, "error", err)
		}
	}()

	slog.InfoContext(ctx, "slack listener started", "account", acct, "users", users, "channels", channels)

	return &api.SlackSender{
		BotAPI:   botAPI,
		UserAPI:  userAPI,
		Resolver: resolver,
		Messages: messages,
		Acct:     acct,
		BotName:  botName,
		UserName: userName,
	}
}
