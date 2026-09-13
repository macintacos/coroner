package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"charm.land/huh/v2"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = Describe("timeline forms", func() {
	var (
		out, errOut bytes.Buffer
		home, db    string
		ids         int
		asked       [][]huh.Field
	)

	// appOn returns a terminal App on home's database file path.
	appOn := func() App {
		app := scratchApp(home, &out, &errOut, func() (uuid.UUID, error) {
			ids++
			return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
		})
		app.InIsTTY, app.OutIsTTY = true, true
		return app
	}

	// runOn runs args through Run against path, and returns the exit code.
	runOn := func(ctx SpecContext, path string, args ...string) int {
		GinkgoHelper()
		out.Reset()
		errOut.Reset()
		app := appOn()
		app.Args = append([]string{"--db", path}, args...)
		return Run(ctx, app)
	}
	run := func(ctx SpecContext, args ...string) int {
		GinkgoHelper()
		return runOn(ctx, db, args...)
	}

	// answers returns forms that answer each ask in turn with the next of answers, recording what
	// each ask showed.
	answers := func(answers ...func([]huh.Field) (bool, error)) func(bool) askFunc {
		return func(bool) askFunc {
			return func(_ context.Context, fields ...huh.Field) (bool, error) {
				GinkgoHelper()
				asked = append(asked, fields)
				Expect(answers).NotTo(BeEmpty(), "asked a form no answer was scripted for")
				answer := answers[0]
				answers = answers[1:]
				return answer(fields)
			}
		}
	}

	// runForm runs args against a root whose timeline command holds command, as RunE runs on a
	// terminal.
	runForm := func(ctx SpecContext, command *cobra.Command, args ...string) error {
		GinkgoHelper()
		out.Reset()
		errOut.Reset()
		root := &cobra.Command{Use: "coroner", SilenceErrors: true, SilenceUsage: true}
		root.PersistentFlags().AddFlagSet(globalFlags())
		parent := &cobra.Command{Use: "timeline"}
		parent.AddCommand(command)
		root.AddCommand(parent)
		root.SetArgs(append([]string{"--db", db}, args...))
		return root.ExecuteContext(ctx)
	}

	// replay runs the last echoed command, with extra args, against a fresh database that setup
	// prepares as the form's was, and returns what it printed as JSON without record keys.
	replay := func(ctx SpecContext, setup func(path string), extra ...string) map[string]any {
		GinkgoHelper()
		echoed := echoedArgs(ctx, errOut.String())[1:]
		Expect(echoed[:2]).To(Equal([]string{"--db", db}))
		fresh := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		ids = 0
		setup(fresh)
		Expect(runOn(ctx, fresh, append(echoed[2:], extra...)...)).To(Equal(0), errOut.String())

		return withoutRecordKeys(out.Bytes())
	}

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		db = filepath.Join(home, "coroner.db")
		ids = 0
		asked = nil
	})

	Describe("timeline create", func() {
		create := func(ctx SpecContext, forms func(bool) askFunc) *cobra.Command {
			command := &cobra.Command{Use: "create", Annotations: map[string]string{formatsAnnotation: "text,json"}}
			return timelineCreateForm(ctx, appOn()).attach(command, appOn(), forms)
		}
		// named fills in a timeline's name and title, enters each of refs, and submits.
		named := func(name, title string, refs ...string) func([]huh.Field) (bool, error) {
			return func(fields []huh.Field) (bool, error) {
				fill(fields, map[string]string{"name": name, "title": title})
				for _, ref := range refs {
					enter(namedField(fields, "ref"), ref)
				}
				return true, nil
			}
		}
		existing := func(ctx SpecContext, path string) {
			GinkgoHelper()
			Expect(runOn(ctx, path, "timeline", "create", "mongo-outage", "--title", "Primary election storm")).To(Equal(0), errOut.String())
		}

		It("exit 2 off a terminal when the name is missing, as cobra rejects it", func(ctx SpecContext) {
			app := appOn()
			app.InIsTTY = false
			app.Args = []string{"--db", db, "timeline", "create"}

			Expect(Run(ctx, app)).To(Equal(2))
			Expect(errOut.String()).To(ContainSubstring("accepts 1 arg(s), received 0"))
		})

		It("write the name, title and refs submitted, as its echoed command does", func(ctx SpecContext) {
			command := create(ctx, answers(named("mongo-outage", "Primary election storm", "INC-4821", "INC-4822", "INC-4823")))

			Expect(runForm(ctx, command, "timeline", "create", "--json")).To(Succeed())
			written := withoutRecordKeys(out.Bytes())

			Expect(written).To(HaveKeyWithValue("refs", []any{"INC-4821", "INC-4822", "INC-4823"}))
			Expect(errOut.String()).To(ContainSubstring("Next:"))
			Expect(replay(ctx, func(string) {}, "--json")).To(Equal(written))
		})

		It("note under the name that a valid name is taken", func(ctx SpecContext) {
			existing(ctx, db)
			command := create(ctx, answers(cancel))

			Expect(runForm(ctx, command, "timeline", "create", "mongo-outage", "--interactive")).To(Succeed())

			Expect(renderedDescription(namedField(asked[0], "name"))).To(ContainSubstring("`mongo-outage` exists"))
		})

		It("use the existing timeline for a taken name, printing what --if-not-exists prints", func(ctx SpecContext) {
			existing(ctx, db)
			command := create(ctx, answers(named("mongo-outage", "Other title"), accept))

			Expect(runForm(ctx, command, "timeline", "create", "--json")).To(Succeed())
			used := bytes.Clone(out.Bytes())

			Expect(echoedArgs(ctx, errOut.String())).To(ContainElement("--if-not-exists"))
			Expect(run(ctx, "timeline", "create", "mongo-outage", "--if-not-exists", "--json")).To(Equal(0), errOut.String())
			Expect(used).To(Equal(out.Bytes()))
		})

		It("reopen with what was typed when the taken name is not used", func(ctx SpecContext) {
			existing(ctx, db)
			command := create(ctx, answers(named("mongo-outage", "Other title", "INC-1"), decline, cancel))

			Expect(runForm(ctx, command, "timeline", "create")).To(Succeed())

			Expect(asked).To(HaveLen(3))
			Expect(fieldText(asked[2], "title")).To(Equal("Other title"))
			Expect(fieldText(asked[2], "ref")).To(Equal("INC-1"))
			Expect(run(ctx, "timeline", "list", "--json")).To(Equal(0), errOut.String())
			Expect(out.String()).NotTo(ContainSubstring("Other title"))
		})
	})
})

var _ = DescribeTable("a name field's preview",
	func(name string, want OmegaMatcher) {
		preview := namePreview([]string{"mongo-outage"}, "Timeline `%s` exists.")

		Expect(preview(name, checkEnv{})).To(want)
	},
	Entry("shows nothing for no name", "", BeEmpty()),
	Entry("shows nothing for a free name", "mongo-failover", BeEmpty()),
	Entry("notes a taken name with its note", "mongo-outage", Equal("Timeline `mongo-outage` exists.")),
	Entry("shows the name suggested for a rejected one", "Mongo Outage", ContainSubstring("`mongo-outage`")),
)
