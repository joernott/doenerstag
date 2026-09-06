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

// OpenAPISpec holds the API description served at /api/v1/openapi.json.
//
// YAML is the source of truth because that is what a person maintains; the
// JSON the endpoint serves is converted from it at runtime, so the two cannot
// drift apart the way a checked-in generated file would.
//
//go:embed api/openapi.yaml
var OpenAPISpec embed.FS

// OpenAPIYAML returns the embedded description.
func OpenAPIYAML() ([]byte, error) {
	return OpenAPISpec.ReadFile("api/openapi.yaml")
}
