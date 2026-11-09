package store

import (
	"bytes"
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

// TestDraftRoundTripPreservesBytes is D-10 made executable: the stored
// document must be the exact bytes handed to SaveDraft. The fixture is hostile
// to any re-serialization — `format_version` comes before `format`, `ui` sits
// after `body`, both are keys the store does not model, and the body holds
// multi-byte UTF-8 long enough that the byte count exceeds the rune count
// (S10: the limit is bytes on both sides).
func TestDraftRoundTripPreservesBytes(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	raw := []byte(`{"format_version":1,"format":"nfsense.draft",` +
		`"body":{"zulu":"→ 日本","alpha":["é","naïve"],"note":"multi-byte ✓"},` +
		`"ui":{"theme":"dark"},"x-notes":"kept verbatim"}`)

	runes, bytelen := len([]rune(string(raw))), len(raw)
	if runes >= bytelen {
		t.Fatalf("fixture is not multi-byte: %d runes vs %d bytes", runes, bytelen)
	}

	rev, err := s.SaveDraft(ctx, "local:test", raw, 0)
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if rev != 1 {
		t.Fatalf("saved revision = %d, want 1", rev)
	}

	got, err := s.GetDraft(ctx)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if got.Revision != 1 {
		t.Fatalf("read revision = %d, want 1", got.Revision)
	}
	if !bytes.Equal(got.Document, raw) {
		t.Fatalf("stored document is not byte-identical to what was written\n got (%d bytes) %q\nwant (%d bytes) %q",
			len(got.Document), got.Document, len(raw), raw)
	}

	// The columns, read directly: `backend` is the backend constant and
	// `format_version` the envelope's — the envelope's `format` ("nfsense.draft")
	// never reaches a column (D-10).
	assertFormatColumns(t, s, BackendNft, 1)
}

// TestSaveDraftUpdatesFormatColumns proves W7: backend and format_version are
// written on every save rather than left as whatever the previous writer left
// behind. Both columns are corrupted behind the CHECK constraints' back, and a
// single save must put them back.
func TestSaveDraftUpdatesFormatColumns(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	if _, err := s.SaveDraft(ctx, "local:test", draftFixture(t, false), 0); err != nil {
		t.Fatalf("first save: %v", err)
	}
	assertFormatColumns(t, s, BackendNft, 1)

	// 'pf' is not a value the schema accepts, so the corruption has to happen
	// with constraint checking disabled; 99 is legal SQL but a version this
	// binary would never write. The pragma is per connection, and the pool is
	// capped at one connection, so both the corruption and the save below see
	// it — which is exactly why the pragma is switched back off immediately.
	if _, err := s.db.ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Fatalf("enable ignore_check_constraints: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.db.ExecContext(context.Background(), `PRAGMA ignore_check_constraints = OFF`)
	})
	if _, err := s.db.ExecContext(ctx,
		`UPDATE working_draft SET backend = 'pf', format_version = 99 WHERE id = 1`); err != nil {
		t.Fatalf("corrupt format columns: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA ignore_check_constraints = OFF`); err != nil {
		t.Fatalf("disable ignore_check_constraints: %v", err)
	}
	assertFormatColumns(t, s, "pf", 99)

	// The corruption did not touch the revision, so a save expecting 1 is a
	// legitimate write, not a conflict.
	rev, err := s.SaveDraft(ctx, "local:test", draftFixture(t, true), 1)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if rev != 2 {
		t.Fatalf("second save revision = %d, want 2", rev)
	}
	assertFormatColumns(t, s, BackendNft, 1)
}
