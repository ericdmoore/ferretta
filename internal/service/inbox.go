package service

import (
	"bytes"
	"context"
	"fmt"
)

type InboxItem struct {
	ID   string
	Data []byte
}

// QueueRetry stores immutable input before acknowledging webhook delivery.
// Completed receipts remain present so a redelivery cannot grant more work.
func (s *Store) QueueRetry(ctx context.Context, id string, data []byte) error {
	if id == "" || len(id) > 100 || len(data) == 0 || len(data) > 4096 {
		return fmt.Errorf("invalid retry receipt")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO retry_inbox(id,data) VALUES(?,?) ON CONFLICT DO NOTHING`, id, data); err != nil {
		return err
	}
	var existing []byte
	if err := s.db.QueryRowContext(ctx, `SELECT data FROM retry_inbox WHERE id=?`, id).Scan(&existing); err != nil {
		return err
	}
	if !bytes.Equal(existing, data) {
		return fmt.Errorf("retry delivery ID reused with different content")
	}
	return nil
}

func (s *Store) PendingRetries(ctx context.Context) ([]InboxItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,data FROM retry_inbox WHERE outcome='' ORDER BY rowid LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []InboxItem
	for rows.Next() {
		var item InboxItem
		if err := rows.Scan(&item.ID, &item.Data); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) FinishRetry(ctx context.Context, id, outcome string) error {
	if outcome == "" {
		return fmt.Errorf("retry outcome required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE retry_inbox SET outcome=? WHERE id=? AND outcome=''`, outcome, id)
	return err
}
