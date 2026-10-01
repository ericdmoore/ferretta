package service

import (
	"context"
	"strings"
	"testing"
)

func TestRetryInboxSurvivesRestartAndRedelivery(t *testing.T) {
	s, dir := openTestStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec("DROP TABLE retry_inbox; PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReview(ctx, "saved-run", []byte("original")); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := s.Review(ctx, "saved-run"); err != nil || string(data) != "original" {
		t.Fatal("migration lost review", err)
	}
	for _, id := range []string{"delivery-2", "delivery-1", "delivery-2"} {
		if err := s.QueueRetry(ctx, id, []byte(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.QueueRetry(ctx, "delivery-2", []byte("conflicting")); err == nil {
		t.Fatal("delivery overwritten")
	}
	pending, err := s.PendingRetries(ctx)
	if err != nil || len(pending) != 2 || pending[0].ID != "delivery-2" {
		t.Fatal(pending, err)
	}
	if err := s.FinishRetry(ctx, "delivery-2", "accepted"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.QueueRetry(ctx, "delivery-2", []byte("delivery-2")); err != nil {
		t.Fatal(err)
	}
	pending, err = s.PendingRetries(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != "delivery-1" {
		t.Fatal("receipt lost", pending, err)
	}
	if err := s.FinishRetry(ctx, "delivery-1", "rejected: stale"); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRetry(ctx, "delivery-1", "accepted"); err != nil {
		t.Fatal(err)
	}
	var outcome string
	if err := s.db.QueryRow("SELECT outcome FROM retry_inbox WHERE id='delivery-1'").Scan(&outcome); err != nil || outcome != "rejected: stale" {
		t.Fatal("outcome overwritten", outcome, err)
	}
	if err := s.FinishRetry(ctx, "delivery-1", ""); err == nil {
		t.Fatal("empty outcome")
	}
	for _, item := range []InboxItem{{}, {ID: strings.Repeat("x", 101), Data: []byte("x")}, {ID: "x", Data: []byte(strings.Repeat("x", 4097))}} {
		if err := s.QueueRetry(ctx, item.ID, item.Data); err == nil {
			t.Fatal("invalid receipt")
		}
	}
	s.Close()
	if err := s.QueueRetry(ctx, "x", []byte("data")); err == nil {
		t.Fatal("closed queue accepted")
	}
	if _, err := s.PendingRetries(ctx); err == nil {
		t.Fatal("closed queue read")
	}
	if err := s.FinishRetry(ctx, "x", "accepted"); err == nil {
		t.Fatal("closed queue acknowledged")
	}
}

func TestRetryInboxCorruptionFailsClosed(t *testing.T) {
	for _, damage := range []string{
		"CREATE TRIGGER vanish AFTER INSERT ON retry_inbox BEGIN DELETE FROM retry_inbox; END",
		"DROP TABLE retry_inbox; CREATE VIEW retry_inbox AS SELECT NULL AS id, 'data' AS data, '' AS outcome, 1 AS rowid",
		"DROP TABLE retry_inbox; CREATE VIEW retry_inbox AS SELECT 'id' AS id, abs(-9223372036854775808) AS data, '' AS outcome, 1 AS rowid",
	} {
		s, _ := openTestStore(t)
		if _, err := s.db.Exec(damage); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(damage, "CREATE TRIGGER") {
			if err := s.QueueRetry(context.Background(), "id", []byte("data")); err == nil {
				t.Fatal("lost receipt acknowledged")
			}
		} else if _, err := s.PendingRetries(context.Background()); err == nil {
			t.Fatal("corrupt receipt read")
		}
	}
}

func TestRetryInboxPartialMigrationRollsBack(t *testing.T) {
	s, dir := openTestStore(t)
	if _, err := s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("partially applied migration accepted")
	}
}
