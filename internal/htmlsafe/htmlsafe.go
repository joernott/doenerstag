// Package htmlsafe sanitises the HTML snippets an operator supplies.
//
// The imprint and the legal notes are the only HTML this application stores
// that it did not write itself, and both arrive from an administrator: through
// the installer at setup, and through the API afterwards. One policy serves
// both, because two policies would eventually differ and the weaker one would
// be the way in.
package htmlsafe

import "github.com/microcosm-cc/bluemonday"

// policy is built once: compiling it is not free and it is immutable.
var policy = func() *bluemonday.Policy {
	// UGC is the right starting point: headings, lists, links and emphasis, but
	// no script, no event handlers and no javascript: URLs.
	p := bluemonday.UGCPolicy()
	// Operators style their own imprint, and these are inert.
	p.AllowAttrs("id", "class").Globally()
	p.AllowElements("section", "article", "header", "footer", "address", "hr")
	p.AllowTables()
	return p
}()

// Sanitise strips anything an administrator-supplied snippet has no business
// carrying.
//
// The snippet is a fragment rather than a document: `html`, `head` and `body`
// are removed along with everything else outside the allow-list, so what is
// stored can be dropped into a page as it is.
func Sanitise(html string) string {
	return policy.Sanitize(html)
}
