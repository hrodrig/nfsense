// Package draftdoc defines the JSON envelope of the intended-state document.
// It is pure: no database, no filesystem, no firewall. The envelope is the
// part of the store that is expensive to change later, so `format` lives only
// inside the JSON and never in a column (D-10).
package draftdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	// FormatName identifies the document shape. Changing it is a data
	// migration for every stored draft.
	FormatName = "nfsense.draft"

	// FormatVersion is the version of the envelope contract; it is bumped
	// only when the meaning of body changes incompatibly.
	FormatVersion = 1

	// MaxBytes caps the document in bytes, matching the
	// length(CAST(document AS BLOB)) guard the schema uses (S10).
	MaxBytes = 1 << 20
)

// canonicalEmpty is the exact bytes Empty returns. It is a literal rather
// than a re-marshal so the first stored draft is byte-stable forever.
const canonicalEmpty = `{"format":"nfsense.draft","format_version":1,"body":{}}`

// Envelope is the decoded view of a document. Body stays as raw bytes so
// unknown keys nested inside it are never re-serialized.
type Envelope struct {
	Format        string          `json:"format"`
	FormatVersion int             `json:"format_version"`
	Body          json.RawMessage `json:"body"`
}

// Empty returns the canonical empty draft: format set, version 1, empty body.
func Empty() []byte { return []byte(canonicalEmpty) }

// Validate rejects anything that is not a well-formed envelope. It runs
// before any SQL so a malformed document never reaches the database.
//
// Rejected: empty input, input over MaxBytes, invalid JSON, a top-level value
// that is not an object, trailing data after the object, a wrong or missing
// format, a format_version other than 1, and a missing or null body.
func Validate(raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("draft document is empty")
	}
	if len(raw) > MaxBytes {
		return fmt.Errorf("draft document is %d bytes, limit is %d", len(raw), MaxBytes)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	var shape any
	if err := dec.Decode(&shape); err != nil {
		return fmt.Errorf("draft document is not valid JSON: %w", err)
	}
	if _, ok := shape.(map[string]any); !ok {
		return errors.New("draft document must be a JSON object")
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("draft document has trailing data after the top-level object")
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("draft document envelope: %w", err)
	}
	if env.Format != FormatName {
		return fmt.Errorf("draft document format is %q, want %q", env.Format, FormatName)
	}
	if env.FormatVersion != FormatVersion {
		return fmt.Errorf("draft document format_version is %d, want %d",
			env.FormatVersion, FormatVersion)
	}
	if len(env.Body) == 0 || bytes.Equal(bytes.TrimSpace(env.Body), []byte("null")) {
		return errors.New("draft document is missing body")
	}
	return nil
}

// Parse validates then returns the decoded envelope. The body is handed back
// exactly as it appeared in raw.
func Parse(raw []byte) (Envelope, error) {
	if err := Validate(raw); err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("draft document envelope: %w", err)
	}
	return env, nil
}
