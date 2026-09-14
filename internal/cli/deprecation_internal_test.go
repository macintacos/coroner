package cli

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var _ = Describe("deprecation", func() {
	valid := deprecation{replacement: "use --new instead", removalVersion: "v0.9.0", removalDate: "2027-03-14"}

	// execute runs root with args and returns what cobra printed through OutOrStderr, which a
	// spec may point at a buffer; coroner's own root never sets Out.
	execute := func(root *cobra.Command, args ...string) string {
		var printed bytes.Buffer
		root.SetOut(&printed)
		root.SetArgs(args)
		Expect(root.Execute()).To(Succeed())
		return printed.String()
	}

	It("makes a deprecated flag warn with its replacement, earliest removal version and date", func() {
		root := &cobra.Command{Use: "coroner", Run: func(*cobra.Command, []string) {}}
		root.Flags().Bool("old", false, "")

		Expect(deprecateFlag(root.Flags(), "old", valid)).To(Succeed())

		Expect(execute(root, "--old")).To(Equal("Flag --old has been deprecated, use --new instead; " +
			"it may be removed in v0.9.0 or later, and no earlier than 2027-03-14\n"))
	})

	It("makes a deprecated command warn with its replacement, earliest removal version and date", func() {
		root := &cobra.Command{Use: "coroner"}
		old := &cobra.Command{Use: "old", Run: func(*cobra.Command, []string) {}}
		root.AddCommand(old)

		Expect(deprecateCommand(old, valid)).To(Succeed())

		Expect(execute(root, "old")).To(Equal(`Command "old" is deprecated, use --new instead; ` +
			"it may be removed in v0.9.0 or later, and no earlier than 2027-03-14\n"))
	})

	DescribeTable("rejects a deprecation that does not name a replacement, a removal version and a removal date",
		func(d deprecation, reason string) {
			flags := pflag.NewFlagSet("throwaway", pflag.ContinueOnError)
			flags.Bool("old", false, "")
			old := &cobra.Command{Use: "old"}

			Expect(deprecateFlag(flags, "old", d)).To(MatchError(ContainSubstring(reason)))
			Expect(deprecateCommand(old, d)).To(MatchError(ContainSubstring(reason)))
			Expect(flags.Lookup("old").Deprecated).To(BeEmpty())
			Expect(old.Deprecated).To(BeEmpty())
		},
		Entry("no replacement",
			deprecation{removalVersion: "v0.9.0", removalDate: "2027-03-14"}, "replacement"),
		Entry("no removal version",
			deprecation{replacement: "use --new instead", removalDate: "2027-03-14"}, "removal version"),
		Entry("a removal version without its v",
			deprecation{replacement: "use --new instead", removalVersion: "0.9.0", removalDate: "2027-03-14"}, "removal version"),
		Entry("a removal version without its patch",
			deprecation{replacement: "use --new instead", removalVersion: "v0.9", removalDate: "2027-03-14"}, "removal version"),
		Entry("no removal date",
			deprecation{replacement: "use --new instead", removalVersion: "v0.9.0"}, "removal date"),
		Entry("a removal date that is not YYYY-MM-DD",
			deprecation{replacement: "use --new instead", removalVersion: "v0.9.0", removalDate: "2027-3-14"}, "removal date"),
		Entry("a removal date that is not a date",
			deprecation{replacement: "use --new instead", removalVersion: "v0.9.0", removalDate: "next spring"}, "removal date"),
	)
})
