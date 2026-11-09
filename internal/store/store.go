// Package store owns the SQLite file: the embedded schema, the migration
// ledger, and the two operations the intended state needs (read the working
// draft, write the working draft). It imports no other nfsense package except
// the pure draft document format, and it never touches the live firewall.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // pure-Go driver, registered as "sqlite"

	"github.com/hrodrig/nfsense/internal/draftdoc"
	"github.com/hrodrig/nfsense/internal/store/migrations"
)

// BackendNft is the only backend value v1 accepts. The CHECK constraints in
// the schema and this constant are the seam a future "pf" driver would widen
// (D-11) — the string is stored, so changing it later is a migration.
const BackendNft = "nft"

// Draft is the persisted working draft as read back from SQLite.
type Draft struct {
	Revision      int64
	Backend       string
	FormatVersion int
	Document      []byte
	UpdatedAt     string
}

// Store is an open handle on one nfsense database file.
type Store struct {
	db   *sql.DB
	path string
}

// Open prepares dir with mode 0700, opens <path> with mode 0600, refuses a
// store written by a newer binary, then brings the schema up to date. The
// data directory is a parameter on purpose: the canonical /var/lib/nfsense is
// resolved by the CLI (plan 05), and every test passes its own directory.
func Open(ctx context.Context, path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("restrict data dir %s: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, openError(path, dir, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close probe of %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("restrict store file %s: %w", path, err)
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, openError(path, dir, err)
	}

	s := &Store{db: db, path: path}
	if err := s.checkSchema(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// dsn keeps SQLite foreign keys on, waits out a concurrent writer instead of
// failing immediately, and takes the write lock at BEGIN so two writers
// serialize instead of one dying on "database is locked" mid-transaction.
func dsn(path string) string {
	return "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(FULL)" +
		"&_pragma=foreign_keys(1)" +
		"&_txlock=immediate"
}

// openError turns the two failures an operator actually hits into advice:
// a too-new/corrupt file, and a directory this euid cannot write.
func openError(path, dir string, err error) error {
	switch {
	case isNotADatabase(err):
		return fmt.Errorf("%w: %s", ErrCorruptDB, path)
	case strings.Contains(err.Error(), "readonly"),
		strings.Contains(err.Error(), "unable to open database file"),
		strings.Contains(err.Error(), "permission denied"):
		mode := "unknown"
		if fi, statErr := os.Stat(dir); statErr == nil {
			mode = fi.Mode().Perm().String()
		}
		return fmt.Errorf("cannot open %s as euid %d (dir %s mode %s): %w; "+
			"pass --data-dir or NFSENSE_DATA_DIR pointing at a writable directory",
			path, os.Geteuid(), dir, mode, err)
	default:
		return fmt.Errorf("open sqlite %s: %w", path, err)
	}
}

func (s *Store) migrate(ctx context.Context) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, s.db, migrations.FS,
		goose.WithTableName("schema_migrations"))
	if err != nil {
		return fmt.Errorf("init migrator for %s: %w", s.path, err)
	}
	if _, upErr := provider.Up(ctx); upErr != nil {
		if isNotADatabase(upErr) {
			return fmt.Errorf("%w: %s", ErrCorruptDB, s.path)
		}
		return fmt.Errorf("migrate %s: %w", s.path, upErr)
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// GetDraft returns the singleton working draft. The document bytes are
// returned exactly as stored — no re-encoding — so unknown top-level keys
// survive a read/write cycle byte for byte (D-10).
func (s *Store) GetDraft(ctx context.Context) (Draft, error) {
	var d Draft
	var doc string
	err := s.db.QueryRowContext(ctx,
		`SELECT revision, backend, format_version, document, updated_at
		   FROM working_draft
		  WHERE id = 1`).
		Scan(&d.Revision, &d.Backend, &d.FormatVersion, &doc, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNoDraft
	}
	if err != nil {
		return Draft{}, fmt.Errorf("read working draft: %w", err)
	}
	d.Document = []byte(doc)
	return d, nil
}

// SaveDraft validates raw with draftdoc before any SQL runs, then performs a
// single compare-and-swap UPDATE that bumps the revision.
//
// expected is the revision the caller read. If the stored revision differs the
// UPDATE matches no row, nothing is written, and ErrConflict is returned
// (D-15, T-1-06). The read-then-write inside one immediate transaction is what
// keeps two operators from silently overwriting each other.
func (s *Store) SaveDraft(ctx context.Context, actor string, raw []byte, expected int64) (int64, error) {
	env, err := draftdoc.Parse(raw)
	if err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin save draft: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var revision int64
	err = tx.QueryRowContext(ctx,
		`UPDATE working_draft
		    SET revision       = revision + 1,
		        backend        = ?,
		        format_version = ?,
		        document       = ?,
		        updated_by     = ?,
		        updated_at     = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		  WHERE id = 1 AND revision = ?
	      RETURNING revision`,
		BackendNft, env.FormatVersion, string(raw), actor, expected).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("save working draft: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit save draft: %w", err)
	}
	return revision, nil
}

// Health reports whether the store answers a trivial read on the current
// handle. It is what `nfsense status` will call (plan 05); it deliberately does
// not touch the firewall.
func (s *Store) Health(ctx context.Context) error {
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM working_draft WHERE id = 1`).Scan(&one); err != nil {
		return fmt.Errorf("store health: %w", err)
	}
	return nil
}
