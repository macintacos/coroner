package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli/annotation"
	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/clitest"
	"github.com/macintacos/coroner/internal/cli/event"
	timelinecmd "github.com/macintacos/coroner/internal/cli/timeline"
)

var _ = Describe("form fields", func() {
	eventOptions := clikit.FormChoices{Options: map[string][]huh.Option[string]{
		"event": {huh.NewOption("none (the whole timeline)", ""), huh.NewOption("#12  Primary stepped down", "12")},
	}}
	kinds := []huh.Option[string]{huh.NewOption("evidence", "evidence"), huh.NewOption("comment", "comment")}
	kindOptions := clikit.FormChoices{Options: map[string][]huh.Option[string]{"kind": kinds}}
	every := clikit.FieldValues{
		"timeline": {"mongo-outage"}, "text": {"Primary stepped down"}, "tz": {"America/New_York"},
		"at": {"2026-09-11 03:14"}, "approximate": {"true"}, "source": {"mongod"},
		"tag": {"election", "phase:detect"}, "dry-run": {"true"},
	}

	DescribeTable("give back the values they hold as the command line would give them",
		func(def clikit.Definition, in clikit.FieldValues, choices clikit.FormChoices, want clikit.FieldValues) {
			Expect(def.FormValues(def.FormFields(clitest.FormCheckEnv(), in, choices))).To(Equal(want))
		},
		Entry("every value they open with", event.AddDefinition(), every, clikit.FormChoices{}, every),
		Entry("only the non-blank lines of a repeatable field", event.AddDefinition(),
			clikit.FieldValues{"tag": {"election", "", " ", "phase:detect"}}, clikit.FormChoices{},
			clikit.FieldValues{"tag": {"election", "phase:detect"}}),
		Entry("nothing for a confirm left false", event.AddDefinition(),
			clikit.FieldValues{"approximate": {"false"}, "dry-run": {"false"}}, clikit.FormChoices{}, clikit.FieldValues{}),
		Entry("nothing for an empty input", event.AddDefinition(),
			clikit.FieldValues{"url": {""}}, clikit.FormChoices{}, clikit.FieldValues{}),
		Entry("nothing for a select on an empty option", annotation.AddDefinition(),
			clikit.FieldValues{}, eventOptions, clikit.FieldValues{}),
		Entry("a select's chosen option", annotation.AddDefinition(),
			clikit.FieldValues{"event": {"12"}}, eventOptions, clikit.FieldValues{"event": {"12"}}),
		Entry("a select's first option when it opens blank", annotation.AddDefinition(),
			clikit.FieldValues{}, kindOptions, clikit.FieldValues{"kind": {"evidence"}}),
		Entry("an event no option holds, for its check to judge", annotation.AddDefinition(),
			clikit.FieldValues{"event": {"mongo-outage#12"}}, eventOptions, clikit.FieldValues{"event": {"mongo-outage#12"}}),
		Entry("a kind no option holds, for its check to judge", annotation.AddDefinition(),
			clikit.FieldValues{"kind": {"Evidence"}}, kindOptions, clikit.FieldValues{"kind": {"Evidence"}}),
		Entry("the positional argument as one value", annotation.AddDefinition(),
			clikit.FieldValues{"body": {"Step-down lands 5 s after the COLLSCAN burst"}}, clikit.FormChoices{},
			clikit.FieldValues{"body": {"Step-down lands 5 s after the COLLSCAN burst"}}),
	)

	It("mark a required field and refuse it blank", func() {
		choices := clikit.FormChoices{Options: map[string][]huh.Option[string]{
			"kind": append([]huh.Option[string]{huh.NewOption("choose…", "")}, kinds...),
		}}
		kind := clitest.NamedField(annotation.AddDefinition().FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices), "kind")

		kind.Blur()

		Expect(kind.Error()).To(SatisfyAll(HaveField("Kind", clikit.KindUsage), MatchError("kind is required")))
		Expect(clitest.FieldView(kind)).To(ContainSubstring("kind (required)"))
	})

	It("title each event add field for people", func() {
		fields := event.AddDefinition().FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{})

		for name, title := range map[string]string{
			"text": "What happened (required)", "at": "When", "source": "Source", "tag": "Tags", "attach": "Files", "url": "url",
		} {
			Expect(clitest.FieldView(clitest.NamedField(fields, name))).To(ContainSubstring(title), name)
		}
	})

	It("head the timeline picker of each form command 📒 Timeline", func() {
		for _, def := range []clikit.Definition{event.AddDefinition(), annotation.AddDefinition(), timelinecmd.EditDefinition()} {
			picker := clikit.OnlyField(def, "timeline").FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{})

			Expect(clitest.FieldView(clitest.NamedField(picker, "timeline"))).To(ContainSubstring("📒 Timeline"))
		}
	})

	It("head each field of the form commands with an icon before its title", func() {
		forms := []clikit.Definition{
			clikit.WithoutField(event.AddDefinition(), "id"), annotation.AddDefinition(),
			clikit.WithoutField(timelinecmd.CreateDefinition(), "if-not-exists"), timelinecmd.EditFormDefinition(nil),
		}
		for _, def := range forms {
			for i, formField := range def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{}) {
				f := def.Fields[i]
				Expect(clikit.FieldIcon(f.Name)).NotTo(BeEmpty(), f.Name)
				Expect(clitest.FieldView(formField)).To(ContainSubstring(clikit.FieldIcon(f.Name)+" "+f.FormTitle()), f.Name)
			}
		}
	})

	It("frame a form's view in a border as wide as asked, with its title set into the top edge and a blank row under it", func() {
		lines := strings.Split(ansi.Strip(clikit.Frame(clikit.FrameEdges{Title: "📝 New event"}, "What happened\n> Primary stepped down", 40)), "\n")

		Expect(lines).To(HaveLen(5))
		Expect(lines[0]).To(HavePrefix("╭─ 📝 New event ─"))
		Expect(lines[1]).To(MatchRegexp(`^│ +│$`))
		Expect(lines[2]).To(HavePrefix("│ What happened"))
		Expect(lines[3]).To(HavePrefix("│ > Primary stepped down"))
		Expect(lines[4]).To(HavePrefix("╰─"))
		for _, line := range lines {
			Expect(ansi.StringWidth(line)).To(Equal(40), line)
		}
	})

	It("clip a line wider than the frame's inside, so the frame keeps the rows the form planned", func() {
		lines := strings.Split(ansi.Strip(clikit.Frame(clikit.FrameEdges{Title: "📝 New event"}, strings.Repeat("x", 50)+"\n> typed", 40)), "\n")

		Expect(lines).To(HaveLen(5))
		Expect(lines[3]).To(HavePrefix("│ > typed"))
		for _, line := range lines {
			Expect(ansi.StringWidth(line)).To(Equal(40), line)
		}
	})

	It("set an edge's pieces into its right end, two spaces apart", func() {
		edges := clikit.FrameEdges{
			Title: "📝 New event", Top: []string{"📒 inc-4821", "ctrl+t switch"}, Bottom: []string{"✓ 2 events added", "esc finish"},
		}

		lines := strings.Split(ansi.Strip(clikit.Frame(edges, "What happened", 80)), "\n")

		Expect(lines[0]).To(Equal("╭─ 📝 New event " + strings.Repeat("─", 34) + " 📒 inc-4821  ctrl+t switch ─╮"))
		Expect(lines[len(lines)-1]).To(Equal("╰" + strings.Repeat("─", 47) + " ✓ 2 events added  esc finish ─╯"))
		for _, line := range lines {
			Expect(ansi.StringWidth(line)).To(Equal(80), line)
		}
	})

	It("drop an edge's last pieces, then cut the first short, where the edge is too narrow, keeping a rule cell", func() {
		edges := clikit.FrameEdges{
			Title: "📝 New event", Top: []string{"📒 inc-4821", "ctrl+t switch"}, Bottom: []string{"✓ 2 events added", "esc finish"},
		}

		lines := strings.Split(ansi.Strip(clikit.Frame(edges, "What happened", 30)), "\n")

		Expect(lines[0]).To(Equal("╭─ 📝 New event ─ 📒 inc-4… ─╮"))
		Expect(lines[len(lines)-1]).To(Equal("╰" + strings.Repeat("─", 9) + " ✓ 2 events added ─╯"))
		for _, line := range lines {
			Expect(ansi.StringWidth(line)).To(Equal(30), line)
		}
	})

	DescribeTable("show an add session's timeline and the records it added in the edges of each form it asks",
		func(f clikit.FormFrame, title string, top, bottom []string) {
			edges := f.Edges("📝 New event")

			Expect(edges.Title).To(Equal(title))
			Expect(clitest.Stripped(edges.Top)).To(Equal(top))
			Expect(clitest.Stripped(edges.Bottom)).To(Equal(bottom))
		},
		Entry("an entry before any was added",
			clikit.FormFrame{Timeline: "inc-4821", Entry: true, Noun: "event"}, "📝 New event",
			[]string{"📒 inc-4821", "ctrl+t switch"}, nil),
		Entry("an entry after two were added",
			clikit.FormFrame{Timeline: "inc-4821", Entry: true, Added: 2, Noun: "event"}, "📝 New event",
			[]string{"📒 inc-4821", "ctrl+t switch"}, []string{"✓ 2 events added", "esc finish"}),
		Entry("Add another",
			clikit.FormFrame{Timeline: "inc-4821", Added: 2, Noun: "event"}, "📝 New event", []string{"📒 inc-4821"}, []string{"✓ 2 events added"}),
		Entry("the timeline picker",
			clikit.FormFrame{Added: 1, Noun: "annotation"}, "📝 New event", nil, []string{"✓ 1 annotation added"}),
		Entry("the new timeline form",
			clikit.FormFrame{Title: "📒 New timeline", Added: 1, Noun: "event"}, "📒 New timeline", nil, []string{"✓ 1 event added"}),
		Entry("the first timeline picker", clikit.FormFrame{}, "📝 New event", nil, nil),
	)

	It("frame a form at most 80 cells wide on a wider terminal", func() {
		form := clikit.NewForm(clikit.FrameEdges{Title: "📝 New event"}, clikit.FormGroup{Fields: []huh.Field{huh.NewInput().Title("What happened")}})

		form.Update(tea.WindowSizeMsg{Width: 200, Height: 20})

		for line := range strings.SplitSeq(strings.TrimLeft(ansi.Strip(form.View().Content), "\n"), "\n") {
			Expect(ansi.StringWidth(line)).To(Equal(80), line)
		}
	})

	It("draw a blank row between the output above a form and its frame", func() {
		form := clikit.NewForm(clikit.FrameEdges{Title: "📝 New event"}, clikit.FormGroup{Fields: []huh.Field{huh.NewInput().Title("What happened")}})

		form.Update(tea.WindowSizeMsg{Width: 60, Height: 20})

		lines := strings.Split(ansi.Strip(form.View().Content), "\n")
		Expect(lines[0]).To(BeEmpty())
		Expect(lines[1]).To(HavePrefix("╭─ 📝 New event"))
	})

	Describe("event add's groups", func() {
		def := clikit.WithoutField(event.AddDefinition(), "id")
		groupsFor := func(in clikit.FieldValues) []clikit.FormGroup {
			return def.FormGroups(def.FormFields(clitest.FormCheckEnv(), in, clikit.FormChoices{}), in)
		}

		It("show the fields most events use first, and the rest behind More options", func() {
			groups := groupsFor(clikit.FieldValues{})

			Expect(groups).To(HaveLen(2))
			Expect(clitest.FieldKeys(groups[0].Fields)).To(Equal([]string{"timeline", "text", "at", "source", "tag", "attach", "more-options"}))
			Expect(clitest.FieldKeys(groups[1].Fields)).To(Equal([]string{"tz", "ended", "approximate", "url", "author", "dry-run"}))
			Expect(clitest.FieldView(clitest.NamedField(groups[0].Fields, "more-options"))).To(
				ContainSubstring("tz, ended, approximate, url, author, dry-run"))
		})

		It("hide the More options fields until it is Yes", func() {
			groups := groupsFor(clikit.FieldValues{})
			Expect(groups[1].Hide()).To(BeTrue())

			clitest.Fill(groups[0].Fields, map[string]string{"more-options": "true"})

			Expect(groups[1].Hide()).To(BeFalse())
		})

		It("open More options when a field behind it holds a value", func() {
			groups := groupsFor(clikit.FieldValues{"url": {"https://e.example"}})

			Expect(clitest.NamedField(groups[0].Fields, "more-options").GetValue()).To(BeTrue())
			Expect(groups[1].Hide()).To(BeFalse())
		})
	})

	It("keep one group for annotation add, timeline create and timeline edit", func() {
		for _, def := range []clikit.Definition{annotation.AddDefinition(), timelinecmd.CreateDefinition(), timelinecmd.EditDefinition()} {
			fields := def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{})

			groups := def.FormGroups(fields, clikit.FieldValues{})

			Expect(groups).To(HaveLen(1))
			Expect(groups[0].Fields).To(Equal(fields))
			Expect(groups[0].Hide).To(BeNil())
		}
	})

	It("read a line holding only spaces as no value, and check only the others", func() {
		def := event.EditDefinition()
		fields := def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{})
		tag := clitest.NamedField(fields, "add-tag")
		clitest.TypeInto(tag, "election\n  ")

		tag.Blur()

		Expect(tag.Error()).NotTo(HaveOccurred())
		Expect(def.FormValues(fields)).To(HaveKeyWithValue("add-tag", []string{"election"}))
	})

	It("pick from options in a select, and suggest in an input", func() {
		choices := eventOptions
		choices.Suggestions = map[string][]string{"source": {"mongod", "slack"}}
		fields := annotation.AddDefinition().FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices)

		event := clitest.NamedField(fields, "event")
		Expect(event).To(BeAssignableToTypeOf(&huh.Select[string]{}))
		Expect(clitest.FieldView(event)).To(ContainSubstring("#12  Primary stepped down"))

		var keys []string
		for _, binding := range clitest.NamedField(fields, "source").WithKeyMap(huh.NewDefaultKeyMap()).KeyBinds() {
			keys = append(keys, binding.Help().Desc)
		}
		Expect(keys).To(ContainElement("complete"))
	})

	Describe("a time field", func() {
		// 02:30 on 2026-03-08 is a wall clock New York's DST change skips, and a real time in UTC.
		const gap = "2026-03-08 02:30"
		utc := clikit.CheckEnv{Now: clitest.TestNow(), Zone: clikit.Zone{Loc: time.UTC, Label: "UTC"}}

		It("checks its time in the zone typed into the form", func() {
			fields := event.AddDefinition().FormFields(utc, clikit.FieldValues{}, clikit.FormChoices{})
			at := clitest.NamedField(fields, "at")
			clitest.TypeInto(at, gap)

			at.Blur()
			Expect(at.Error()).NotTo(HaveOccurred())

			clitest.TypeInto(clitest.NamedField(fields, "tz"), "America/New_York")
			at.Blur()
			Expect(at.Error()).To(HaveField("Kind", clikit.KindInvalidTime))
		})

		DescribeTable("previews its time under its help, in the zone typed into the form",
			func(tz, want string) {
				fields := event.AddDefinition().FormFields(utc, clikit.FieldValues{}, clikit.FormChoices{})
				at := clitest.NamedField(fields, "at")
				clitest.TypeInto(at, gap)
				clitest.TypeInto(clitest.NamedField(fields, "tz"), tz)

				Expect(clitest.RenderedDescription(at)).To(SatisfyAll(ContainSubstring("When it happened"), ContainSubstring(want)))
			},
			Entry("the base zone with none typed", "", "2026-03-08T02:30:00Z, minute precision"),
			Entry("the gap's hint in a zone that skips it", "America/New_York", "-05:00"),
		)

		It("preview the time in the zone typed into the zone field", func() {
			fields := event.AddDefinition().FormFields(utc, clikit.FieldValues{}, clikit.FormChoices{})
			clitest.TypeInto(clitest.NamedField(fields, "at"), "03:25")
			tz := clitest.NamedField(fields, "tz")
			clitest.TypeInto(tz, "Asia/Tokyo")

			Expect(clitest.RenderedDescription(tz)).To(ContainSubstring("(+09:00 Asia/Tokyo: 03:25:00)"))

			unknown := clitest.NamedField(event.AddDefinition().FormFields(utc, clikit.FieldValues{}, clikit.FormChoices{}), "tz")
			clitest.TypeInto(unknown, "Mars/Olympus")
			Expect(clitest.RenderedDescription(unknown)).To(ContainSubstring(clikit.ZoneHint))
		})

		It("shows a failing check's hint under the help of a field with no preview", func() {
			source := clitest.NamedField(event.AddDefinition().FormFields(utc, clikit.FieldValues{}, clikit.FormChoices{}), "source")
			clitest.TypeInto(source, "DB 2")

			Expect(clitest.RenderedDescription(source)).To(ContainSubstring("Did you mean `db-2`?"))
		})
	})

	Describe("a list field", func() {
		rejectBad := clikit.WithoutEnv(func(ref string) error {
			if ref == "Bad Ref" {
				return errors.New("bad ref")
			}
			return nil
		})
		refs := clikit.Definition{Fields: []clikit.Field{{Name: "ref", Typ: clikit.StringsField, List: true, Usage: "Ticket key or URL", Check: rejectBad}}}

		It("adds each value typed and entered, once, after the values it opens with", func() {
			fields := refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{"ref": {"INC-1"}}, clikit.FormChoices{})
			ref := clitest.NamedField(fields, "ref")

			clitest.Enter(ref, "INC-2")
			clitest.Enter(ref, "INC-1")

			Expect(refs.FormValues(fields)).To(Equal(clikit.FieldValues{"ref": {"INC-1", "INC-2"}}))
			Expect(clitest.FieldView(ref)).To(SatisfyAll(ContainSubstring("Ticket key or URL"), ContainSubstring("INC-2")))
		})

		It("moves on when enter finds nothing typed, and stays while it adds", func() {
			ref := clitest.NamedField(refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{}), "ref")
			ref.WithKeyMap(huh.NewDefaultKeyMap())

			clitest.TypeInto(ref, "INC-1")
			_, stay := ref.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			_, next := ref.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

			Expect(stay).NotTo(BeNil())
			Expect(stay()).To(Equal(tea.RequestWindowSize()))
			Expect(next).NotTo(BeNil())
			Expect(next()).To(Equal(huh.NextField()))
		})

		It("removes the value up picks, counting back from the last", func() {
			fields := refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{"ref": {"INC-1", "INC-2", "INC-3"}}, clikit.FormChoices{})
			ref := clitest.NamedField(fields, "ref")

			clitest.Press(ref, tea.KeyUp, tea.KeyUp)
			Expect(clitest.FieldView(ref)).To(SatisfyAll(ContainSubstring("× INC-2"), ContainSubstring("✓ INC-3")))
			clitest.Press(ref, tea.KeyBackspace)

			Expect(refs.FormValues(fields)).To(Equal(clikit.FieldValues{"ref": {"INC-1", "INC-3"}}))
		})

		It("goes back to typing when down passes the last value", func() {
			fields := refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{"ref": {"INC-1"}}, clikit.FormChoices{})
			ref := clitest.NamedField(fields, "ref")

			clitest.Press(ref, tea.KeyUp, tea.KeyDown, tea.KeyBackspace)

			Expect(refs.FormValues(fields)).To(Equal(clikit.FieldValues{"ref": {"INC-1"}}))
			Expect(clitest.FieldView(ref)).NotTo(ContainSubstring("×"))
		})

		It("adds a value left typed when it loses focus", func() {
			fields := refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{})
			ref := clitest.NamedField(fields, "ref")
			clitest.TypeInto(ref, "INC-1")

			ref.Blur()

			Expect(ref.Error()).NotTo(HaveOccurred())
			Expect(refs.FormValues(fields)).To(Equal(clikit.FieldValues{"ref": {"INC-1"}}))
		})

		It("gives back a value it opens with unchanged, even one holding a newline", func() {
			in := clikit.FieldValues{"ref": {"INC-1\nfollow-up"}}

			Expect(refs.FormValues(refs.FormFields(clitest.FormCheckEnv(), in, clikit.FormChoices{}))).To(Equal(in))
		})

		It("shows the last five values while typing, and five around the picked one, counting the rest", func() {
			in := clikit.FieldValues{"ref": {"INC-1", "INC-2", "INC-3", "INC-4", "INC-5", "INC-6", "INC-7", "INC-8"}}
			ref := clitest.NamedField(refs.FormFields(clitest.FormCheckEnv(), in, clikit.FormChoices{}), "ref")

			Expect(clitest.FieldView(ref)).To(SatisfyAll(
				ContainSubstring("… 3 more"), ContainSubstring("INC-4"), ContainSubstring("INC-8"), Not(ContainSubstring("INC-3"))))

			clitest.Press(ref, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp)
			Expect(clitest.FieldView(ref)).To(SatisfyAll(
				ContainSubstring("… 3 more"), ContainSubstring("× INC-3"), ContainSubstring("INC-7"), Not(ContainSubstring("INC-8"))))
		})

		It("shows in its help only the keys that act on what it holds", func() {
			ref := clitest.NamedField(refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{}), "ref")
			ref.WithKeyMap(huh.NewDefaultKeyMap())
			Expect(clitest.HelpKeys(ref)).To(Equal([]string{"shift+tab back", "enter submit", "enter next"}))

			clitest.TypeInto(ref, "INC-1")
			Expect(clitest.HelpKeys(ref)).To(Equal([]string{"enter add", "shift+tab back"}))

			clitest.Press(ref, tea.KeyEnter)
			Expect(clitest.HelpKeys(ref)).To(Equal([]string{"↑/↓ pick", "shift+tab back", "enter submit", "enter next"}))

			clitest.Press(ref, tea.KeyUp)
			Expect(clitest.HelpKeys(ref)).To(Equal([]string{"↑/↓ pick", "backspace remove", "shift+tab back", "enter submit", "enter next"}))
		})

		It("keeps its typing line on screen inside a form after a dozen values", func() {
			def := clikit.Definition{Fields: []clikit.Field{{Name: "name", Arg: true, Check: clikit.WithoutEnv(timelinecmd.CheckName), Suggest: timelinecmd.SuggestName}, refs.Fields[0]}}
			form := huh.NewForm(huh.NewGroup(def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{})...))
			size := tea.WindowSizeMsg{Width: 100, Height: 12}
			form.Init()
			form.Update(size)

			clitest.TypeKeys(form, "Mongo Outage")
			form.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			form.Update(huh.NextField())
			for i := range 12 {
				clitest.TypeKeys(form, fmt.Sprintf("INC-%d", i))
				clitest.PressResizing(form, tea.KeyEnter, size)
			}
			clitest.TypeKeys(form, "TYPING-NOW")

			Expect(form.GetString("name")).To(Equal("mongo-outage"))
			Expect(ansi.Strip(form.View())).To(SatisfyAll(ContainSubstring("… 7 more"), ContainSubstring("TYPING-NOW")))
		})

		It("suggests nothing for the empty input after an add", func() {
			choices := clikit.FormChoices{Suggestions: map[string][]string{"ref": {"election", "escalation"}}}
			fields := refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices)
			ref := clitest.NamedField(fields, "ref")
			ref.WithKeyMap(huh.NewDefaultKeyMap())
			ref.Focus()

			clitest.TypeText(ref, "e")
			clitest.Press(ref, tea.KeyEnter)
			Expect(clitest.FieldView(ref)).NotTo(MatchRegexp(`> +election`))

			ref.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			clitest.Press(ref, tea.KeyEnter)

			Expect(refs.FormValues(fields)).To(Equal(clikit.FieldValues{"ref": {"e"}}))
		})

		It("picks values with up and down only while nothing is typed", func() {
			fields := refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{"ref": {"INC-1"}}, clikit.FormChoices{})
			ref := clitest.NamedField(fields, "ref")
			clitest.TypeInto(ref, "INC-2")

			clitest.Press(ref, tea.KeyUp)

			Expect(clitest.FieldView(ref)).NotTo(ContainSubstring("×"))
		})

		It("picks timeline create's refs with up and down", func() {
			def := timelinecmd.CreateDefinition()
			fields := def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{"ref": {"INC-1", "INC-2"}}, clikit.FormChoices{})
			ref := clitest.NamedField(fields, "ref")

			clitest.Press(ref, tea.KeyUp, tea.KeyBackspace)

			Expect(def.FormValues(fields)["ref"]).To(Equal([]string{"INC-1"}))
		})

		It("refuses a value its check rejects, and adds nothing", func() {
			fields := refs.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{})
			ref := clitest.NamedField(fields, "ref")

			clitest.Enter(ref, "Bad Ref")

			Expect(ref.Error()).To(MatchError("bad ref"))
			Expect(refs.FormValues(fields)).To(BeEmpty())
		})
	})

	Describe("a match row", func() {
		seven := []string{"db-1", "db-2", "db-3", "db-4", "db-5", "db-6", "db-7"}
		sourceWith := func(suggestions ...string) huh.Field {
			GinkgoHelper()
			choices := clikit.FormChoices{Suggestions: map[string][]string{"source": suggestions}}
			source := clitest.NamedField(annotation.AddDefinition().FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices), "source")
			source.WithKeyMap(huh.NewDefaultKeyMap())
			source.Focus()

			return source
		}
		tagWith := func(suggestions ...string) huh.Field {
			GinkgoHelper()
			choices := clikit.FormChoices{Suggestions: map[string][]string{"tag": suggestions}}
			tag := clitest.NamedField(event.AddDefinition().FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices), "tag")
			tag.WithKeyMap(huh.NewDefaultKeyMap())
			tag.Focus()

			return tag
		}
		It("shows the first five suggestions under their label while nothing is typed", func() {
			Expect(clitest.FieldView(sourceWith(seven...))).To(SatisfyAll(
				ContainSubstring("💡 Suggestions: db-1 · db-2 · db-3 · db-4 · db-5"), Not(ContainSubstring("db-6"))))
		})

		It("shows the suggestions typed text starts, in their order, ignoring case", func() {
			source := sourceWith("Mongod", "slack", "mongos", "db-1")

			clitest.TypeText(source, "MON")

			Expect(clitest.FieldView(source)).To(SatisfyAll(ContainSubstring(clikit.SuggestionsLabel+"Mongod · mongos"), Not(ContainSubstring("slack"))))
		})

		It("keeps the match up and down cycle to in its window", func() {
			source := sourceWith(seven...)
			clitest.TypeText(source, "db")

			clitest.Press(source, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown)
			Expect(clitest.FieldView(source)).To(ContainSubstring(clikit.SuggestionsLabel + "db-3 · db-4 · db-5 · db-6 · db-7"))
			clitest.CtrlE(source)

			Expect(source.GetValue()).To(Equal("db-7"))
			Expect(clitest.FieldView(source)).To(ContainSubstring(clikit.SuggestionsLabel + "db-7"))
		})

		It("wraps up and down around the matches, as the inline suggestion does", func() {
			down := sourceWith("db-1", "db-2", "db-3")
			clitest.TypeText(down, "db")
			clitest.Press(down, tea.KeyDown, tea.KeyDown, tea.KeyDown)
			clitest.CtrlE(down)
			Expect(down.GetValue()).To(Equal("db-1"))

			up := sourceWith(seven...)
			clitest.TypeText(up, "db")
			clitest.Press(up, tea.KeyUp)
			Expect(clitest.FieldView(up)).To(ContainSubstring(clikit.SuggestionsLabel + "db-3 · db-4 · db-5 · db-6 · db-7"))
			clitest.CtrlE(up)
			Expect(up.GetValue()).To(Equal("db-7"))
		})

		It("shows the first five suggestions again once a list field adds what was typed on leaving", func() {
			choices := clikit.FormChoices{Suggestions: map[string][]string{"tag": {"election", "escalation", "edge"}}}
			tag := clitest.NamedField(event.AddDefinition().FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices), "tag")
			tag.WithKeyMap(huh.NewDefaultKeyMap())
			tag.Focus()
			clitest.TypeText(tag, "el")

			tag.Blur()

			Expect(clitest.FieldView(tag)).To(ContainSubstring(clikit.SuggestionsLabel + "election · escalation · edge"))
		})

		It("matches what a paste leaves in a list field", func() {
			tag := tagWith("election", "escalation", "edge")
			clitest.TypeText(tag, "e")
			tag.Update(tea.PasteMsg{Content: "s"})

			clitest.CtrlE(tag)
			clitest.Press(tag, tea.KeyEnter)

			Expect(tag.GetValue()).To(Equal([]string{"escalation"}))
		})

		It("takes nothing on ctrl+e once a paste runs past every match", func() {
			tag := tagWith("election", "escalation", "edge")
			clitest.TypeText(tag, "ed")
			tag.Update(tea.PasteMsg{Content: "gexyz"})

			Expect(func() { clitest.CtrlE(tag) }).NotTo(Panic())
			clitest.Press(tag, tea.KeyEnter)

			Expect(tag.GetValue()).To(Equal([]string{"edgexyz"}))
		})

		It("opens a suggested field that comes before the tz field", func() {
			source := clikit.Field{Name: "source", Usage: "System it came from", Check: clikit.WithoutEnv(clikit.CheckSource)}
			def := clikit.Definition{Fields: []clikit.Field{source, clikit.TzField()}}
			choices := clikit.FormChoices{Suggestions: map[string][]string{"source": {"mongod"}}}

			Expect(func() { def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices) }).NotTo(Panic())
		})

		It("shows no row when nothing matches", func() {
			source := sourceWith(seven...)

			clitest.TypeText(source, "zz")

			Expect(clitest.FieldView(source)).NotTo(ContainSubstring(clikit.SuggestionsLabel))
		})

		It("names the cycling keys in its help only while something matches", func() {
			source := sourceWith(seven...)
			clitest.TypeText(source, "db")
			Expect(clitest.HelpKeys(source)).To(ContainElements("↑/↓ cycle matches", "ctrl+n/ctrl+p cycle"))

			clitest.TypeText(source, "z")
			Expect(clitest.HelpKeys(source)).NotTo(ContainElement("↑/↓ cycle matches"))
		})

		It("binds ctrl+e once on every field of the form commands", func() {
			choices := clikit.FormChoices{Suggestions: map[string][]string{"source": {"mongod"}, "tag": {"election"}}}
			for _, def := range []clikit.Definition{
				event.AddDefinition(), annotation.AddDefinition(), timelinecmd.CreateDefinition(), timelinecmd.EditDefinition(),
			} {
				in := clikit.FieldValues{"source": {"mongo"}, "name": {"Mongo Outage"}}
				for _, formField := range def.FormFields(clitest.FormCheckEnv(), in, choices) {
					formField.WithKeyMap(huh.NewDefaultKeyMap())
					bound := 0
					for _, binding := range formField.KeyBinds() {
						if slices.Contains(binding.Keys(), "ctrl+e") {
							bound++
						}
					}
					Expect(bound).To(BeNumerically("<=", 1), formField.GetKey())
				}
			}
		})
	})

	Describe("a completing list field", func() {
		listing := map[string][]string{"": {"docs/", "files/"}, "files/": {"files/p99.png", "files/q01.png"}}
		pathWith := func(entries map[string][]string) huh.Field {
			GinkgoHelper()
			complete := func(typed string) []string {
				dir := typed[:strings.LastIndex(typed, "/")+1]
				return slices.DeleteFunc(slices.Clone(entries[dir]), func(entry string) bool {
					return !strings.HasPrefix(entry, typed)
				})
			}
			def := clikit.Definition{Fields: []clikit.Field{{Name: "path", Typ: clikit.StringsField, List: true, Usage: "Files to attach"}}}
			choices := clikit.FormChoices{Completions: map[string]func(string) []string{"path": complete}}
			path := clitest.NamedField(def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices), "path")
			path.WithKeyMap(huh.NewDefaultKeyMap())
			path.Focus()

			return path
		}

		It("shows what its completion returns for what is typed, in the row and as the ghost", func() {
			path := pathWith(listing)

			clitest.TypeText(path, "fi")

			Expect(clitest.FieldView(path)).To(SatisfyAll(ContainSubstring(clikit.SuggestionsLabel+"files/"), ContainSubstring("> files/")))
		})

		It("completes what a paste leaves", func() {
			path := pathWith(listing)
			clitest.TypeText(path, "files/")

			path.Update(tea.PasteMsg{Content: "q"})

			Expect(clitest.FieldView(path)).To(ContainSubstring("> files/q01.png"))
		})

		It("takes a real completion on ctrl+e after a paste runs past the match typed", func() {
			path := pathWith(listing)
			clitest.TypeText(path, "fi")
			path.Update(tea.PasteMsg{Content: "les/p99"})

			Expect(func() { clitest.CtrlE(path) }).NotTo(Panic())
			clitest.Press(path, tea.KeyEnter)

			Expect(path.GetValue()).To(Equal([]string{"files/p99.png"}))
		})

		It("completes nothing again for a key that leaves the typed text as it was", func() {
			calls := 0
			complete := func(string) []string { calls++; return []string{"files/p99.png", "files/q01.png"} }
			def := clikit.Definition{Fields: []clikit.Field{{Name: "path", Typ: clikit.StringsField, List: true, Usage: "Files to attach"}}}
			choices := clikit.FormChoices{Completions: map[string]func(string) []string{"path": complete}}
			path := clitest.NamedField(def.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, choices), "path")
			path.WithKeyMap(huh.NewDefaultKeyMap())
			path.Focus()
			clitest.TypeText(path, "files/")
			before := calls

			clitest.Press(path, tea.KeyDown, tea.KeyUp, tea.KeyLeft)

			Expect(calls).To(Equal(before))
		})

		It("completes again from what taking the ghost leaves", func() {
			path := pathWith(listing)
			clitest.TypeText(path, "fi")

			clitest.CtrlE(path)

			Expect(clitest.FieldView(path)).To(SatisfyAll(
				ContainSubstring("> files/"), ContainSubstring(clikit.SuggestionsLabel+"files/p99.png · files/q01.png")))
		})

		It("completes the empty input again after an add", func() {
			path := pathWith(listing)
			clitest.TypeText(path, "files/p99.png")
			clitest.Press(path, tea.KeyEnter)

			clitest.TypeText(path, "d")

			Expect(clitest.FieldView(path)).To(ContainSubstring(clikit.SuggestionsLabel + "docs/"))
		})

		It("ghosts nothing over the empty input after an add when nothing completes it", func() {
			path := pathWith(map[string][]string{"/x/": {"/x/a.png"}})
			clitest.TypeText(path, "/x/a")

			clitest.Press(path, tea.KeyEnter)

			Expect(path.GetValue()).To(Equal([]string{"/x/a"}))
			Expect(clitest.FieldView(path)).NotTo(ContainSubstring(".png"))
		})

		It("cycles the matches with up and down while text is typed, and picks a value while none is", func() {
			path := pathWith(listing)
			clitest.TypeText(path, "docs/")
			clitest.Press(path, tea.KeyEnter)

			clitest.TypeText(path, "files/")
			Expect(clitest.HelpKeys(path)).To(ContainElement("↑/↓ cycle matches"))
			clitest.Press(path, tea.KeyDown)
			Expect(clitest.FieldView(path)).To(ContainSubstring("> files/q01.png"))

			for range "files/" {
				clitest.Press(path, tea.KeyBackspace)
			}
			Expect(clitest.HelpKeys(path)).To(ContainElement("↑/↓ pick"))
		})
	})

	It("replace a rejected value with the one its field suggests on ctrl+e, clearing its error", func() {
		names := clikit.Definition{Fields: []clikit.Field{{Name: "name", Arg: true, Check: clikit.WithoutEnv(timelinecmd.CheckName), Suggest: timelinecmd.SuggestName}}}
		name := clitest.NamedField(names.FormFields(clitest.FormCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{}), "name")
		name.WithKeyMap(huh.NewDefaultKeyMap())
		clitest.TypeInto(name, "Mongo Outage")
		clitest.Press(name, tea.KeyEnter)
		Expect(name.Error()).To(HaveOccurred())

		name.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})

		Expect(clitest.FieldText([]huh.Field{name}, "name")).To(Equal("mongo-outage"))
		Expect(name.Error()).NotTo(HaveOccurred())
	})
})

var _ = DescribeTable("suggestName offers a valid timeline name only for a rejected one",
	func(name, want string) {
		Expect(timelinecmd.SuggestName(name)).To(Equal(want))
	},
	Entry("spaces and capitals", "Mongo Outage", "mongo-outage"),
	Entry("a valid name", "mongo-outage", ""),
	Entry("nothing a name can hold", "!!!", ""),
)
