package store

import "errors"

// Sentinel errors the store returns. Callers match them with errors.Is; the
// CLI is the only place that turns them into process exit codes (D-16).
var (
	// ErrConflict means the compare-and-swap revision did not match the
	// stored revision; nothing was written. Exit code 3.
	ErrConflict = errors.New("draft revision conflict")

	// ErrSchemaTooNew means the database was written by a newer nfsense
	// whose schema this binary cannot read. Exit code 4.
	ErrSchemaTooNew = errors.New("store schema is newer than this binary")

	// ErrCorruptDB means the file at the store path is not a database this
	// binary can read (SQLITE_NOTADB, or a truncated schema). Exit code 4.
	ErrCorruptDB = errors.New("store file is not a valid database")

	// ErrNoDraft means the singleton working draft row is missing after
	// migration, i.e. the migration was tampered with. Exit code 4.
	ErrNoDraft = errors.New("working draft row is missing")
)
