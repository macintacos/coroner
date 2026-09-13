package cli

import (
	"fmt"
	"strings"
	"text/template"
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
{{- if .HasHelpSubCommands}}

HELP TOPICS
{{- range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .Name .NamePadding}}  {{.Short}}
{{- end}}{{end}}
{{- end}}
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

func newHelpTemplate() *template.Template {
	// Local funcs: cobra's own register through the package-global cobra.AddTemplateFunc.
	return template.Must(template.New("help").Funcs(template.FuncMap{
		"rpad": func(s string, width int) string {
			return fmt.Sprintf("%-*s", width, s)
		},
		"trimRight": func(s string) string {
			return strings.TrimRight(s, " \n")
		},
	}).Parse(helpTemplate))
}
