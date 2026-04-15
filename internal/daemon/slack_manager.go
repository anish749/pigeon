package daemon

import (
	"context"
	"fmt"
	"log/slog"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"golang.org/x/sync/errgroup"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	slacklistener "github.com/anish749/pigeon/internal/listener/slack"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// SlackManager owns the lifecycle of all Slack workspace listeners.
// It starts initial workspaces, watches for config changes, and
// starts/stops workspaces as they are added or removed.
type SlackManager struct {
	apiServer   *api.Server
	onMessage   hub.MessageNotifyFunc
	onReaction  hub.ReactionNotifyFunc
	store       store.Store
	idStore     identity.Store
	dataRoot    paths.DataRoot
	syncTracker *syncstatus.Tracker
	running     map[string]*runningWorkspace // teamID → workspace
}

type runningWorkspace struct {
	cancel context.CancelFunc
}

// NewSlackManager creates a manager that registers Slack senders with the
// given API server. onMessage is called when a routable message arrives
// (DMs, MPDMs, private channels, bot mentions). onReaction is called when
// a reaction or unreaction event arrives. Both must be non-nil.
//
// Each workspace gets its own identity.Writer scoped to
// slack/<workspace>/identity/people.jsonl.
func NewSlackManager(apiServer *api.Server, s store.Store, onMessage hub.MessageNotifyFunc, onReaction hub.ReactionNotifyFunc, idStore identity.Store, dataRoot paths.DataRoot, syncTracker *syncstatus.Tracker) *SlackManager {
	return &SlackManager{
		apiServer:   apiServer,
		onMessage:   onMessage,
		onReaction:  onReaction,
		store:       s,
		idStore:     idStore,
		dataRoot:    dataRoot,
		syncTracker: syncTracker,
		running:     make(map[string]*runningWorkspace),
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
	m.running[sl.TeamID] = &runningWorkspace{cancel: cancel}

	go runWithRestart(wsCtx, "slack/"+sl.Workspace, func(ctx context.Context) error {
		return m.runSlackWorkspace(ctx, sl)
	})
}

// runSlackWorkspace creates a Socket Mode connection, resolver, listener, and
// sync for a single workspace. It blocks until the connection or listener exits.
func (m *SlackManager) runSlackWorkspace(ctx context.Context, sl config.SlackConfig) error {
	acct := account.New("slack", sl.Workspace)

	botAPI := goslack.New(sl.BotToken, goslack.OptionAppLevelToken(sl.AppToken))
	smClient := socketmode.New(botAPI)

	userAPI := goslack.New(sl.UserToken)
	writer := identity.NewWriter(m.idStore, m.dataRoot.AccountFor(account.New("slack", sl.Workspace)).Identity())
	resolver := slacklistener.NewResolver(userAPI, writer, sl.Workspace)
	users, channels, err := resolver.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names", "account", acct, "error", err)
	}

	var userName string
	var userID string
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

	messages := slacklistener.NewMessageStore(acct, m.store)
	listener := slacklistener.NewListener(smClient, resolver, messages, sl.UserToken, sl.BotToken, acct, sl.TeamID, botUserID, m.onMessage, m.onReaction, m.syncTracker)

	m.apiServer.RegisterSlack(&api.SlackSender{
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

	slog.InfoContext(ctx, "slack listener started", "account", acct, "users", users, "channels", channels)

	// Run listener and socket mode client concurrently. If either exits,
	// the errgroup cancels the other.
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		listener.Run(gCtx)
		return nil
	})
	g.Go(func() error {
		if err := smClient.RunContext(gCtx); err != nil {
			return fmt.Errorf("slack socket mode: %w", err)
		}
		return nil
	})
	return g.Wait()
}
