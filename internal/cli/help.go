package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

// helpTemplate renders help for any command. Root's persistent flags are its GLOBAL FLAGS;
// a subcommand's are the ones it inherits.
const helpTemplate = `{{trimRight (or .Long .Short)}}

USAGE
  {{.UseLine}}
{{- if and .HasParent .HasAvailableSubCommands}}
  {{.CommandPath}} <command> [flags]
{{- end}}
{{- /* IsAvailableCommand is false for cobra's help command, so it is named explicitly. */}}
{{- range $group := .Groups}}

{{$group.Title}}
{{- range $.Commands}}{{if and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding}}  {{.Short}}
{{- end}}{{end}}
{{- end}}
{{- if not .AllChildCommandsHaveGroup}}

COMMANDS
{{- range .Commands}}{{if and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding}}  {{.Short}}
{{- end}}{{end}}
{{- end}}
{{- if not .HasParent}}{{with helpTopics}}
{{- $pad := 0}}{{range $.Commands}}{{$pad = .NamePadding}}{{end}}

HELP TOPICS
{{- range .}}
  {{rpad .Name $pad}}  {{.Short}}
{{- end}}
{{- end}}{{end}}
{{- if .HasExample}}

EXAMPLES
{{.Example}}
{{- end}}
{{- $flags := .LocalFlags}}{{$global := .InheritedFlags}}
{{- if not .HasParent}}{{$flags = .LocalNonPersistentFlags}}{{$global = .PersistentFlags}}{{end}}
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
	// Local funcs: cobra's own register through the package-global cobra.AddTemplateFunc.
	return template.Must(template.New("help").Funcs(template.FuncMap{
		"helpTopics": func() []HelpTopic {
			return topics
		},
		"rpad": func(s string, width int) string {
			return fmt.Sprintf("%-*s", width, s)
		},
		"trimRight": func(s string) string {
			return strings.TrimRight(s, " \n")
		},
	}).Parse(helpTemplate))
}

// addHelpCommand adds the help command, which prints a help topic, or else a command's
// help. Topics are not commands, so a topic's name never becomes a runnable one.
func addHelpCommand(root *cobra.Command, app App, topics []HelpTopic) {
	root.SetHelpCommand(&cobra.Command{
		Use:     "help [<command> | <topic>]",
		Short:   "Help about any command or topic",
		GroupID: additionalCommands,
		Example: "  $ coroner help\n  $ coroner help version\n  $ coroner help time",
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			var completions []cobra.Completion
			for _, name := range helpNames(root, args, topics) {
				if strings.HasPrefix(name.name, toComplete) {
					completions = append(completions, cobra.CompletionWithDesc(name.name, name.short))
				}
			}

			return completions, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				if i := slices.IndexFunc(topics, func(topic HelpTopic) bool { return topic.Name == args[0] }); i >= 0 {
					_, err := io.WriteString(app.Out, topics[i].Body)
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
		},
	})
	root.InitDefaultHelpCmd()
}

type helpName struct {
	name, short string
}

// helpNames lists what `help <args...> <name>` accepts: the subcommands of the command args
// name, and at the top level the topics first.
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
		// IsAvailableCommand is false for cobra's help command, so it is named explicitly.
		if cmd.IsAvailableCommand() || cmd.Name() == "help" {
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

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("unknown help topic or command %q", name),
		Hint:    fmt.Sprintf("Did you mean '%s help %s'?", root.Name(), closest(name, candidates)),
		Err:     err,
	}
}
