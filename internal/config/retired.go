package config

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RetiredSetting is a key that used to exist and no longer does.
//
// `update` rewrites the configuration file from the settings this binary knows
// about, which would silently drop anything a previous version wrote. Deleting
// an operator's line without a word is the wrong answer twice over: it destroys
// information, and it hides the fact that a setting has gone. So the old line is
// kept, commented out, with a note saying why.
type RetiredSetting struct {
	// Key is the dotted key as it appeared in the file: "server.old_thing".
	Key string
	// Value is the text that followed the colon, verbatim.
	Value string
}

// RenderOptions is what RenderFile needs beyond the values themselves.
type RenderOptions struct {
	// GeneratedAt is stamped into the header.
	GeneratedAt time.Time

	// Verb is the command that wrote the file: "install" or "update". It
	// appears in the header, so an operator can tell how the file came to be.
	Verb string

	// Retired are the keys found in the old file that this binary no longer
	// knows about. They are written back as comments.
	Retired []RetiredSetting
}

/*
RetiredIn finds the keys an existing configuration file has that this binary
does not know about.

The file this parses is one this package wrote, so the shape is known: a
section at column zero, its settings indented under it. That is enough to
recognise a key, and recognising keys is all this needs to do -- it is not a
YAML parser and does not pretend to be one. A file an operator has restructured
by hand may confuse it, and the consequence is a spurious "retired" comment
rather than a lost setting.
*/
func RetiredIn(existing string) []RetiredSetting {
	known := make(map[string]bool, len(Settings))
	for _, setting := range Settings {
		if setting.InConfigFile() {
			known[setting.Key] = true
		}
	}

	var (
		retired []RetiredSetting
		section string
	)

	for _, line := range strings.Split(existing, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if trimmed == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
			continue
		}

		// A section header sits at column zero and ends in a colon.
		if !strings.HasPrefix(trimmed, " ") && strings.HasSuffix(strings.TrimSpace(trimmed), ":") {
			section = strings.TrimSuffix(strings.TrimSpace(trimmed), ":")
			continue
		}

		if section == "" || !strings.HasPrefix(trimmed, " ") {
			continue
		}

		name, value, found := strings.Cut(strings.TrimSpace(trimmed), ":")
		if !found {
			continue
		}

		key := section + "." + strings.TrimSpace(name)
		if known[key] {
			continue
		}
		retired = append(retired, RetiredSetting{Key: key, Value: strings.TrimSpace(value)})
	}

	sort.Slice(retired, func(i, j int) bool { return retired[i].Key < retired[j].Key })
	return retired
}

// renderRetired writes the retired keys as a commented block.
func renderRetired(b *strings.Builder, retired []RetiredSetting) {
	if len(retired) == 0 {
		return
	}

	b.WriteString("\n# Settings this version no longer has.\n")
	b.WriteString("#\n")
	b.WriteString("# They were in the file this one replaced. They are kept here, commented\n")
	b.WriteString("# out, so that nothing an operator set is lost without trace -- but they\n")
	b.WriteString("# do nothing, and removing them is safe.\n")

	for _, setting := range retired {
		fmt.Fprintf(b, "# %s: %s\n", setting.Key, setting.Value)
	}
}
