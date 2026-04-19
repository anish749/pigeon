package ingress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/workstream/models"
)

// StoreSource reads persisted messaging data directly from the filesystem-backed
// pigeon store and normalizes it into workstream signals.
//
// It is intentionally narrow: account + conversation + absolute cursor. The
// hub can call it whenever a listener hints that a conversation may have new
// data, and it will recover the actual missing signals from disk.
type StoreSource struct {
	root paths.DataRoot
}

// NewStoreSource creates a Source rooted at the on-disk pigeon data store.
func NewStoreSource(root paths.DataRoot) *StoreSource {
	return &StoreSource{root: root}
}

// ListSignals reads all persisted message signals for a single conversation
// after the given absolute timestamp. It currently supports Slack and
// WhatsApp conversations, including thread replies.
func (s *StoreSource) ListSignals(_ context.Context, acct account.Account, conversation string, since time.Time) ([]models.Signal, error) {
	sigType, err := signalTypeForAccount(acct)
	if err != nil {
		return nil, err
	}

	convDir := s.root.AccountFor(acct).Conversation(conversation)
	dateFiles, err := listDateFiles(convDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list conversation date files: %w", err)
	}

	var signals []models.Signal
	var errs []error

	for _, path := range dateFiles {
		if !dateFileMaybeAfter(path, since) {
			continue
		}
		lines, err := readLines(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", path, err))
		}
		signals = append(signals, normalizeLines(acct, conversation, "", sigType, lines, since)...)
	}

	threadFiles, err := listThreadFiles(convDir.ThreadsDir())
	if err != nil {
		if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("list thread files: %w", err))
		}
	} else {
		for _, path := range threadFiles {
			threadID := strings.TrimSuffix(filepath.Base(path), paths.FileExt)
			lines, err := readLines(path)
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s: %w", path, err))
			}
			signals = append(signals, normalizeLines(acct, conversation, threadID, sigType, lines, since)...)
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Ts.Equal(signals[j].Ts) {
			return signals[i].ID < signals[j].ID
		}
		return signals[i].Ts.Before(signals[j].Ts)
	})

	return signals, errors.Join(errs...)
}

func signalTypeForAccount(acct account.Account) (models.SignalType, error) {
	switch acct.Platform {
	case "slack":
		return models.SignalSlackMessage, nil
	case "whatsapp":
		return models.SignalWhatsApp, nil
	default:
		return "", fmt.Errorf("workstream ingress: unsupported account platform %q", acct.Platform)
	}
}

func normalizeLines(acct account.Account, conversation, threadID string, sigType models.SignalType, lines []modelv1.Line, since time.Time) []models.Signal {
	var signals []models.Signal
	for _, line := range lines {
		if line.Type != modelv1.LineMessage || line.Msg == nil {
			continue
		}
		if !line.Msg.Ts.After(since) {
			continue
		}
		signals = append(signals, models.Signal{
			ID:           line.Msg.ID,
			Type:         sigType,
			Account:      acct,
			Conversation: conversation,
			ThreadID:     threadID,
			Ts:           line.Msg.Ts,
			Sender:       line.Msg.Sender,
			Text:         line.Msg.Text,
		})
	}
	return signals
}

func dateFileMaybeAfter(path string, since time.Time) bool {
	base := filepath.Base(path)
	dateStr := strings.TrimSuffix(base, paths.FileExt)
	day, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return true
	}
	dayEnd := day.Add(24*time.Hour - time.Nanosecond)
	return dayEnd.After(since)
}

func listDateFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && paths.IsDateFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func listThreadFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), paths.FileExt) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func readLines(path string) ([]modelv1.Line, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 2*1024*1024), 2*1024*1024)

	var lines []modelv1.Line
	var errs []error
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		line, err := modelv1.Parse(text)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		errs = append(errs, fmt.Errorf("scan %s: %w", path, err))
	}
	return lines, errors.Join(errs...)
}
