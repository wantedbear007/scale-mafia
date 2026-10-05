// Package migrations embeds the SQL migration files so that the compiled
// binary can migrate an empty database without needing the source tree.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
