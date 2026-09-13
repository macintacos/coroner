package cli

import (
	"slices"
	"strings"
)

// commandArgs returns the flags-only command line that gives in to the command at path: the
// positional argument, then each given field in definition order.
func commandArgs(path string, def definition, in fieldValues) []string {
	words := append([]string{"coroner"}, strings.Fields(path)...)
	var flags, tail []string
	for _, f := range def.fields {
		values := in[f.name]
		switch {
		case !f.arg:
			for _, value := range values {
				flags = append(flags, flagArgs(f, value)...)
			}
		case slices.ContainsFunc(values, dashed):
			// cobra would take a positional argument led by - for a flag.
			tail = append([]string{"--"}, values...)
		default:
			words = append(words, values...)
		}
	}

	return slices.Concat(words, flags, tail)
}

// flagArgs returns the args that give f's flag value, by its shorthand when it has one.
func flagArgs(f field, value string) []string {
	flag := "--" + f.name
	if f.shorthand != "" {
		flag = "-" + f.shorthand
	}

	switch {
	case f.typ == boolField && value == "true":
		return []string{flag}
	// A bool reads no separate value, a value led by - looks like a flag of its own, and pflag
	// has no -t=value for an empty value.
	case f.typ == boolField || dashed(value):
		return []string{"--" + f.name + "=" + value}
	default:
		return []string{flag, value}
	}
}

func dashed(value string) bool {
	return strings.HasPrefix(value, "-")
}

// shellLine joins args into one line a POSIX shell reads back as args.
func shellLine(args []string) string {
	const bare = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789@%+=:,./_-"
	words := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "" && strings.Trim(arg, bare) == "" {
			words = append(words, arg)
			continue
		}
		words = append(words, "'"+strings.ReplaceAll(arg, "'", `'\''`)+"'")
	}

	return strings.Join(words, " ")
}
