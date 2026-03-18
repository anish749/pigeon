package walog

import (
	"context"
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

var minLevel = slog.LevelInfo

// SetMinLevel sets the minimum log level for whatsmeow logging.
func SetMinLevel(level slog.Level) {
	minLevel = level
}

type slogAdapter struct {
	ctx    context.Context
	module string
}

// New creates a waLog.Logger that routes whatsmeow logs through slog.
func New(ctx context.Context, module string) waLog.Logger {
	return &slogAdapter{ctx: ctx, module: module}
}

func (s *slogAdapter) Debugf(msg string, args ...any) {
	if minLevel > slog.LevelDebug {
		return
	}
	slog.DebugContext(s.ctx, fmt.Sprintf(msg, args...), "module", s.module)
}

func (s *slogAdapter) Infof(msg string, args ...any) {
	if minLevel > slog.LevelInfo {
		return
	}
	slog.InfoContext(s.ctx, fmt.Sprintf(msg, args...), "module", s.module)
}

func (s *slogAdapter) Warnf(msg string, args ...any) {
	if minLevel > slog.LevelWarn {
		return
	}
	slog.WarnContext(s.ctx, fmt.Sprintf(msg, args...), "module", s.module)
}

func (s *slogAdapter) Errorf(msg string, args ...any) {
	slog.ErrorContext(s.ctx, fmt.Sprintf(msg, args...), "module", s.module)
}

func (s *slogAdapter) Sub(module string) waLog.Logger {
	return &slogAdapter{ctx: s.ctx, module: s.module + "/" + module}
}
