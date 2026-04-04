package claude

import (
	"context"
	"log/slog"

	"github.com/fsnotify/fsnotify"
)

// WatchSessions watches the sessions directory for new or updated session
// files and sends the full list of sessions on the returned channel whenever
// a change is detected. The channel is closed when ctx is cancelled.
func WatchSessions(ctx context.Context) <-chan []*Session {
	ch := make(chan []*Session)

	go func() {
		defer close(ch)

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			slog.ErrorContext(ctx, "failed to create session watcher", "error", err)
			return
		}
		defer watcher.Close()

		dir := SessionsDir()
		if err := watcher.Add(dir); err != nil {
			// Directory may not exist yet — that's fine, no sessions to watch.
			slog.InfoContext(ctx, "sessions directory not found, watching skipped", "dir", dir)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
					continue
				}
				sessions, err := ListAllSessions()
				if err != nil {
					slog.ErrorContext(ctx, "failed to reload sessions", "error", err)
					continue
				}
				select {
				case ch <- sessions:
				case <-ctx.Done():
					return
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.ErrorContext(ctx, "session watcher error", "error", err)
			}
		}
	}()

	return ch
}
