package config

import (
	"fmt"

	"github.com/spf13/pflag"
)

// RegisterGlobalFlags adds the settings every verb understands, plus --config.
// They are persistent flags on the root command.
func RegisterGlobalFlags(flags *pflag.FlagSet) {
	flags.StringP(ConfigFlag, "c", DefaultConfigFile,
		"path to the configuration file")
	registerScope(flags, ScopeGlobal)
}

// RegisterScopeFlags adds the settings a single verb understands.
func RegisterScopeFlags(flags *pflag.FlagSet, scope Scope) {
	registerScope(flags, scope)
}

func registerScope(flags *pflag.FlagSet, scope Scope) {
	for _, setting := range SettingsInScope(scope) {
		registerSetting(flags, setting)
	}
}

// registerSetting adds one setting as a flag.
//
// Secret settings are registered too, but hidden. Registering them is what
// allows the check in secrets.go to report the documented FATAL security error
// when one appears on the command line; without the flag, pflag would reject it
// first with an unhelpful "unknown flag" and the operator would never learn why
// it is refused.
func registerSetting(flags *pflag.FlagSet, s Setting) {
	// Registering the same flag twice makes pflag panic. That can happen when a
	// caller asks for a scope whose settings are already present, so it is
	// tolerated rather than fatal: the first registration is authoritative.
	if flags.Lookup(s.Flag) != nil {
		return
	}

	usage := s.Usage
	if s.Secret {
		usage += " (must not be passed on the command line)"
	}

	switch s.Kind {
	case KindString, KindDuration, KindByteSize:
		// Durations and byte sizes are registered as strings so that "7d" and
		// "5MiB" reach the application's own parsers rather than pflag's.
		flags.StringP(s.Flag, s.Short, toString(s.Default), usage)
	case KindInt:
		flags.IntP(s.Flag, s.Short, toInt(s.Default), usage)
	case KindBool:
		flags.BoolP(s.Flag, s.Short, toBool(s.Default), usage)
	}

	if s.Secret {
		// Hidden rather than absent: it is still parsed, so the security check
		// can see it and explain itself.
		if err := flags.MarkHidden(s.Flag); err != nil {
			panic(fmt.Sprintf("marking %s hidden: %v", s.Flag, err))
		}
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toInt(v any) int {
	if i, ok := v.(int); ok {
		return i
	}
	return 0
}

func toBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
