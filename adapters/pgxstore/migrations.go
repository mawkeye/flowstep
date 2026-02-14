package pgxstore

import "embed"

// Migrations contains the SQL migration files for flowstate.
//
//go:embed migrations/*.sql
var Migrations embed.FS
