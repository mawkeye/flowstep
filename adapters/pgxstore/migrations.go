package pgxstore

import "embed"

// Migrations contains the SQL migration files for flowstep.
//
//go:embed migrations/*.sql
var Migrations embed.FS
