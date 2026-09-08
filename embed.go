//go:build embedstatic

package doenerstag

import "embed"

// StaticFS holds the built frontend, compiled into the binary.
//
// Present only in a release build, selected with -tags embedstatic. A default
// build gets the counterpart in embed_disabled.go and serves the assets from
// disk instead, so that editing a stylesheet does not mean rebuilding the
// backend. See docs/adr/0002-static-assets-embed-toggle.md.
//
//go:embed all:static
var StaticFS embed.FS

// Embedded reports whether the frontend is compiled in.
const Embedded = true
