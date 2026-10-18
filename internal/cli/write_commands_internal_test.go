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

var _ = Describe("write commands", func() {
	It("each take --dry-run and print JSON, so a script can preview any write and read what it wrote", func(ctx SpecContext) {
		var problems []string
		var walk func(cmd *cobra.Command)
		walk = func(cmd *cobra.Command) {
			if slices.Contains(writeVerbs(), cmd.Name()) {
				if cmd.LocalFlags().Lookup("dry-run") == nil {
					problems = append(problems, cmd.CommandPath()+" has no --dry-run")
				}
				if !slices.Contains(strings.Split(cmd.Annotations[formatsAnnotation], ","), string(formatJSON)) {
					problems = append(problems, cmd.CommandPath()+" cannot print --json")
				}
			}
			for _, child := range cmd.Commands() {
				walk(child)
			}
		}
		walk(NewRoot(ctx, App{}))

		Expect(problems).To(BeEmpty())
	})
})
