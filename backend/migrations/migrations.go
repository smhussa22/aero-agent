// Package migrations embeds the SQL files applied by cmd/migrate, in filename order.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
