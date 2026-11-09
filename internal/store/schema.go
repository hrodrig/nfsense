package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SupportedStoreFormatVersion is the newest persisted store format this
// binary understands. A database declaring a higher version is refused rather
// than migrated in place, because a newer binary may have written columns this
// one would silently drop on the next write.
const SupportedStoreFormatVersion = 1

// checkSchema runs before the migrator so a database produced by a newer
// nfsense is reported as ErrSchemaTooNew instead of being handed to goose,
// which knows nothing about future migrations. A database with no schema_meta
// table at all is fresh and is not an error.
func (s *Store) checkSchema(ctx context.Context) error {
	var raw string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM schema_meta WHERE key = 'store_format_version'`).Scan(&raw)
	if err != nil {
		switch {
		case isNoSuchTable(err):
			return nil
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("%w: schema_meta has no store_format_version row", ErrCorruptDB)
		case isNotADatabase(err):
			return fmt.Errorf("%w: %s", ErrCorruptDB, s.path)
		default:
			return fmt.Errorf("read store format version: %w", err)
		}
	}
	v, convErr := strconv.Atoi(strings.TrimSpace(raw))
	if convErr != nil {
		return fmt.Errorf("%w: store_format_version=%q is not a number", ErrCorruptDB, raw)
	}
	if v > SupportedStoreFormatVersion {
		return fmt.Errorf("%w: store format %d, this binary supports %d",
			ErrSchemaTooNew, v, SupportedStoreFormatVersion)
	}
	return nil
}

func isNoSuchTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

func isNotADatabase(err error) bool {
	return err != nil && strings.Contains(err.Error(), "file is not a database")
}
