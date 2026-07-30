// Package migrations embeds the SQL migration files into the binary so the
// deployed image is self-contained: no migration CLI, no volume mount, and no
// risk of the container running SQL from a different build than its code.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
