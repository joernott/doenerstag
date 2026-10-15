package testdb

import (
	"context"
	"testing"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
)

// DropProvisioned removes what an installer test created: the database and the
// two roles a Provisioner makes. The install and update tests each kept a copy
// (20.23).
//
// Best effort, at the end of a test: a failure is logged rather than failing a
// test that has already passed.
func DropProvisioned(t *testing.T, p *install.Provisioner) {
	t.Helper()

	ctx := context.Background()
	opts := p.Server.WithUser(p.Root.User, p.Root.Password).WithDatabase(install.MaintenanceDatabase)
	pool, err := db.Connect(ctx, opts, nil)
	if err != nil {
		t.Logf("cleaning up: %v", err)
		return
	}
	defer pool.Close()

	for _, statement := range []string{
		`DROP DATABASE IF EXISTS "` + p.Database + `" WITH (FORCE)`,
		`DROP ROLE IF EXISTS "` + p.Runtime.User + `"`,
		`DROP ROLE IF EXISTS "` + p.Admin.User + `"`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Logf("cleaning up: %v", err)
		}
	}
}
