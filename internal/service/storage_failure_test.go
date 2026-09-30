package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericdmoore/ferretta/internal/github"
)

func TestDamagedStateIsNotReportedAsHealthy(t *testing.T) {
	// Exercise the actual adapter against malformed/missing stored data. A future
	// migration or damaged store must fail explicitly, never publish partial status.
	for name, damage := range map[string]string{
		"missing observations": "PRAGMA foreign_keys=OFF; DROP TABLE observations",
		"missing polls":        "DROP TABLE polls",
		"missing current":      "DROP TABLE current_prs",
		"invalid poll time":    "UPDATE polls SET observed_at='invalid'",
		"null poll identity":   "UPDATE polls SET repository=NULL",
		"invalid first seen":   "UPDATE observations SET first_seen='invalid'",
		"invalid PR number":    "UPDATE observations SET pr='invalid'",
		"row evaluation error": "DROP TABLE polls; CREATE VIEW polls AS SELECT 'o/r' AS repository, abs(-9223372036854775808) AS observed_at",
	} {
		t.Run(name, func(t *testing.T) {
			s, dir := openTestStore(t)
			record(t, s, snapshot(t, "o/r", []github.PullRequest{testPR(1)}, testTime))
			if _, err := s.db.Exec(damage); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadStatus(context.Background(), dir); err == nil {
				t.Fatal("damaged state reported successfully")
			}
		})
	}
}

func TestStorageAdmissionFailures(t *testing.T) {
	for name, damage := range map[string]string{
		"missing polls":     "DROP TABLE polls",
		"invalid timestamp": "UPDATE polls SET observed_at='invalid'",
		"delete failure":    "CREATE TRIGGER fail_delete BEFORE DELETE ON current_prs BEGIN SELECT RAISE(ABORT,'storage failure'); END",
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := openTestStore(t)
			record(t, s, snapshot(t, "o/r", []github.PullRequest{testPR(1)}, testTime))
			if _, err := s.db.Exec(damage); err != nil {
				t.Fatal(err)
			}
			if err := s.Record(context.Background(), snapshot(t, "o/r", nil, testTime.Add(time.Minute))); err == nil {
				t.Fatal("corruption ignored during admission")
			}
		})
	}
}

func TestInitializationFailureReleasesOwner(t *testing.T) {
	s, dir := openTestStore(t)
	if _, err := s.db.Exec("PRAGMA user_version=0"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("partially initialized database accepted")
	}
	// A failed open must release ownership so an operator can repair and restart.
	if err := os.Remove(filepath.Join(dir, "state.sqlite")); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
