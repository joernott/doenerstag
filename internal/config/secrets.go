package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/pflag"
)

// SecretOnCommandLineError reports that a secret was passed as a command line
// flag. It is fatal: the application refuses to start, and the value is never
// read, logged or written anywhere.
//
// The reason is that a command line is visible to every user on the machine
// through the process list, and ends up in shell history. A password that has
// been on a command line should be considered disclosed.
type SecretOnCommandLineError struct {
	// Flags are the offending flag names, without the leading dashes.
	Flags []string
}

func (e *SecretOnCommandLineError) Error() string {
	var b strings.Builder

	if len(e.Flags) == 1 {
		fmt.Fprintf(&b, "--%s must not be passed on the command line", e.Flags[0])
	} else {
		names := make([]string, len(e.Flags))
		for i, flag := range e.Flags {
			names[i] = "--" + flag
		}
		fmt.Fprintf(&b, "%s must not be passed on the command line",
			strings.Join(names, ", "))
	}

	b.WriteString(": a command line is visible to every user on the machine " +
		"and is recorded in shell history")

	// Say where the value does belong, so the message is actionable.
	b.WriteString(".\nSupply it ")
	b.WriteString(joinWithOr(alternativeSources(e.Flags)))
	b.WriteString(" instead.")

	return b.String()
}

// joinWithOr renders a list as prose: "a", "a or b", "a, b or c".
func joinWithOr(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
	}
}

// alternativeSources describes where each offending secret may legitimately
// come from. The privileged installer passwords are never stored in a file, so
// their advice differs from the runtime settings'.
func alternativeSources(flags []string) []string {
	viaFile := false
	for _, flag := range flags {
		if setting, ok := SettingByFlag(flag); ok && setting.InConfigFile() {
			viaFile = true
		}
	}

	sources := make([]string, 0, 3)
	if viaFile {
		sources = append(sources, "in the configuration file")
	}
	sources = append(sources, "in the matching "+EnvPrefix+"_* environment variable")
	if !viaFile {
		sources = append(sources, "at the interactive prompt")
	}
	return sources
}

// CheckCommandLineSecrets reports an error when any setting marked Secret was
// changed on the command line.
//
// The secret flags are registered but hidden, precisely so that this check can
// see them and explain itself. Leaving them unregistered would make pflag
// reject the flag first, with an "unknown flag" message that tells the operator
// nothing about why it is refused.
func CheckCommandLineSecrets(flags *pflag.FlagSet) error {
	var offending []string

	for _, setting := range SecretSettings() {
		flag := flags.Lookup(setting.Flag)
		if flag != nil && flag.Changed {
			offending = append(offending, setting.Flag)
		}
	}

	if len(offending) == 0 {
		return nil
	}

	// Stable order, so the message does not depend on registry order.
	sort.Strings(offending)
	return &SecretOnCommandLineError{Flags: offending}
}
