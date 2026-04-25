package pingit

import "embed"

//go:embed all:web/build
var WebFS embed.FS

//go:embed migrations/*.sql
var MigrationsFS embed.FS
