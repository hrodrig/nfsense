package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// document is deliberately non-canonical: unknown top-level key, unknown
// nested key, unsorted members and irregular spacing. If any layer re-encodes
// the JSON, one of these bytes changes.
const document = `{
  "format": "nfsense.draft",
  "format_version": 1,
  "body": {"zulu": 1, "alpha": {"nested": [1, 2, 3]}},
  "x_unknown": "kept byte for byte"
}`

// TestDraftSetShowRoundTrip is the tracer: write through the CLI, then read
// back through a fresh command tree (the in-process equivalent of a new
// process) and require byte-identical output.
func TestDraftSetShowRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.json")
	writeFile(t, path, document)

	setOut, setErr, setCode := run(t, "draft", "set", "--file", path, "--if-revision", "0", "--data-dir", dir)
	if setCode != 0 {
		t.Fatalf("draft set exit = %d, stderr = %q", setCode, setErr)
	}
	if setOut != "" {
		t.Fatalf("draft set stdout = %q, want empty", setOut)
	}
	if want := "revision=1\n"; setErr != want {
		t.Fatalf("draft set stderr = %q, want %q", setErr, want)
	}

	showOut, showErr, showCode := run(t, "draft", "show", "--data-dir", dir)
	if showCode != 0 {
		t.Fatalf("draft show exit = %d, stderr = %q", showCode, showErr)
	}
	if showOut != document {
		t.Fatalf("draft show is not byte-identical to what was written:\n got %q\nwant %q", showOut, document)
	}
	if want := "revision=1\n"; showErr != want {
		t.Fatalf("draft show stderr = %q, want %q", showErr, want)
	}
}

// TestDraftShowJSONAddsRevision proves --json merges the revision in without
// dropping the unknown top-level key or re-encoding body.
func TestDraftShowJSONAddsRevision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.json")
	writeFile(t, path, document)

	if _, stderr, code := run(t, "draft", "set", "--file", path, "--if-revision", "0", "--data-dir", dir); code != 0 {
		t.Fatalf("draft set exit = %d, stderr = %q", code, stderr)
	}

	out, stderr, code := run(t, "draft", "show", "--json", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("draft show --json exit = %d, stderr = %q", code, stderr)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("draft show --json output is not an object: %v (%q)", err, out)
	}
	for _, key := range []string{"format", "format_version", "body", "x_unknown", "revision"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("draft show --json output is missing %q: %q", key, out)
		}
	}
	var revision int64
	if err := json.Unmarshal(got["revision"], &revision); err != nil || revision != 1 {
		t.Fatalf("revision = %s (err %v), want 1", got["revision"], err)
	}
	if string(got["x_unknown"]) != `"kept byte for byte"` {
		t.Fatalf("x_unknown = %s, want it preserved", got["x_unknown"])
	}
}

// TestDraftSetStaleRevisionConflicts pins the compare-and-swap: a second write
// claiming revision 0 must fail with exit 3 and leave the draft untouched.
func TestDraftSetStaleRevisionConflicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.json")
	writeFile(t, path, document)

	if _, stderr, code := run(t, "draft", "set", "--file", path, "--if-revision", "0", "--data-dir", dir); code != 0 {
		t.Fatalf("first draft set exit = %d, stderr = %q", code, stderr)
	}

	other := filepath.Join(dir, "other.json")
	writeFile(t, other, `{"format":"nfsense.draft","format_version":1,"body":{"replaced":true}}`)
	if _, _, code := run(t, "draft", "set", "--file", other, "--if-revision", "0", "--data-dir", dir); code != exitConflict {
		t.Fatalf("stale draft set exit = %d, want %d", code, exitConflict)
	}

	out, _, code := run(t, "draft", "show", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("draft show exit = %d", code)
	}
	if out != document {
		t.Fatalf("failed compare-and-swap changed the draft:\n got %q\nwant %q", out, document)
	}
}

// TestDraftSetRequiresOneOfRevisionOrForce pins that neither a missing nor a
// doubled guard is accepted: both are usage errors, exit 2.
func TestDraftSetRequiresOneOfRevisionOrForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.json")
	writeFile(t, path, document)

	for _, args := range [][]string{
		{"draft", "set", "--file", path, "--data-dir", dir},
		{"draft", "set", "--file", path, "--if-revision", "0", "--force", "--data-dir", dir},
	} {
		if _, stderr, code := run(t, args...); code != exitUsage {
			t.Fatalf("%v exit = %d, want %d (stderr %q)", args, code, exitUsage, stderr)
		}
	}
}

// TestDraftSetRejectsBadEnvelope proves validation happens before the write.
func TestDraftSetRejectsBadEnvelope(t *testing.T) {
	dir := t.TempDir()

	for name, body := range map[string]string{
		"empty":         "",
		"not json":      "nope",
		"array":         `[1,2,3]`,
		"wrong format":  `{"format":"other","format_version":1,"body":{}}`,
		"wrong version": `{"format":"nfsense.draft","format_version":2,"body":{}}`,
		"no body":       `{"format":"nfsense.draft","format_version":1}`,
		"trailing data": `{"format":"nfsense.draft","format_version":1,"body":{}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "draft.json")
			writeFile(t, path, body)
			if _, _, code := run(t, "draft", "set", "--file", path, "--if-revision", "0", "--data-dir", dir); code != exitGeneral {
				t.Fatalf("exit = %d, want %d", code, exitGeneral)
			}
		})
	}
}

// TestStoreRejectsNewerSchema lives in internal/store: see store_test.go.

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err != nil {
		return out.String(), errOut.String() + "nfsense: " + err.Error() + "\n", exitCode(err)
	}
	return out.String(), errOut.String(), exitOK
}
