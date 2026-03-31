package config

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watch watches the config file for changes and sends the reloaded Config
// on the returned channel whenever it changes. The channel is closed when
// ctx is cancelled.
func Watch(ctx context.Context) <-chan *Config {
	ch := make(chan *Config)

	go func() {
		defer close(ch)

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			slog.ErrorContext(ctx, "failed to create config watcher", "error", err)
			return
		}
		defer watcher.Close()

		// Watch the directory, not the file, because editors often
		// write to a temp file and rename — which removes a file-level watch.
		dir := filepath.Dir(ConfigPath())
		if err := watcher.Add(dir); err != nil {
			slog.ErrorContext(ctx, "failed to watch config directory", "error", err)
			return
		}

		configFile := filepath.Base(ConfigPath())

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(event.Name) != configFile {
					continue
				}
				if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
					continue
				}
				cfg, err := Load()
				if err != nil {
					slog.ErrorContext(ctx, "failed to reload config", "error", err)
					continue
				}
				select {
				case ch <- cfg:
				case <-ctx.Done():
					return
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.ErrorContext(ctx, "config watcher error", "error", err)
			}
		}
	}()

	return ch
}
