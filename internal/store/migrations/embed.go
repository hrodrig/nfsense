// Package migrations embeds the SQL schema migrations so the schema ships
// inside the binary and can never drift from the code that needs it.
package migrations

import "embed"

// FS holds every migration applied by the store, named
// <version>_<description>.sql and driven by goose.
//
//go:embed *.sql
var FS embed.FS
