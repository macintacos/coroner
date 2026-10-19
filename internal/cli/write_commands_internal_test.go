package cli

import (
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

// writeVerbs are the verbs in the command checklist's verb glossary that change the database.
func writeVerbs() []string {
	return []string{"create", "add", "edit", "delete", "use", "link", "unlink", "import"}
}

// walkCommands calls visit on cmd and every command below it.
func walkCommands(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, child := range cmd.Commands() {
		walkCommands(child, visit)
	}
}

var _ = Describe("write commands", func() {
	It("each take --dry-run and print JSON, so a script can preview any write and read what it wrote", func(ctx SpecContext) {
		var problems []string
		walkCommands(NewRoot(ctx, App{}), func(cmd *cobra.Command) {
			if !slices.Contains(writeVerbs(), cmd.Name()) {
				return
			}
			if cmd.LocalFlags().Lookup("dry-run") == nil {
				problems = append(problems, cmd.CommandPath()+" has no --dry-run")
			}
			if !slices.Contains(strings.Split(cmd.Annotations[formatsAnnotation], ","), string(formatJSON)) {
				problems = append(problems, cmd.CommandPath()+" cannot print --json")
			}
		})

		Expect(problems).To(BeEmpty())
	})
})
