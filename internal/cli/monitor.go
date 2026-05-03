package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/tailapi"
	"github.com/anish749/pigeon/internal/utils/timeutil"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "monitor",
		Short:   "Stream incoming messages to stdout",
		GroupID: groupReading,
		Long: `Stream incoming messages and reactions as JSON lines to stdout.
Each line is one event. The stream has no natural end — it runs until interrupted.

When running this from an agent, do not set a timeout. This is a persistent
listener, not a one-shot command — a timeout will cause the agent to miss
messages that arrive after the cutoff.

Each line is a flat JSON object. Fields depend on kind.

Envelope (present on message, reaction, unreact, edit, delete):
  kind          "message", "reaction", "unreact", "edit", "delete", or "system"
  platform      platform slug, e.g. "slack", "whatsapp"
  name          account name (e.g. "acme-corp")
  conversation  channel or DM name (e.g. "#engineering", "@alice")

Message (kind=message):
  id            platform message ID
  ts            message timestamp (RFC3339)
  sender        display name of the author
  from          platform user ID of the author
  text          message body
  via           message pathway (omitted if empty)
  reply         true if this is a thread reply
  replyTo       quoted message ID (omitted if empty)
  thread_ts     parent thread's TS for Slack replies (omitted if empty)
  thread_id     parent thread's platform-neutral ID for replies (omitted if empty)
  attach        attachments (omitted if none)
  rawType       platform raw-content type (omitted if empty)
  raw           platform-specific raw payload (omitted if empty)

Reaction (kind=reaction for adds, kind=unreact for removes):
  ts            reaction timestamp (RFC3339)
  msg           target message ID
  sender        display name of the reactor
  from          platform user ID of the reactor
  emoji         emoji name (e.g. "thumbsup")
  via           message pathway (omitted if empty)

Edit (kind=edit):
  ts            edit timestamp (RFC3339) — when the edit happened
  msg           target message ID
  sender        display name of the editor
  from          platform user ID of the editor
  text          new message text (omitted if empty)
  via           message pathway (omitted if empty)
  thread_ts     parent thread's TS when target lives in a thread (omitted if empty)
  thread_id     parent thread's platform-neutral ID when target lives in a thread (omitted if empty)
  attach        complete attachment set after edit (omitted if none)
  rawType       platform raw-content type (omitted if empty)
  raw           updated platform-specific raw payload (omitted if empty)

Delete (kind=delete):
  ts            delete timestamp (RFC3339) — when the delete happened
  msg           target message ID
  sender        display name of who deleted
  from          platform user ID of who deleted
  via           message pathway (omitted if empty)
  thread_ts     parent thread's TS when target lived in a thread (omitted if empty)
  thread_id     parent thread's platform-neutral ID when target lived in a thread (omitted if empty)

System (kind=system):
  ts            RFC3339 timestamp
  content       status text ("connected", "replay error: ...")`,
		Example: `  pigeon monitor
  pigeon monitor --platform=slack --account=acme-corp
  pigeon monitor --workspace=eng --since=5m

  # Filter by kind (output stays as full JSON lines)
  pigeon monitor | grep --line-buffered '"kind":"message"'
  pigeon monitor | jq --unbuffered -c 'select(.kind == "reaction" or .kind == "unreact")'

  # Scope to one conversation
  pigeon monitor | jq --unbuffered -c 'select(.conversation == "#engineering")'

  # Project fields out of message events
  pigeon monitor | jq --unbuffered -c 'select(.kind == "message") | {sender, text, conversation}'`,
		PreRunE: ensureDaemon,
		RunE:    runMonitor,
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform (slack, whatsapp)")
	cmd.Flags().StringP("account", "a", "", "filter by account (requires --platform)")
	cmd.Flags().StringP("since", "s", "", "replay history from this duration ago (e.g. 5m, 2h)")
	return cmd
}

func runMonitor(cmd *cobra.Command, _ []string) error {
	platform, err := cmd.Flags().GetString("platform")
	if err != nil {
		return fmt.Errorf("get platform flag: %w", err)
	}
	acctName, err := cmd.Flags().GetString("account")
	if err != nil {
		return fmt.Errorf("get account flag: %w", err)
	}
	since, err := cmd.Flags().GetString("since")
	if err != nil {
		return fmt.Errorf("get since flag: %w", err)
	}

	req := tailapi.Request{}

	// Workspace resolves at the CLI layer. --platform/--account override
	// the workspace account list if both are set.
	switch {
	case platform != "" && acctName != "":
		if err := validateAccountInScope(account.New(platform, acctName)); err != nil {
			return err
		}
		req.Accounts = []account.Account{account.New(platform, acctName)}
	case platform != "":
		if activeWorkspace == nil {
			return fmt.Errorf("--platform without --account requires an active workspace")
		}
		accts := activeWorkspace.AccountsForPlatform(platform)
		if len(accts) == 0 {
			return fmt.Errorf("workspace %q has no %s accounts", activeWorkspace.Name, platform)
		}
		req.Accounts = accts
	case activeWorkspace != nil:
		req.Accounts = activeWorkspace.Accounts
	}

	if since != "" {
		d, err := timeutil.ParseDuration(since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", since, err)
		}
		req.Since = time.Now().Add(-d)
	}

	return commands.RunMonitor(cmd.Context(), req, os.Stdout)
}
