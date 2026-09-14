package cli

import (
	"io"
	"text/template"

	"github.com/spf13/cobra"
)

// referenceTopic is the help topic whose body is writeReference's page rather than a file
// under topics/.
const referenceTopic = "reference"

// referenceTemplate renders one command's section of `coroner help reference`, from the same
// metadata and flag sets as helpTemplate. Fences are tildes so the template stays a raw string.
const referenceTemplate = `{{if .HasParent}}##{{else}}#{{end}} {{.CommandPath}}

{{trimRight (or .Long .Short)}}

~~~text
{{.UseLine}}
{{- if and .HasParent .HasAvailableSubCommands}}
{{.CommandPath}} <command> [flags]
{{- end}}
~~~
{{- $pad := namePadding .}}
{{- if .HasAvailableSubCommands}}

Commands:

~~~text
{{- range .Commands}}{{if listed .}}
{{rpad .Name $pad}}  {{.Short}}
{{- end}}{{end}}
~~~
{{- end}}
{{- if .HasExample}}

Examples:

~~~console
{{.Example}}
~~~
{{- end}}
{{- $flags := .LocalFlags}}{{$global := .InheritedFlags}}
{{- if not .HasParent}}{{$flags = .LocalNonPersistentFlags}}{{$global = .PersistentFlags}}{{end}}
{{- if $flags.HasAvailableFlags}}

Flags:

~~~text
{{trimRight $flags.FlagUsages}}
~~~
{{- end}}
{{- if $global.HasAvailableFlags}}

Global flags:

~~~text
{{trimRight $global.FlagUsages}}
~~~
{{- end}}
`

// writeReference writes root's command tree to w as one Markdown page: a section for root and
// for each command help lists under it, depth-first. The completion shells, like help, are
// only lines in their parent's section.
func writeReference(w io.Writer, root *cobra.Command) error {
	section := template.Must(template.New("reference").Funcs(helpFuncs(nil)).Parse(referenceTemplate))

	return writeSections(w, section, root)
}

func writeSections(w io.Writer, section *template.Template, cmd *cobra.Command) error {
	// Merges the inherited flags, which UseLine needs to show [flags].
	cmd.InitDefaultHelpFlag()

	err := section.Execute(w, cmd)
	if err != nil || cmd.Name() == "completion" {
		return err
	}

	for _, child := range cmd.Commands() {
		if !child.IsAvailableCommand() {
			continue
		}

		_, err = io.WriteString(w, "\n")
		if err == nil {
			err = writeSections(w, section, child)
		}
		if err != nil {
			return err
		}
	}

	return nil
}
