package cli

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("timeline forms", func() {
	var (
		out, errOut bytes.Buffer
		home, db    string
		ids         int
		asked       [][]huh.Field
	)

	// appOn returns a terminal App on home.
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
	answers := func(answers ...func([]huh.Field) (bool, error)) func(bool, graphics.Protocol) askFunc {
		return func(bool, graphics.Protocol) askFunc {
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
		create := func(ctx SpecContext, forms func(bool, graphics.Protocol) askFunc) *cobra.Command {
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

		DescribeTable("exit 2 off a terminal unless one name is given, as cobra rejects it, showing the usage line",
			func(ctx SpecContext, args []string, message string) {
				errOut.Reset()
				app := appOn()
				app.InIsTTY = false
				app.Args = append([]string{"--db", db, "timeline", "create"}, args...)

				Expect(Run(ctx, app)).To(Equal(2))
				Expect(errOut.String()).To(ContainSubstring(message))
				Expect(errOut.String()).To(ContainSubstring("Usage: coroner timeline create <name> [flags]\nRun 'coroner timeline create --help' for more."))
			},
			Entry("no name", nil, "accepts 1 arg(s), received 0"),
			Entry("two names", []string{"mongo-outage", "second"}, "accepts 1 arg(s), received 2"),
		)

		It("write nothing and report interrupted when Ctrl-C ends the form", func(ctx SpecContext) {
			command := create(ctx, answers(func(fields []huh.Field) (bool, error) {
				fill(fields, map[string]string{"name": "mongo-outage"})
				return false, huh.ErrUserAborted
			}))

			err := runForm(ctx, command, "timeline", "create")

			Expect(err).To(HaveField("Kind", KindInterrupted))
			Expect(run(ctx, "timeline", "list", "--json")).To(Equal(0), errOut.String())
			Expect(out.String()).NotTo(ContainSubstring("mongo-outage"))
		})

		It("write the name, title and refs submitted, as its echoed command does", func(ctx SpecContext) {
			command := create(ctx, answers(named("mongo-failover", "Primary election storm", "INC-4821", "INC-4822", "INC-4823")))

			Expect(runForm(ctx, command, "timeline", "create", "--interactive", "--json")).To(Succeed())
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

		It("note under a taken name that submitting uses it when --if-not-exists is given", func(ctx SpecContext) {
			existing(ctx, db)
			command := create(ctx, answers(cancel))

			Expect(runForm(ctx, command, "timeline", "create", "mongo-outage", "--if-not-exists", "--interactive")).To(Succeed())

			Expect(renderedDescription(namedField(asked[0], "name"))).To(ContainSubstring("submitting uses it"))
		})

		It("use the existing timeline for a taken name without asking when --if-not-exists is given", func(ctx SpecContext) {
			existing(ctx, db)
			command := create(ctx, answers(named("mongo-outage", "Other title")))

			Expect(runForm(ctx, command, "timeline", "create", "--if-not-exists", "--interactive", "--json")).To(Succeed())

			Expect(asked).To(HaveLen(1))
			Expect(withoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("title", "Primary election storm"))
			Expect(echoedArgs(ctx, errOut.String())).To(ContainElement("--if-not-exists"))
		})

		It("use the existing timeline for a taken name, printing what --if-not-exists prints", func(ctx SpecContext) {
			existing(ctx, db)
			command := create(ctx, answers(named("mongo-outage", "Other title"), accept))

			Expect(runForm(ctx, command, "timeline", "create", "--interactive", "--json")).To(Succeed())
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
			Expect(fieldText(asked[2], "ref")).To(Equal("[INC-1]"))
			Expect(run(ctx, "timeline", "list", "--json")).To(Equal(0), errOut.String())
			Expect(out.String()).NotTo(ContainSubstring("Other title"))
		})
	})

	Describe("timeline edit", func() {
		edit := func(ctx SpecContext, forms func(bool, graphics.Protocol) askFunc) *cobra.Command {
			command := &cobra.Command{Use: "edit", Annotations: map[string]string{formatsAnnotation: "text,json"}}
			return timelineEditForm(ctx, appOn()).attach(command, appOn(), forms)
		}
		seed := func(ctx SpecContext, path string) {
			GinkgoHelper()
			Expect(runOn(ctx, path, "timeline", "create", "mongo-outage", "--title", "Primary election storm",
				"--ref", "INC-4821", "--ref", "INC-4822")).To(Equal(0), errOut.String())
		}
		// renamed fills in name and submits.
		renamed := func(name string) func([]huh.Field) (bool, error) {
			return submit(map[string]string{"name": name})
		}

		BeforeEach(func(ctx SpecContext) {
			seed(ctx, db)
		})

		It("open prefilled with the stored name, title and refs when no change is given", func(ctx SpecContext) {
			command := edit(ctx, answers(cancel))

			Expect(runForm(ctx, command, "timeline", "edit", "mongo-outage")).To(Succeed())

			Expect(fieldText(asked[0], "name")).To(Equal("mongo-outage"))
			Expect(fieldText(asked[0], "title")).To(Equal("Primary election storm"))
			Expect(fieldText(asked[0], "ref")).To(Equal("[INC-4821 INC-4822]"))
		})

		It("replace a rejected new name with the one its field suggests on ctrl+e", func() {
			name := namedField(timelineEditFormDefinition(nil).formFields(formCheckEnv(), fieldValues{}, formChoices{}), "name")
			typeInto(name, "Mongo Failover")

			name.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})

			Expect(fieldText([]huh.Field{name}, "name")).To(Equal("mongo-failover"))
		})

		It("store the refs added and removed, as its echoed command does", func(ctx SpecContext) {
			command := edit(ctx, answers(func(fields []huh.Field) (bool, error) {
				refs := namedField(fields, "ref")
				for _, ref := range []string{"INC-1", "INC-2", "INC-3"} {
					enter(refs, ref)
				}
				press(refs, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyBackspace)
				return true, nil
			}))

			Expect(runForm(ctx, command, "timeline", "edit", "mongo-outage", "--interactive", "--json")).To(Succeed())
			written := withoutRecordKeys(out.Bytes())

			Expect(written).To(HaveKeyWithValue("refs", []any{"INC-4822", "INC-1", "INC-2", "INC-3"}))
			Expect(replay(ctx, func(path string) { seed(ctx, path) }, "--dry-run", "--json")).To(Equal(written))
		})

		It("keep a renamed current timeline current, as its echoed command renames it", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())
			command := edit(ctx, answers(renamed("mongo-failover")))

			Expect(runForm(ctx, command, "timeline", "edit", "--interactive", "--json")).To(Succeed())
			written := withoutRecordKeys(out.Bytes())

			Expect(errOut.String()).To(ContainSubstring("`mongo-outage` no longer selects"))
			Expect(replay(ctx, func(path string) { seed(ctx, path) }, "--dry-run", "--json")).To(Equal(written))
			Expect(run(ctx, "status", "--json")).To(Equal(0), errOut.String())
			Expect(withoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("timeline", HaveKeyWithValue("name", "mongo-failover")))
		})

		It("ask which timeline to edit when none is selected", func(ctx SpecContext) {
			command := edit(ctx, answers(submit(map[string]string{"timeline": "mongo-outage"}), cancel))

			Expect(runForm(ctx, command, "timeline", "edit")).To(Succeed())

			Expect(asked).To(HaveLen(2))
			Expect(fieldText(asked[1], "name")).To(Equal("mongo-outage"))
		})

		It("exit as timeline_required, pointing at timeline create, when there is no timeline to edit", func(ctx SpecContext) {
			db = filepath.Join(GinkgoT().TempDir(), "coroner.db")
			command := edit(ctx, answers())

			err := runForm(ctx, command, "timeline", "edit")

			Expect(err).To(HaveField("Kind", KindTimelineRequired))
			Expect(err).To(HaveField("Hint", ContainSubstring("coroner timeline create")))
		})

		It("reopen with what was typed when the new name is taken", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "second")).To(Equal(0), errOut.String())
			command := edit(ctx, answers(renamed("second"), cancel))

			Expect(runForm(ctx, command, "timeline", "edit", "mongo-outage")).To(Succeed())

			Expect(errOut.String()).To(ContainSubstring(`timeline "second" already exists`))
			Expect(fieldText(asked[1], "name")).To(Equal("second"))
		})

		It("write nothing and echo nothing when nothing changed", func(ctx SpecContext) {
			command := edit(ctx, answers(submit(map[string]string{})))

			Expect(runForm(ctx, command, "timeline", "edit", "mongo-outage")).To(Succeed())

			Expect(errOut.String()).To(ContainSubstring("Nothing to change."))
			Expect(errOut.String()).NotTo(ContainSubstring("Equivalent command"))
		})
	})
})

var _ = DescribeTable("timelineEditValues gives edit the changes a form made to a timeline",
	func(submitted, want fieldValues) {
		t := timeline.Timeline{Name: "mongo-outage", Title: "Primary election storm", Refs: []string{"INC-1", "INC-2"}}
		unchanged := fieldValues{"name": {"mongo-outage"}, "title": {"Primary election storm"}, "ref": {"INC-1", "INC-2"}}
		maps.Copy(unchanged, submitted)
		maps.DeleteFunc(unchanged, func(_ string, values []string) bool { return values == nil })

		Expect(timelineEditValues(t, "mongo-outage", unchanged)).To(Equal(want))
	},
	Entry("nothing when nothing changed", fieldValues{}, fieldValues{"timeline": {"mongo-outage"}}),
	Entry("a new name", fieldValues{"name": {"mongo-failover"}},
		fieldValues{"timeline": {"mongo-outage"}, "name": {"mongo-failover"}}),
	Entry("a cleared title as an empty one", fieldValues{"title": nil},
		fieldValues{"timeline": {"mongo-outage"}, "title": {""}}),
	Entry("the refs added and those removed", fieldValues{"ref": {"INC-2", "INC-3", "INC-4"}},
		fieldValues{"timeline": {"mongo-outage"}, "add-ref": {"INC-3", "INC-4"}, "remove-ref": {"INC-1"}}),
	Entry("nothing for the same refs in another order", fieldValues{"ref": {"INC-2", "INC-1"}},
		fieldValues{"timeline": {"mongo-outage"}}),
	Entry("every ref removed", fieldValues{"ref": nil},
		fieldValues{"timeline": {"mongo-outage"}, "remove-ref": {"INC-1", "INC-2"}}),
	Entry("a dry run", fieldValues{"dry-run": {"true"}},
		fieldValues{"timeline": {"mongo-outage"}, "dry-run": {"true"}}),
)

var _ = DescribeTable("a name field's preview",
	func(name string, want OmegaMatcher) {
		preview := namePreview([]string{"mongo-outage"}, func(name string) string { return "Timeline `" + name + "` exists." })

		Expect(preview(name, checkEnv{})).To(want)
	},
	Entry("shows nothing for no name", "", BeEmpty()),
	Entry("shows nothing for a free name", "mongo-failover", BeEmpty()),
	Entry("notes a taken name with its note", "mongo-outage", Equal("Timeline `mongo-outage` exists.")),
	Entry("shows the name suggested for a rejected one", "Mongo Outage", ContainSubstring("`mongo-outage`")),
)
