package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE observations (
 id TEXT PRIMARY KEY, repository TEXT NOT NULL, pr INTEGER NOT NULL CHECK(pr > 0),
 head TEXT NOT NULL CHECK(length(head)=40), base TEXT NOT NULL CHECK(length(base)=40),
 first_seen TEXT NOT NULL, UNIQUE(repository, pr, head, base)
);
CREATE TABLE polls (repository TEXT PRIMARY KEY, observed_at TEXT NOT NULL);
CREATE TABLE current_prs (
 repository TEXT NOT NULL, pr INTEGER NOT NULL, observation_id TEXT NOT NULL REFERENCES observations(id),
 PRIMARY KEY(repository, pr)
);
PRAGMA user_version=1;`

type Store struct {
	db    *sql.DB
	owner *os.File
}

// OpenStore holds an OS lock for the owner's lifetime. The kernel releases it
// on exit/crash; never delete the lock file or use a stale PID as authority.
// All runners for an installation must use this same local state directory.
func OpenStore(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("state directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("state directory must be private (chmod 700)")
	}
	lock, err := os.OpenFile(filepath.Join(dir, "owner.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("state directory already owned or cannot be locked: %w", err)
	}
	s := &Store{owner: lock}
	if err := s.open(dir); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) open(dir string) error {
	path := filepath.Join(dir, "state.sqlite")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	file.Close()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("state database must be a private regular file (chmod 600)")
	}
	s.db = connect(path, "rw")
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != 1 {
		return fmt.Errorf("unsupported state schema %d", version)
	}
	if _, err := s.db.Exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;"); err != nil {
		return err
	}
	if version == 1 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	return tx.Commit()
}

func connect(path, mode string) *sql.DB {
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"mode": {mode}, "_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}}.Encode()
	// This driver is statically registered and does not implement DriverContext:
	// sql.Open cannot fail here. Actual SQLite opens/errors occur on the first use.
	db, _ := sql.Open("sqlite", u.String())
	db.SetMaxOpenConns(1)
	return db
}

func (s *Store) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	if s.owner != nil {
		err = errors.Join(err, s.owner.Close())
	}
	return err
}

func (s *Store) Record(ctx context.Context, command RecordSnapshot) error {
	if command.repository == "" {
		return fmt.Errorf("validated snapshot required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Ignore older deliveries; a delayed snapshot cannot roll back current input.
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT observed_at FROM polls WHERE repository=?", command.repository).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	stamp := command.at.Format(time.RFC3339Nano)
	if err == nil {
		at, err := time.Parse(time.RFC3339Nano, previous)
		if err != nil {
			return err
		}
		if !command.at.After(at) {
			return nil
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM current_prs WHERE repository=?", command.repository); err != nil {
		return err
	}
	for _, r := range command.revisions {
		if _, err := tx.ExecContext(ctx, "INSERT INTO observations VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING", r.ID, r.Repository, r.PR, r.Head, r.Base, stamp); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO current_prs VALUES(?,?,?)", r.Repository, r.PR, r.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO polls VALUES(?,?) ON CONFLICT(repository) DO UPDATE SET observed_at=excluded.observed_at", command.repository, stamp); err != nil {
		return err
	}
	return tx.Commit()
}

type Poll struct {
	Repository string    `json:"repository"`
	ObservedAt time.Time `json:"observed_at"`
}
type Status struct {
	Mode              string     `json:"mode"`
	Polls             []Poll     `json:"last_successful_polls"`
	Candidates        []Revision `json:"current_candidates"`
	ObservedRevisions int        `json:"observed_revisions"`
}

// ReadStatus opens existing state read-only. It neither creates a scheduler nor
// claims liveness from an old timestamp. Reads use one consistent transaction.
func ReadStatus(ctx context.Context, dir string) (Status, error) {
	if !filepath.IsAbs(dir) {
		return Status{}, fmt.Errorf("state directory must be absolute")
	}
	db := connect(filepath.Join(dir, "state.sqlite"), "ro")
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Status{}, err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return Status{}, err
	}
	if version != 1 {
		return Status{}, fmt.Errorf("unsupported state schema %d", version)
	}
	result := Status{Mode: "intake-only", Polls: []Poll{}, Candidates: []Revision{}}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM observations").Scan(&result.ObservedRevisions); err != nil {
		return Status{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT repository, observed_at FROM polls ORDER BY repository")
	if err != nil {
		return Status{}, err
	}
	for rows.Next() {
		var p Poll
		var stamp string
		if err := rows.Scan(&p.Repository, &stamp); err != nil {
			rows.Close()
			return Status{}, err
		}
		p.ObservedAt, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			rows.Close()
			return Status{}, err
		}
		result.Polls = append(result.Polls, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Status{}, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT o.id,o.repository,o.pr,o.head,o.base,o.first_seen FROM observations o JOIN current_prs c ON c.observation_id=o.id ORDER BY o.repository,o.pr")
	if err != nil {
		return Status{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Revision
		var stamp string
		if err := rows.Scan(&r.ID, &r.Repository, &r.PR, &r.Head, &r.Base, &stamp); err != nil {
			return Status{}, err
		}
		r.FirstSeen, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return Status{}, err
		}
		result.Candidates = append(result.Candidates, r)
	}
	return result, rows.Err()
}
