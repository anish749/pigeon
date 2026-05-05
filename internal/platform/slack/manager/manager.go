package manager

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
	"github.com/anish749/pigeon/internal/daemon"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	slacklistener "github.com/anish749/pigeon/internal/platform/slack"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// Manager owns the lifecycle of all Slack workspace listeners.
// It starts initial workspaces, watches for config changes, and
// starts/stops workspaces as they are added or removed.
type Manager struct {
	apiServer       *api.Server
	onEvent         hub.NotifyFunc
	store           *store.FSStore
	idStore         identity.Store
	dataRoot        paths.DataRoot
	syncTracker     *syncstatus.Tracker
	triggerMaintain func(context.Context, account.Account)
	running         map[string]*runningWorkspace // teamID → workspace
}

type runningWorkspace struct {
	cancel context.CancelFunc
}

// NewManager creates a manager that registers Slack senders with the
// given API server. onEvent is the single hub callback used for every
// routable platform event — messages, reactions, edits, deletes — built
// at the listener call sites via hub.NewMsg / NewReact / NewEdit / NewDelete.
// Must be non-nil.
//
// triggerMaintain is invoked by each workspace's syncer after a successful
// sync to compact the freshly written files. Routing through this hook
// (instead of calling FSStore.Maintain directly) keeps eager post-sync
// compaction and the periodic scheduler serialised on the daemon's
// single maintenance worker. Required non-nil.
//
// Each workspace gets its own identity.Writer scoped to
// slack/<workspace>/identity/people.jsonl.
func NewManager(apiServer *api.Server, s *store.FSStore, onEvent hub.NotifyFunc, idStore identity.Store, dataRoot paths.DataRoot, syncTracker *syncstatus.Tracker, triggerMaintain func(context.Context, account.Account)) *Manager {
	return &Manager{
		apiServer:       apiServer,
		onEvent:         onEvent,
		store:           s,
		idStore:         idStore,
		dataRoot:        dataRoot,
		syncTracker:     syncTracker,
		triggerMaintain: triggerMaintain,
		running:         make(map[string]*runningWorkspace),
	}
}

// Run starts listeners for the initial config, then watches for changes.
// Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context, initial []config.SlackConfig) {
	for _, sl := range initial {
		m.startWorkspace(ctx, sl)
	}

	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Slack)
	}
}

// Count returns the number of running workspaces.
func (m *Manager) Count() int {
	return len(m.running)
}

// reconcile diffs the desired config against running workspaces,
// starting new ones and stopping removed ones.
func (m *Manager) reconcile(ctx context.Context, desired []config.SlackConfig) {
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

func (m *Manager) startWorkspace(ctx context.Context, sl config.SlackConfig) {
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

	go daemon.RunWithRestart(wsCtx, "slack/"+sl.Workspace, func(ctx context.Context) error {
		return m.runSlackWorkspace(ctx, sl)
	})
}

// runSlackWorkspace creates a Socket Mode connection, resolver, listener, and
// sync for a single workspace. It blocks until the connection or listener exits.
func (m *Manager) runSlackWorkspace(ctx context.Context, sl config.SlackConfig) error {
	acct := account.New("slack", sl.Workspace)

	botAPI := goslack.New(sl.BotToken, goslack.OptionAppLevelToken(sl.AppToken))
	smClient := socketmode.New(botAPI)

	userAPI := goslack.New(sl.UserToken)
	writer := identity.NewWriter(m.idStore, m.dataRoot.AccountFor(account.New("slack", sl.Workspace)).Identity())
	resolver := slacklistener.NewResolver(userAPI, botAPI, writer, sl.Workspace)
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

	messages, err := slacklistener.NewMessageStore(acct, m.store, m.triggerMaintain)
	if err != nil {
		return fmt.Errorf("create message store for %s: %w", acct, err)
	}
	listener := slacklistener.NewListener(smClient, resolver, messages, sl.UserToken, sl.BotToken, acct, sl.TeamID, botUserID, sl.AppDisplay(), m.onEvent, m.syncTracker)

	m.apiServer.RegisterSlack(&api.SlackSender{
		BotAPI:         botAPI,
		UserAPI:        userAPI,
		Resolver:       resolver,
		Messages:       messages,
		Acct:           acct,
		BotName:        botName,
		BotUserID:      botUserID,
		UserName:       userName,
		UserID:         userID,
		AppAttribution: sl.AppAttribution(),
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
