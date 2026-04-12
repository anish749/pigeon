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

// SlackManager translates Slack config entries into supervised Listeners.
// Actual lifetime, restarts, and backoff live in the Supervisor.
type SlackManager struct {
	sup        *lifecycle.Supervisor
	apiServer  *api.Server
	onMessage  hub.MessageNotifyFunc
	onReaction hub.ReactionNotifyFunc
	store      store.Store
	identity   *identity.Service
}

// NewSlackManager creates a manager that registers Slack senders with the
// given API server. onMessage is called when a routable message arrives
// (DMs, MPDMs, private channels, bot mentions). onReaction is called when
// a reaction or unreaction event arrives. Either may be nil.
func NewSlackManager(sup *lifecycle.Supervisor, apiServer *api.Server, s store.Store, onMessage hub.MessageNotifyFunc, onReaction hub.ReactionNotifyFunc, id *identity.Service) *SlackManager {
	return &SlackManager{
		sup:        sup,
		apiServer:  apiServer,
		onMessage:  onMessage,
		onReaction: onReaction,
		store:      s,
		identity:   id,
	}
}

// Run applies the initial config and reconciles on every change.
func (m *SlackManager) Run(ctx context.Context, initial []config.SlackConfig) {
	m.reconcile(ctx, initial)
	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Slack)
	}
}

func (m *SlackManager) reconcile(ctx context.Context, desired []config.SlackConfig) {
	listeners := make([]lifecycle.Listener, 0, len(desired))
	for _, sl := range desired {
		listeners = append(listeners, &slackListenerAdapter{
			cfg:        sl,
			apiServer:  m.apiServer,
			store:      m.store,
			identity:   m.identity,
			onMessage:  m.onMessage,
			onReaction: m.onReaction,
		})
	}
	if err := m.sup.Reconcile(listeners); err != nil {
		slog.ErrorContext(ctx, "slack reconcile failed", "error", err)
	}
}

// slackListenerAdapter builds a Slack Socket Mode stack for one workspace
// and runs it until ctx is cancelled or the socket mode client exits with
// an error (which bubbles up to the Supervisor for restart).
type slackListenerAdapter struct {
	cfg        config.SlackConfig
	apiServer  *api.Server
	store      store.Store
	identity   *identity.Service
	onMessage  hub.MessageNotifyFunc
	onReaction hub.ReactionNotifyFunc
}

func (l *slackListenerAdapter) ID() string { return "slack/" + l.cfg.TeamID }

func (l *slackListenerAdapter) Run(ctx context.Context) error {
	sl := l.cfg
	if sl.AppToken == "" || sl.BotToken == "" || sl.UserToken == "" {
		// Misconfiguration — restarting won't help, so exit cleanly.
		slog.ErrorContext(ctx, "slack workspace missing required token(s), skipping",
			"workspace", sl.Workspace,
			"has_app_token", sl.AppToken != "",
			"has_bot_token", sl.BotToken != "",
			"has_user_token", sl.UserToken != "")
		return nil
	}

	acct := account.New("slack", sl.Workspace)

	botAPI := goslack.New(sl.BotToken, goslack.OptionAppLevelToken(sl.AppToken))
	smClient := socketmode.New(botAPI)

	userAPI := goslack.New(sl.UserToken)
	resolver := slacklistener.NewResolver(userAPI, l.identity, sl.Workspace)
	users, channels, err := resolver.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names", "account", acct, "error", err)
	}

	var userName, userID string
	if authResp, err := userAPI.AuthTestContext(ctx); err == nil {
		userID = authResp.UserID
		name, nerr := resolver.UserName(ctx, userID)
		if nerr != nil {
			slog.WarnContext(ctx, "failed to resolve Slack user name", "account", acct, "user_id", userID, "error", nerr)
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
	listener := slacklistener.NewListener(smClient, resolver, messages, sl.UserToken, sl.BotToken, acct, sl.TeamID, botUserID, l.onMessage, l.onReaction)

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
	defer l.apiServer.UnregisterSlack(acct)

	slog.InfoContext(ctx, "slack listener started", "account", acct, "users", users, "channels", channels)

	// Event loop reads from smClient.Events; it exits when the events channel
	// closes (which happens after socket mode's RunContext returns).
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		listener.Run(ctx)
	}()

	runErr := smClient.RunContext(ctx)
	<-listenerDone

	if ctx.Err() != nil {
		return nil
	}
	if runErr != nil {
		return fmt.Errorf("slack socket mode %s: %w", acct.Display(), runErr)
	}
	// Socket mode returned without ctx cancellation and without an error —
	// treat this as an unexpected exit so the Supervisor restarts us.
	return fmt.Errorf("slack socket mode %s exited unexpectedly", acct.Display())
}
