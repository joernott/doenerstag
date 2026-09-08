// Package doenerstag is the module root. It exists to hold the embedded
// assets, because a go:embed pattern cannot reach outside its own package
// directory and both migrations/ and static/ are subdirectories of the module
// root rather than of any package under internal/.
//
// The static asset embedding joins this file in task 4.6.
package doenerstag

import "embed"

// MigrationsFS holds the golang-migrate SQL, including the seed data.
//
// These are embedded unconditionally, unlike the frontend assets: an
// installation must be able to migrate its own database without a directory of
// SQL files shipped alongside the binary, and there is no development workflow
// that benefits from editing them without a rebuild.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationsDir is the path MigrationsFS is rooted at.
const MigrationsDir = "migrations"
