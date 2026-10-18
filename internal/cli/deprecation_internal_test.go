package cli

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var _ = Describe("deprecation", func() {
	valid := deprecation{replacement: "use --new instead", removalVersion: "v0.9.0", removalDate: "2027-03-14"}

	// warning runs root with args and returns what cobra printed through OutOrStderr, which a
	// spec may point at a buffer; coroner's own root never sets Out.
	warning := func(root *cobra.Command, args ...string) string {
		var printed bytes.Buffer
		root.SetOut(&printed)
		root.SetArgs(args)
		Expect(root.Execute()).To(Succeed())
		return printed.String()
	}

	It("hides a deprecated flag and makes it warn with its replacement, earliest removal version and date", func() {
		root := &cobra.Command{Use: "coroner", Run: func(*cobra.Command, []string) {}}
		root.Flags().Bool("old", false, "")

		deprecateFlag(root.Flags(), "old", valid)

		Expect(root.Flags().Lookup("old").Hidden).To(BeTrue())
		Expect(warning(root, "--old")).To(Equal("Flag --old has been deprecated, use --new instead; " +
			"it may be removed in v0.9.0 or later, and no earlier than 2027-03-14\n"))
	})

	It("hides a deprecated command and makes it warn with its replacement, earliest removal version and date", func() {
		root := &cobra.Command{Use: "coroner"}
		old := &cobra.Command{Use: "old", Run: func(*cobra.Command, []string) {}}
		root.AddCommand(old)

		deprecateCommand(old, valid)

		Expect(old.Hidden).To(BeTrue())
		Expect(warning(root, "old")).To(Equal(`Command "old" is deprecated, use --new instead; ` +
			"it may be removed in v0.9.0 or later, and no earlier than 2027-03-14\n"))
	})

	// onlyReason matches a message that gives reason and none of the other reasons.
	onlyReason := func(reason string) types.GomegaMatcher {
		matchers := []types.GomegaMatcher{ContainSubstring(reason)}
		for _, other := range []string{"replacement", "removal version", "removal date"} {
			if other != reason {
				matchers = append(matchers, Not(ContainSubstring(other)))
			}
		}
		return SatisfyAll(matchers...)
	}

	DescribeTable("panics on a deprecation that does not name a replacement, a removal version and a removal date",
		func(d deprecation, reason string) {
			flags := pflag.NewFlagSet("throwaway", pflag.ContinueOnError)
			flags.Bool("old", false, "")
			old := &cobra.Command{Use: "old"}

			Expect(func() { deprecateFlag(flags, "old", d) }).To(PanicWith(MatchError(onlyReason(reason))))
			Expect(func() { deprecateCommand(old, d) }).To(PanicWith(MatchError(onlyReason(reason))))
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
