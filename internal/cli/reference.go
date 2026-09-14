package cli

import (
	"io"
	"text/template"

	"github.com/spf13/cobra"
)

// referenceTopic is the help topic whose body is writeReference's page rather than a file
// under topics/.
const referenceTopic = "reference"

// commandTopics are the topics help lists: the files, then the reference, whose Body is empty
// because help renders it from the command tree.
func commandTopics() []HelpTopic {
	return append(HelpTopics(), HelpTopic{Name: referenceTopic, Short: "Every command and flag on one Markdown page"})
}

// referenceTemplate renders one command's section of `coroner help reference`, from the same
// metadata and flag sets as helpTemplate. Fences are tildes so the template stays a raw string.
const referenceTemplate = `{{if .HasParent}}##{{else}}#{{end}} {{.CommandPath}}

~~~text
{{trimRight (or .Long .Short)}}
~~~

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
{{- if not .HasParent}}{{with helpTopics}}

Help topics:

~~~text
{{- range .}}
{{rpad .Name $pad}}  {{.Short}}
{{- end}}
~~~
{{- end}}{{end}}
{{- if .HasExample}}

Examples:

~~~console
{{.Example}}
~~~
{{- end}}
{{- $flags := ownFlags .}}{{$global := inheritedFlags .}}
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

// writeReference writes root's command tree to w as one Markdown page, with topics listed in
// root's section: a section for root and for each available command under it, depth-first.
// help and the completion shells are only lines in their parent's section.
func writeReference(w io.Writer, root *cobra.Command, topics []HelpTopic) error {
	section := template.Must(template.New("reference").Funcs(helpFuncs(topics)).Parse(referenceTemplate))

	return writeSections(w, section, root)
}

func writeSections(w io.Writer, section *template.Template, cmd *cobra.Command) error {
	// Merges the inherited flags, which UseLine needs to show [flags].
	cmd.InitDefaultHelpFlag()

	err := section.Execute(w, cmd)
	// The shells' sections would be four near-identical install scripts' help.
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
