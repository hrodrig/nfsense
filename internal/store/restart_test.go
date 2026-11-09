package store

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// helperProcessEnv selects child mode. It is set by the parent in cmd.Env
	// and never read from the developer's shell (see environWithout).
	helperProcessEnv = "GO_WANT_HELPER_PROCESS"

	// helperDBEnv carries the database path the child must write. The child
	// refuses to run without it rather than defaulting to a path of its own
	// (T-1-12: a child that guesses writes to the wrong database).
	helperDBEnv = "NFSENSE_TEST_DB"
)

// TestDraftSurvivesProcessRestart is SAFE-01's primary proof and runs in two
// modes chosen by GO_WANT_HELPER_PROCESS:
//
//   - child (helperProcessEnv=1): writes the fixture through a store on
//     NFSENSE_TEST_DB, closes it, and exits. Only the file on disk outlives it.
//   - parent (default): proves the handle-reopen case, then re-executes this
//     same test binary in child mode, waits for it to exit, and only then
//     reads the file back from a store of its own.
//
// The child is deliberately not a second Test function: the phase validation
// matrix counts a fixed set of rows and a helper test with its own name would
// sit outside it (W2). The environment switch keeps exactly one test here.
func TestDraftSurvivesProcessRestart(t *testing.T) {
	if os.Getenv(helperProcessEnv) == "1" {
		runRestartHelper(t)
		return
	}

	ctx := context.Background()

	// Branch 1 — reopen the same file through a new handle in this process.
	// Cheap, and it already fails if the draft only ever lived in memory.
	t.Log("branch 1: write, close, reopen")
	path := filepath.Join(t.TempDir(), "nfsense.db")
	want := draftFixture(t, true)

	s1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open store #1: %v", err)
	}
	rev, err := s1.SaveDraft(ctx, "local:writer", want, 0)
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if rev != 1 {
		t.Fatalf("saved revision = %d, want 1", rev)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close store #1: %v", err)
	}

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open store #2: %v", err)
	}
	got, err := s2.GetDraft(ctx)
	if err != nil {
		s2.Close()
		t.Fatalf("get draft from reopened store: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close store #2: %v", err)
	}
	if got.Revision != 1 {
		t.Fatalf("reopened revision = %d, want 1", got.Revision)
	}
	if !bytes.Equal(got.Document, want) {
		t.Fatalf("reopened document is not byte-identical\n got %q\nwant %q", got.Document, want)
	}

	// Branch 2 — a real process boundary. A second OS process with its own
	// heap and its own handles writes and exits; nothing of it survives except
	// the file. This is the part a reopened handle cannot prove.
	t.Log("branch 2: child process writes and exits, parent reads")
	childPath := filepath.Join(t.TempDir(), "nfsense.db")
	childWant := draftFixture(t, true)

	cmd := exec.Command(os.Args[0], "-test.run=^TestDraftSurvivesProcessRestart$", "-test.v")
	cmd.Env = append(environWithout(helperProcessEnv, helperDBEnv),
		helperProcessEnv+"=1",
		helperDBEnv+"="+childPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}
	t.Logf("child exited 0, wrote %d bytes to %s", len(childWant), childPath)

	s3, err := Open(ctx, childPath)
	if err != nil {
		t.Fatalf("open store over child's file: %v\n%s", err, out)
	}
	defer func() {
		if err := s3.Close(); err != nil {
			t.Errorf("close store over child's file: %v", err)
		}
	}()
	got, err = s3.GetDraft(ctx)
	if err != nil {
		t.Fatalf("get draft written by child: %v", err)
	}
	if got.Revision != 1 {
		t.Fatalf("revision from child's write = %d, want 1 (a revision held only in memory could not survive the exit)",
			got.Revision)
	}
	if !bytes.Equal(got.Document, childWant) {
		t.Fatalf("document written by child is not byte-identical\n got %q\nwant %q", got.Document, childWant)
	}
	assertFormatColumns(t, s3, BackendNft, 1)
}

// runRestartHelper is child mode: one write, one close, exit. It never returns.
func runRestartHelper(t *testing.T) {
	t.Helper()
	path := os.Getenv(helperDBEnv)
	if path == "" {
		t.Fatalf("%s=1 but %s is unset: the parent must name the database explicitly",
			helperProcessEnv, helperDBEnv)
	}

	ctx := context.Background()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("helper open %s: %v", path, err)
	}
	rev, err := s.SaveDraft(ctx, "local:helper", draftFixture(t, true), 0)
	if err != nil {
		t.Fatalf("helper save: %v", err)
	}
	if rev != 1 {
		t.Fatalf("helper revision = %d, want 1", rev)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("helper close: %v", err)
	}
	t.Logf("helper wrote revision %d to %s", rev, path)

	// Exit rather than return: returning would fall through to the parent
	// branch inside the child, and the exit status is how the parent learns
	// the write succeeded.
	os.Exit(0)
}

// environWithout returns os.Environ() minus any assignment of the named keys,
// so a stale value exported in the developer's shell cannot decide which
// database the child writes (T-1-12).
func environWithout(keys ...string) []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, k := range keys {
			if name == k {
				drop = true
				break
			}
		}
		if !drop {
			env = append(env, kv)
		}
	}
	return env
}
