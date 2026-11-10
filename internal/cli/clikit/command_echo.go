package clikit

import (
	"slices"
	"strings"
)

// CommandArgs returns the flags-only command line that gives in to the command at path: the
// positional argument, then each given field in definition order.
func CommandArgs(path string, def Definition, in FieldValues) []string {
	words := append([]string{"coroner"}, strings.Fields(path)...)
	var flags, tail []string
	for _, f := range def.Fields {
		values := in[f.Name]
		switch {
		case !f.Arg:
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
func flagArgs(f Field, value string) []string {
	flag := "--" + f.Name
	if f.Shorthand != "" {
		flag = "-" + f.Shorthand
	}

	switch {
	case f.Typ == BoolField && value == "true":
		return []string{flag}
	// A bool takes no separate value, and a separate value led by - would read as a flag.
	case f.Typ == BoolField || dashed(value):
		return []string{"--" + f.Name + "=" + value}
	default:
		return []string{flag, value}
	}
}

func dashed(value string) bool {
	return strings.HasPrefix(value, "-")
}

// ShellLine joins args into one line a POSIX shell reads back as args.
// ponytail: the quoting is POSIX sh; fish reads \\ inside single quotes and zsh expands a bare
// word starting with =. Quote per shell, if pasting there matters.
func ShellLine(args []string) string {
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
