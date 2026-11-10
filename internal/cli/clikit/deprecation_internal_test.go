package clikit

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var _ = Describe("deprecation", func() {
	valid := Deprecation{Replacement: "use --new instead", RemovalVersion: "v0.9.0", RemovalDate: "2027-03-14"}

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

		DeprecateFlag(root.Flags(), "old", valid)

		Expect(root.Flags().Lookup("old").Hidden).To(BeTrue())
		Expect(warning(root, "--old")).To(Equal("Flag --old has been deprecated, use --new instead; " +
			"it may be removed in v0.9.0 or later, and no earlier than 2027-03-14\n"))
	})

	It("hides a deprecated command and makes it warn with its replacement, earliest removal version and date", func() {
		root := &cobra.Command{Use: "coroner"}
		old := &cobra.Command{Use: "old", Run: func(*cobra.Command, []string) {}}
		root.AddCommand(old)

		DeprecateCommand(old, valid)

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
		func(d Deprecation, reason string) {
			flags := pflag.NewFlagSet("throwaway", pflag.ContinueOnError)
			flags.Bool("old", false, "")
			old := &cobra.Command{Use: "old"}

			Expect(func() { DeprecateFlag(flags, "old", d) }).To(PanicWith(MatchError(onlyReason(reason))))
			Expect(func() { DeprecateCommand(old, d) }).To(PanicWith(MatchError(onlyReason(reason))))
			Expect(flags.Lookup("old").Deprecated).To(BeEmpty())
			Expect(old.Deprecated).To(BeEmpty())
		},
		Entry("no replacement",
			Deprecation{RemovalVersion: "v0.9.0", RemovalDate: "2027-03-14"}, "replacement"),
		Entry("no removal version",
			Deprecation{Replacement: "use --new instead", RemovalDate: "2027-03-14"}, "removal version"),
		Entry("a removal version without its v",
			Deprecation{Replacement: "use --new instead", RemovalVersion: "0.9.0", RemovalDate: "2027-03-14"}, "removal version"),
		Entry("a removal version without its patch",
			Deprecation{Replacement: "use --new instead", RemovalVersion: "v0.9", RemovalDate: "2027-03-14"}, "removal version"),
		Entry("no removal date",
			Deprecation{Replacement: "use --new instead", RemovalVersion: "v0.9.0"}, "removal date"),
		Entry("a removal date that is not YYYY-MM-DD",
			Deprecation{Replacement: "use --new instead", RemovalVersion: "v0.9.0", RemovalDate: "2027-3-14"}, "removal date"),
		Entry("a removal date that is not a date",
			Deprecation{Replacement: "use --new instead", RemovalVersion: "v0.9.0", RemovalDate: "next spring"}, "removal date"),
	)
})
