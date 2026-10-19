package cli

import (
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// helpTemplate renders help for any command.
const helpTemplate = `{{trimRight (or .Long .Short)}}

USAGE
  {{.UseLine}}
{{- if and .HasParent .HasAvailableSubCommands}}
  {{.CommandPath}} <command> [flags]
{{- end}}
{{- $pad := namePadding .}}
{{- range $group := .Groups}}

{{$group.Title}}
{{- range $.Commands}}{{if and (eq .GroupID $group.ID) (listed .)}}
  {{rpad .Name $pad}}  {{.Short}}
{{- end}}{{end}}
{{- end}}
{{- if not .AllChildCommandsHaveGroup}}

COMMANDS
{{- range .Commands}}{{if and (eq .GroupID "") (listed .)}}
  {{rpad .Name $pad}}  {{.Short}}
{{- end}}{{end}}
{{- end}}
{{- if not .HasParent}}{{with helpTopics}}

HELP TOPICS
{{- range .}}
  {{rpad .Name $pad}}  {{.Short}}
{{- end}}
{{- end}}{{end}}
{{- if .HasExample}}

EXAMPLES
{{.Example}}
{{- end}}
{{- $flags := ownFlags .}}{{$global := inheritedFlags .}}
{{- if $flags.HasAvailableFlags}}

FLAGS
{{trimRight $flags.FlagUsages}}
{{- end}}
{{- if $global.HasAvailableFlags}}

GLOBAL FLAGS
{{trimRight $global.FlagUsages}}
{{- end}}

LEARN MORE
  Use ` + "`coroner <command> --help`" + ` for more information about a command.
  Report bugs and send feedback at ` + issuesURL + `
`

func newHelpTemplate(topics []HelpTopic) *template.Template {
	return template.Must(template.New("help").Funcs(helpFuncs(topics)).Parse(helpTemplate))
}

// helpFuncs are the functions help's and the reference's templates call; topics pad root's name
// column and fill HELP TOPICS.
func helpFuncs(topics []HelpTopic) template.FuncMap {
	// Local funcs: cobra's own register through the package-global cobra.AddTemplateFunc.
	return template.FuncMap{
		"helpTopics": func() []HelpTopic {
			return topics
		},
		"inheritedFlags": inheritedFlags,
		"listed":         listed,
		"namePadding": func(cmd *cobra.Command) int {
			return namePadding(cmd, topics)
		},
		"ownFlags": ownFlags,
		"rpad": func(s string, width int) string {
			return fmt.Sprintf("%-*s", width, s)
		},
		"trimRight": func(s string) string {
			return strings.TrimRight(s, " \n")
		},
	}
}

// ownFlags are the flags cmd's help lists under FLAGS, which on root leave out its persistent
// flags.
func ownFlags(cmd *cobra.Command) *pflag.FlagSet {
	if cmd.HasParent() {
		return cmd.LocalFlags()
	}

	return cmd.LocalNonPersistentFlags()
}

// inheritedFlags are the flags cmd's help lists under GLOBAL FLAGS: root's persistent flags, or
// those a subcommand inherits.
func inheritedFlags(cmd *cobra.Command) *pflag.FlagSet {
	if cmd.HasParent() {
		return cmd.InheritedFlags()
	}

	return cmd.PersistentFlags()
}

// listed reports whether help lists cmd.
func listed(cmd *cobra.Command) bool {
	// IsAvailableCommand is false for cobra's help command.
	return cmd.IsAvailableCommand() || cmd.Name() == "help"
}

// namePadding is the width of the name column in cmd's help: its subcommands' names and, at
// the top level, the topics' names.
func namePadding(cmd *cobra.Command, topics []HelpTopic) int {
	width := 0
	for _, sub := range cmd.Commands() {
		width = max(width, sub.NamePadding())
	}

	if !cmd.HasParent() {
		for _, topic := range topics {
			width = max(width, len(topic.Name))
		}
	}

	return width
}

// addHelpCommand adds the help command, which prints a help topic, or else a command's
// help. Topics are not commands, so a topic's name never becomes a runnable one.
func addHelpCommand(root *cobra.Command, app App, topics []HelpTopic) {
	root.SetHelpCommand(&cobra.Command{
		Use:     "help [<command> | <topic>]",
		Short:   "Help about any command or topic",
		GroupID: additionalCommands,
		Example: "  $ coroner help\n  $ coroner help version\n  $ coroner help time\n  $ coroner help reference",
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			var completions []cobra.Completion
			for _, candidate := range helpNames(root, args, topics) {
				if strings.HasPrefix(candidate.name, toComplete) {
					completions = append(completions, cobra.CompletionWithDesc(candidate.name, candidate.short))
				}
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: typedHook(func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				if args[0] == referenceTopic {
					return writeReference(app.Out, root, topics)
				}

				if topic, ok := topicNamed(topics, args[0]); ok {
					_, err := io.WriteString(app.Out, topic.Body)
					return err
				}
			}

			target, _, err := root.Find(args)
			if err != nil {
				return unknownHelpError(root, args[0], topics, err)
			}

			// Merges the inherited flags, which UseLine needs to show [flags].
			target.InitDefaultHelpFlag()

			return target.Help()
		}),
	})
	root.InitDefaultHelpCmd()
}

type helpName struct {
	name, short string
}

// helpNames lists what `help <args...> <name>` accepts: the subcommands of the command args
// names, preceded at the top level by the topics.
func helpNames(root *cobra.Command, args []string, topics []HelpTopic) []helpName {
	parent, _, err := root.Find(args)
	if err != nil {
		return nil
	}

	var names []helpName
	if len(args) == 0 {
		for _, topic := range topics {
			names = append(names, helpName{topic.Name, topic.Short})
		}
	}

	for _, cmd := range parent.Commands() {
		if listed(cmd) {
			names = append(names, helpName{cmd.Name(), cmd.Short})
		}
	}

	return names
}

func unknownHelpError(root *cobra.Command, name string, topics []HelpTopic, err error) *Error {
	names := helpNames(root, nil, topics)
	candidates := make([]string, len(names))
	for i, candidate := range names {
		candidates[i] = candidate.name
	}

	hint := fmt.Sprintf("Run '%s help' to list commands and topics.", root.Name())
	if suggestion := closest(name, candidates); suggestion != "" {
		hint = fmt.Sprintf("Did you mean '%s help %s'?", root.Name(), suggestion)
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("unknown help topic or command %q", name),
		Hint:    hint,
		Err:     err,
	}
}

// closest returns the candidate the fewest single-character edits from name, the first of
// several at the same distance, or "" when none is within two, the distance cobra suggests
// commands within; names are ASCII.
func closest(name string, candidates []string) string {
	best, bestDistance := "", 3
	for _, candidate := range candidates {
		if distance := levenshtein(name, candidate); distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}

	return best
}

func levenshtein(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := range len(a) {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j := range len(b) {
			substitution := previous[j]
			if a[i] != b[j] {
				substitution++
			}

			current[j+1] = min(previous[j+1]+1, current[j]+1, substitution)
		}

		previous = current
	}

	return previous[len(b)]
}
