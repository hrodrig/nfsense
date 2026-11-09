package store

import (
	"context"
	"path/filepath"
	"testing"
)

// newTestStore opens a store on a fresh temporary directory. The canonical
// /var/lib/nfsense is root-owned on Debian and not writable on the macOS dev
// machine, so every test owns its data directory (D-12 keeps the path a
// parameter, which is what makes this possible).
//
// The handle is closed by t.Cleanup so a failing assertion cannot leak a live
// SQLite file into the next test. Close is idempotent, so a test is free to
// close and reopen the same path itself.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nfsense.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open store %s: %v", path, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store %s: %v", path, err)
		}
	})
	return s, path
}

// queryRow reads one row straight off the store's handle. Tests use it to
// observe what SQLite actually holds rather than what the API reports; it is
// deliberately not an exported method (no production caller needs it).
func queryRow(t *testing.T, s *Store, query string, dest ...any) {
	t.Helper()
	if err := s.db.QueryRowContext(context.Background(), query).Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}

// assertFormatColumns pins the two derived columns of working_draft: the
// backend is always the constant 'nft' (never the envelope's `format`, D-10),
// and format_version is the one the envelope declared.
func assertFormatColumns(t *testing.T, s *Store, wantBackend string, wantVersion int) {
	t.Helper()
	var backend string
	var version int
	queryRow(t, s, `SELECT backend, format_version FROM working_draft WHERE id = 1`, &backend, &version)
	if backend != wantBackend || version != wantVersion {
		t.Fatalf("working_draft columns = (backend %q, format_version %d), want (%q, %d)",
			backend, version, wantBackend, wantVersion)
	}
}

// draftFixture returns a valid envelope. With extraKeys it also carries two
// top-level keys the store has never heard of ("x-notes", "ui"), which must
// survive a write/read cycle untouched: nfsense stores bytes and does not own
// its document format (D-10).
func draftFixture(t *testing.T, extraKeys bool) []byte {
	t.Helper()
	if !extraKeys {
		return []byte(`{"format":"nfsense.draft","format_version":1,"body":{"name":"lab"}}`)
	}
	return []byte(`{"format":"nfsense.draft","format_version":1,"body":{"name":"lab"},` +
		`"x-notes":"kept verbatim","ui":{"theme":"dark"}}`)
}
