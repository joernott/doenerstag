//go:build !embedstatic

package doenerstag

import "embed"

// StaticFS is empty in a development build. The assets come from --static-dir
// instead; see embed.go for the release counterpart.
var StaticFS embed.FS

// Embedded reports whether the frontend is compiled in.
const Embedded = false
