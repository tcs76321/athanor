// Package migrations embeds the forward-only schema migrations applied
// by internal/store.Migrate (ARCHITECTURE §23.5).
package migrations

import "embed"

// FS holds all NNNN_description.sql migration files.
//
//go:embed *.sql
var FS embed.FS
