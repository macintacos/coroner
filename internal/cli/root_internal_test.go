package cli

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

type hookFunc = func(*cobra.Command, []string) error

var _ = Describe("wrapRunErrors", func() {
	newCommand := func() *cobra.Command {
		cmd := &cobra.Command{Use: "throwaway"}
		cmd.Flags().AddFlagSet(globalFlags())
		return cmd
	}

	DescribeTable("makes an untyped error from a hook or body an internal *Error",
		func(hook func(*cobra.Command) *hookFunc) {
			cmd := newCommand()
			*hook(cmd) = func(*cobra.Command, []string) error { return errors.New("disk on fire") }

			wrapRunErrors(cmd)

			Expect((*hook(cmd))(cmd, nil)).To(SatisfyAll(
				HaveField("Kind", KindInternal), HaveField("Message", "disk on fire")))
		},
		Entry("PersistentPreRunE", func(cmd *cobra.Command) *hookFunc { return &cmd.PersistentPreRunE }),
		Entry("PreRunE", func(cmd *cobra.Command) *hookFunc { return &cmd.PreRunE }),
		Entry("RunE", func(cmd *cobra.Command) *hookFunc { return &cmd.RunE }),
		Entry("PostRunE", func(cmd *cobra.Command) *hookFunc { return &cmd.PostRunE }),
		Entry("PersistentPostRunE", func(cmd *cobra.Command) *hookFunc { return &cmd.PersistentPostRunE }),
	)

	It("recovers a panic into an internal *Error", func() {
		cmd := newCommand()
		cmd.RunE = func(*cobra.Command, []string) error { panic("nil map") }

		wrapRunErrors(cmd)

		Expect(cmd.RunE(cmd, nil)).To(SatisfyAll(
			HaveField("Kind", KindInternal), HaveField("Message", ContainSubstring("nil map"))))
	})
})

var _ = It("gives every command an example", func(ctx SpecContext) {
	var missing []string
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if !cmd.Hidden && cmd.Example == "" {
			missing = append(missing, cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}

	visit(NewRoot(ctx, App{}))

	Expect(missing).To(BeEmpty())
})

var _ = Describe("asInternal", func() {
	It("makes a kindless *Error internal, keeping its message", func() {
		Expect(asInternal(&Error{Message: "oops"})).To(SatisfyAll(
			HaveField("Kind", KindInternal), HaveField("Message", "oops"), HaveField("Hint", Not(BeEmpty()))))
	})
})
