// Package service is the operator-facing seam over the store. Every exported
// call takes an Actor so attribution cannot be forgotten at a call site, and
// imports of internal/store point this way only — the store never imports the
// service (D-02, D-13).
package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/hrodrig/nfsense/internal/store"
)

// DBName is the file name inside the data directory.
const DBName = "nfsense.db"

// Service is an open nfsense instance rooted at one data directory.
type Service struct {
	st *store.Store
}

// Open resolves <dataDir>/nfsense.db and migrates it. dataDir comes from the
// CLI precedence ladder (flag, then NFSENSE_DATA_DIR, then /var/lib/nfsense).
func Open(ctx context.Context, dataDir string) (*Service, error) {
	st, err := store.Open(ctx, filepath.Join(dataDir, DBName))
	if err != nil {
		return nil, err
	}
	return &Service{st: st}, nil
}

// Close releases the database handle.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	return s.st.Close()
}

// GetDraft reads the working draft.
func (s *Service) GetDraft(ctx context.Context, _ Actor) (store.Draft, error) {
	d, err := s.st.GetDraft(ctx)
	if err != nil {
		return store.Draft{}, fmt.Errorf("get draft: %w", err)
	}
	return d, nil
}

// SaveDraft validates and stores raw, returning the revision SQLite assigned.
// expected is the revision the caller read; a mismatch is ErrConflict.
func (s *Service) SaveDraft(ctx context.Context, a Actor, raw []byte, expected int64) (int64, error) {
	rev, err := s.st.SaveDraft(ctx, a.String(), raw, expected)
	if err != nil {
		return 0, fmt.Errorf("save draft: %w", err)
	}
	return rev, nil
}

// Health reports whether the store is answering. It says nothing about the
// firewall, which this plan does not touch.
func (s *Service) Health(ctx context.Context) error {
	return s.st.Health(ctx)
}
