package store

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// WriteMeta writes .meta.json for a conversation directory.
func (s *FSStore) WriteMeta(acct account.Account, conversation string, meta modelv1.ConversationMeta) error {
	conv := s.convDir(acct, conversation)
	if err := os.MkdirAll(conv.Path(), 0755); err != nil {
		return fmt.Errorf("create conversation dir: %w", err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(conv.MetaFile(), data, 0644)
}

// ReadMeta reads .meta.json for a conversation. Returns nil if the file
// does not exist.
func (s *FSStore) ReadMeta(acct account.Account, conversation string) (*modelv1.ConversationMeta, error) {
	conv := s.convDir(acct, conversation)
	data, err := os.ReadFile(conv.MetaFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var meta modelv1.ConversationMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse .meta.json: %w", err)
	}
	return &meta, nil
}
