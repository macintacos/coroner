package cli

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("form fields", func() {
	eventOptions := formChoices{options: map[string][]huh.Option[string]{
		"event": {huh.NewOption("none (the whole timeline)", ""), huh.NewOption("#12  Primary stepped down", "12")},
	}}
	kinds := []huh.Option[string]{huh.NewOption("evidence", "evidence"), huh.NewOption("comment", "comment")}
	kindOptions := formChoices{options: map[string][]huh.Option[string]{"kind": kinds}}
	every := fieldValues{
		"timeline": {"mongo-outage"}, "text": {"Primary stepped down"}, "tz": {"America/New_York"},
		"at": {"2026-09-11 03:14"}, "approximate": {"true"}, "source": {"mongod"},
		"tag": {"election", "phase:detect"}, "dry-run": {"true"},
	}

	DescribeTable("give back the values they hold as the command line would give them",
		func(def definition, in fieldValues, choices formChoices, want fieldValues) {
			Expect(def.formValues(def.formFields(formCheckEnv(), in, choices))).To(Equal(want))
		},
		Entry("every value they open with", eventAddDefinition(), every, formChoices{}, every),
		Entry("only the non-blank lines of a repeatable field", eventAddDefinition(),
			fieldValues{"tag": {"election", "", " ", "phase:detect"}}, formChoices{},
			fieldValues{"tag": {"election", "phase:detect"}}),
		Entry("nothing for a confirm left false", eventAddDefinition(),
			fieldValues{"approximate": {"false"}, "dry-run": {"false"}}, formChoices{}, fieldValues{}),
		Entry("nothing for an empty input", eventAddDefinition(),
			fieldValues{"url": {""}}, formChoices{}, fieldValues{}),
		Entry("nothing for a select on an empty option", annotationAddDefinition(),
			fieldValues{}, eventOptions, fieldValues{}),
		Entry("a select's chosen option", annotationAddDefinition(),
			fieldValues{"event": {"12"}}, eventOptions, fieldValues{"event": {"12"}}),
		Entry("a select's first option when it opens blank", annotationAddDefinition(),
			fieldValues{}, kindOptions, fieldValues{"kind": {"evidence"}}),
		Entry("an event no option holds, for its check to judge", annotationAddDefinition(),
			fieldValues{"event": {"mongo-outage#12"}}, eventOptions, fieldValues{"event": {"mongo-outage#12"}}),
		Entry("a kind no option holds, for its check to judge", annotationAddDefinition(),
			fieldValues{"kind": {"Evidence"}}, kindOptions, fieldValues{"kind": {"Evidence"}}),
		Entry("the positional argument as one value", annotationAddDefinition(),
			fieldValues{"body": {"Step-down lands 5 s after the COLLSCAN burst"}}, formChoices{},
			fieldValues{"body": {"Step-down lands 5 s after the COLLSCAN burst"}}),
	)

	It("mark a required field and refuse it blank", func() {
		choices := formChoices{options: map[string][]huh.Option[string]{
			"kind": append([]huh.Option[string]{huh.NewOption("choose…", "")}, kinds...),
		}}
		kind := namedField(annotationAddDefinition().formFields(formCheckEnv(), fieldValues{}, choices), "kind")

		kind.Blur()

		Expect(kind.Error()).To(SatisfyAll(HaveField("Kind", KindUsage), MatchError("kind is required")))
		Expect(fieldView(kind)).To(ContainSubstring("kind (required)"))
	})

	It("title each event add field for people", func() {
		fields := eventAddDefinition().formFields(formCheckEnv(), fieldValues{}, formChoices{})

		for name, title := range map[string]string{
			"text": "What happened (required)", "at": "When", "source": "Source", "tag": "Tags", "attach": "Files", "url": "url",
		} {
			Expect(fieldView(namedField(fields, name))).To(ContainSubstring(title), name)
		}
	})

	It("head each field the form commands ask with an icon before its title", func() {
		forms := []definition{
			eventAddDefinition(), annotationAddDefinition(),
			withoutField(timelineCreateDefinition(), "if-not-exists"), timelineEditFormDefinition(nil),
		}
		for _, def := range forms {
			for i, formField := range def.formFields(formCheckEnv(), fieldValues{}, formChoices{}) {
				f := def.fields[i]
				if f.unasked {
					continue
				}
				Expect(fieldIcon(f.name)).NotTo(BeEmpty(), f.name)
				Expect(fieldView(formField)).To(ContainSubstring(fieldIcon(f.name)+" "+f.formTitle()), f.name)
			}
		}
	})

	It("frame a form's view in a border as wide as asked, with its title set into the top edge", func() {
		lines := strings.Split(ansi.Strip(frame("📝 New event", "What happened\n> Primary stepped down", 40)), "\n")

		Expect(lines).To(HaveLen(4))
		Expect(lines[0]).To(HavePrefix("╭─ 📝 New event ─"))
		Expect(lines[1]).To(HavePrefix("│ What happened"))
		Expect(lines[2]).To(HavePrefix("│ > Primary stepped down"))
		Expect(lines[3]).To(HavePrefix("╰─"))
		for _, line := range lines {
			Expect(ansi.StringWidth(line)).To(Equal(40), line)
		}
	})

	It("clip a line wider than the frame's inside, so the frame keeps the rows the form planned", func() {
		lines := strings.Split(ansi.Strip(frame("📝 New event", strings.Repeat("x", 50)+"\n> typed", 40)), "\n")

		Expect(lines).To(HaveLen(4))
		Expect(lines[2]).To(HavePrefix("│ > typed"))
		for _, line := range lines {
			Expect(ansi.StringWidth(line)).To(Equal(40), line)
		}
	})

	It("frame a form at most 80 cells wide on a wider terminal", func() {
		form := newForm("📝 New event", formGroup{fields: []huh.Field{huh.NewInput().Title("What happened")}})

		form.Update(tea.WindowSizeMsg{Width: 200, Height: 20})

		for line := range strings.SplitSeq(strings.TrimLeft(ansi.Strip(form.View().Content), "\n"), "\n") {
			Expect(ansi.StringWidth(line)).To(Equal(80), line)
		}
	})

	It("draw a blank row between the output above a form and its frame", func() {
		form := newForm("📝 New event", formGroup{fields: []huh.Field{huh.NewInput().Title("What happened")}})

		form.Update(tea.WindowSizeMsg{Width: 60, Height: 20})

		lines := strings.Split(ansi.Strip(form.View().Content), "\n")
		Expect(lines[0]).To(BeEmpty())
		Expect(lines[1]).To(HavePrefix("╭─ 📝 New event"))
	})

	Describe("event add's groups", func() {
		def := eventAddDefinition()
		groupsFor := func(in fieldValues) []formGroup {
			return def.formGroups(def.formFields(formCheckEnv(), in, formChoices{}), in)
		}

		It("show the fields most events use first, and the rest behind More options", func() {
			groups := groupsFor(fieldValues{})

			Expect(groups).To(HaveLen(3))
			Expect(fieldKeys(groups[0].fields)).To(Equal([]string{"timeline", "text", "at", "source", "tag", "attach", "more-options"}))
			Expect(fieldKeys(groups[1].fields)).To(Equal([]string{"tz", "ended", "approximate", "url", "author", "dry-run"}))
			Expect(fieldView(namedField(groups[0].fields, "more-options"))).To(
				ContainSubstring("tz, ended, approximate, url, author, dry-run"))
		})

		It("keep the value given to a field the form does not ask", func() {
			in := fieldValues{"id": {"01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2"}}

			Expect(def.formValues(def.shownFields(groupsFor(in)))).To(HaveKeyWithValue("id", in["id"]))
		})

		It("hide the More options fields until it is Yes", func() {
			groups := groupsFor(fieldValues{})
			Expect(groups[1].hide()).To(BeTrue())

			fill(groups[0].fields, map[string]string{"more-options": "true"})

			Expect(groups[1].hide()).To(BeFalse())
		})

		It("open More options when a field behind it holds a value", func() {
			groups := groupsFor(fieldValues{"url": {"https://e.example"}})

			Expect(namedField(groups[0].fields, "more-options").GetValue()).To(BeTrue())
			Expect(groups[1].hide()).To(BeFalse())
		})
	})

	It("keep one group for annotation add, timeline create and timeline edit", func() {
		for _, def := range []definition{annotationAddDefinition(), timelineCreateDefinition(), timelineEditDefinition()} {
			fields := def.formFields(formCheckEnv(), fieldValues{}, formChoices{})

			groups := def.formGroups(fields, fieldValues{})

			Expect(groups).To(HaveLen(1))
			Expect(groups[0].fields).To(Equal(fields))
			Expect(groups[0].hide).To(BeNil())
		}
	})

	It("read a line holding only spaces as no value, and check only the others", func() {
		def := eventEditDefinition()
		fields := def.formFields(formCheckEnv(), fieldValues{}, formChoices{})
		tag := namedField(fields, "add-tag")
		typeInto(tag, "election\n  ")

		tag.Blur()

		Expect(tag.Error()).NotTo(HaveOccurred())
		Expect(def.formValues(fields)).To(HaveKeyWithValue("add-tag", []string{"election"}))
	})

	It("pick from options in a select, and suggest in an input", func() {
		choices := eventOptions
		choices.suggestions = map[string][]string{"source": {"mongod", "slack"}}
		fields := annotationAddDefinition().formFields(formCheckEnv(), fieldValues{}, choices)

		event := namedField(fields, "event")
		Expect(event).To(BeAssignableToTypeOf(&huh.Select[string]{}))
		Expect(fieldView(event)).To(ContainSubstring("#12  Primary stepped down"))

		var keys []string
		for _, binding := range namedField(fields, "source").WithKeyMap(huh.NewDefaultKeyMap()).KeyBinds() {
			keys = append(keys, binding.Help().Desc)
		}
		Expect(keys).To(ContainElement("complete"))
	})

	Describe("a time field", func() {
		// 02:30 on 2026-03-08 is a wall clock New York's DST change skips, and a real time in UTC.
		const gap = "2026-03-08 02:30"
		utc := checkEnv{now: testNow(), zone: zone{loc: time.UTC, label: "UTC"}}

		It("checks its time in the zone typed into the form", func() {
			fields := eventAddDefinition().formFields(utc, fieldValues{}, formChoices{})
			at := namedField(fields, "at")
			typeInto(at, gap)

			at.Blur()
			Expect(at.Error()).NotTo(HaveOccurred())

			typeInto(namedField(fields, "tz"), "America/New_York")
			at.Blur()
			Expect(at.Error()).To(HaveField("Kind", KindInvalidTime))
		})

		DescribeTable("previews its time under its help, in the zone typed into the form",
			func(tz, want string) {
				fields := eventAddDefinition().formFields(utc, fieldValues{}, formChoices{})
				at := namedField(fields, "at")
				typeInto(at, gap)
				typeInto(namedField(fields, "tz"), tz)

				Expect(renderedDescription(at)).To(SatisfyAll(ContainSubstring("When it happened"), ContainSubstring(want)))
			},
			Entry("the base zone with none typed", "", "2026-03-08T02:30:00Z (+00:00 UTC: 02:30:00), minute precision"),
			Entry("the gap's hint in a zone that skips it", "America/New_York", "-05:00"),
		)

		It("preview the time in the zone typed into the zone field", func() {
			fields := eventAddDefinition().formFields(utc, fieldValues{}, formChoices{})
			typeInto(namedField(fields, "at"), "03:25")
			tz := namedField(fields, "tz")
			typeInto(tz, "Asia/Tokyo")

			Expect(renderedDescription(tz)).To(ContainSubstring("(+09:00 Asia/Tokyo: 03:25:00)"))

			unknown := namedField(eventAddDefinition().formFields(utc, fieldValues{}, formChoices{}), "tz")
			typeInto(unknown, "Mars/Olympus")
			Expect(renderedDescription(unknown)).To(ContainSubstring(zoneHint))
		})

		It("shows a failing check's hint under the help of a field with no preview", func() {
			source := namedField(eventAddDefinition().formFields(utc, fieldValues{}, formChoices{}), "source")
			typeInto(source, "DB 2")

			Expect(renderedDescription(source)).To(ContainSubstring("Did you mean `db-2`?"))
		})
	})

	Describe("a list field", func() {
		rejectBad := withoutEnv(func(ref string) error {
			if ref == "Bad Ref" {
				return errors.New("bad ref")
			}
			return nil
		})
		refs := definition{fields: []field{{name: "ref", typ: stringsField, list: true, usage: "Ticket key or URL", check: rejectBad}}}

		It("adds each value typed and entered, once, after the values it opens with", func() {
			fields := refs.formFields(formCheckEnv(), fieldValues{"ref": {"INC-1"}}, formChoices{})
			ref := namedField(fields, "ref")

			enter(ref, "INC-2")
			enter(ref, "INC-1")

			Expect(refs.formValues(fields)).To(Equal(fieldValues{"ref": {"INC-1", "INC-2"}}))
			Expect(fieldView(ref)).To(SatisfyAll(ContainSubstring("Ticket key or URL"), ContainSubstring("INC-2")))
		})

		It("moves on when enter finds nothing typed, and stays while it adds", func() {
			ref := namedField(refs.formFields(formCheckEnv(), fieldValues{}, formChoices{}), "ref")
			ref.WithKeyMap(huh.NewDefaultKeyMap())

			typeInto(ref, "INC-1")
			_, stay := ref.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			_, next := ref.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

			Expect(stay).NotTo(BeNil())
			Expect(stay()).To(Equal(tea.RequestWindowSize()))
			Expect(next).NotTo(BeNil())
			Expect(next()).To(Equal(huh.NextField()))
		})

		It("removes the value up picks, counting back from the last", func() {
			fields := refs.formFields(formCheckEnv(), fieldValues{"ref": {"INC-1", "INC-2", "INC-3"}}, formChoices{})
			ref := namedField(fields, "ref")

			press(ref, tea.KeyUp, tea.KeyUp)
			Expect(fieldView(ref)).To(SatisfyAll(ContainSubstring("× INC-2"), ContainSubstring("✓ INC-3")))
			press(ref, tea.KeyBackspace)

			Expect(refs.formValues(fields)).To(Equal(fieldValues{"ref": {"INC-1", "INC-3"}}))
		})

		It("goes back to typing when down passes the last value", func() {
			fields := refs.formFields(formCheckEnv(), fieldValues{"ref": {"INC-1"}}, formChoices{})
			ref := namedField(fields, "ref")

			press(ref, tea.KeyUp, tea.KeyDown, tea.KeyBackspace)

			Expect(refs.formValues(fields)).To(Equal(fieldValues{"ref": {"INC-1"}}))
			Expect(fieldView(ref)).NotTo(ContainSubstring("×"))
		})

		It("adds a value left typed when it loses focus", func() {
			fields := refs.formFields(formCheckEnv(), fieldValues{}, formChoices{})
			ref := namedField(fields, "ref")
			typeInto(ref, "INC-1")

			ref.Blur()

			Expect(ref.Error()).NotTo(HaveOccurred())
			Expect(refs.formValues(fields)).To(Equal(fieldValues{"ref": {"INC-1"}}))
		})

		It("gives back a value it opens with unchanged, even one holding a newline", func() {
			in := fieldValues{"ref": {"INC-1\nfollow-up"}}

			Expect(refs.formValues(refs.formFields(formCheckEnv(), in, formChoices{}))).To(Equal(in))
		})

		It("shows the last five values while typing, and five around the picked one, counting the rest", func() {
			in := fieldValues{"ref": {"INC-1", "INC-2", "INC-3", "INC-4", "INC-5", "INC-6", "INC-7", "INC-8"}}
			ref := namedField(refs.formFields(formCheckEnv(), in, formChoices{}), "ref")

			Expect(fieldView(ref)).To(SatisfyAll(
				ContainSubstring("… 3 more"), ContainSubstring("INC-4"), ContainSubstring("INC-8"), Not(ContainSubstring("INC-3"))))

			press(ref, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyUp)
			Expect(fieldView(ref)).To(SatisfyAll(
				ContainSubstring("… 3 more"), ContainSubstring("× INC-3"), ContainSubstring("INC-7"), Not(ContainSubstring("INC-8"))))
		})

		It("shows in its help only the keys that act on what it holds", func() {
			ref := namedField(refs.formFields(formCheckEnv(), fieldValues{}, formChoices{}), "ref")
			ref.WithKeyMap(huh.NewDefaultKeyMap())
			Expect(helpKeys(ref)).To(Equal([]string{"shift+tab back", "enter submit", "enter next"}))

			typeInto(ref, "INC-1")
			Expect(helpKeys(ref)).To(Equal([]string{"enter add", "shift+tab back"}))

			press(ref, tea.KeyEnter)
			Expect(helpKeys(ref)).To(Equal([]string{"↑/↓ pick", "shift+tab back", "enter submit", "enter next"}))

			press(ref, tea.KeyUp)
			Expect(helpKeys(ref)).To(Equal([]string{"↑/↓ pick", "backspace remove", "shift+tab back", "enter submit", "enter next"}))
		})

		It("keeps its typing line on screen inside a form after a dozen values", func() {
			def := definition{fields: []field{{name: "name", arg: true, check: withoutEnv(checkName), suggest: suggestName}, refs.fields[0]}}
			form := huh.NewForm(huh.NewGroup(def.formFields(formCheckEnv(), fieldValues{}, formChoices{})...))
			size := tea.WindowSizeMsg{Width: 100, Height: 12}
			form.Init()
			form.Update(size)

			typeKeys(form, "Mongo Outage")
			form.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			form.Update(huh.NextField())
			for i := range 12 {
				typeKeys(form, fmt.Sprintf("INC-%d", i))
				pressResizing(form, tea.KeyEnter, size)
			}
			typeKeys(form, "TYPING-NOW")

			Expect(form.GetString("name")).To(Equal("mongo-outage"))
			Expect(ansi.Strip(form.View())).To(SatisfyAll(ContainSubstring("… 7 more"), ContainSubstring("TYPING-NOW")))
		})

		It("suggests nothing for the empty input after an add", func() {
			choices := formChoices{suggestions: map[string][]string{"ref": {"election", "escalation"}}}
			fields := refs.formFields(formCheckEnv(), fieldValues{}, choices)
			ref := namedField(fields, "ref")
			ref.WithKeyMap(huh.NewDefaultKeyMap())
			ref.Focus()

			typeText(ref, "e")
			press(ref, tea.KeyEnter)
			Expect(fieldView(ref)).NotTo(MatchRegexp(`> +election`))

			ref.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			press(ref, tea.KeyEnter)

			Expect(refs.formValues(fields)).To(Equal(fieldValues{"ref": {"e"}}))
		})

		It("picks values with up and down only while nothing is typed", func() {
			fields := refs.formFields(formCheckEnv(), fieldValues{"ref": {"INC-1"}}, formChoices{})
			ref := namedField(fields, "ref")
			typeInto(ref, "INC-2")

			press(ref, tea.KeyUp)

			Expect(fieldView(ref)).NotTo(ContainSubstring("×"))
		})

		It("picks timeline create's refs with up and down", func() {
			def := timelineCreateDefinition()
			fields := def.formFields(formCheckEnv(), fieldValues{"ref": {"INC-1", "INC-2"}}, formChoices{})
			ref := namedField(fields, "ref")

			press(ref, tea.KeyUp, tea.KeyBackspace)

			Expect(def.formValues(fields)["ref"]).To(Equal([]string{"INC-1"}))
		})

		It("refuses a value its check rejects, and adds nothing", func() {
			fields := refs.formFields(formCheckEnv(), fieldValues{}, formChoices{})
			ref := namedField(fields, "ref")

			enter(ref, "Bad Ref")

			Expect(ref.Error()).To(MatchError("bad ref"))
			Expect(refs.formValues(fields)).To(BeEmpty())
		})
	})

	Describe("a match row", func() {
		seven := []string{"db-1", "db-2", "db-3", "db-4", "db-5", "db-6", "db-7"}
		sourceWith := func(suggestions ...string) huh.Field {
			GinkgoHelper()
			choices := formChoices{suggestions: map[string][]string{"source": suggestions}}
			source := namedField(annotationAddDefinition().formFields(formCheckEnv(), fieldValues{}, choices), "source")
			source.WithKeyMap(huh.NewDefaultKeyMap())
			source.Focus()

			return source
		}
		tagWith := func(suggestions ...string) huh.Field {
			GinkgoHelper()
			choices := formChoices{suggestions: map[string][]string{"tag": suggestions}}
			tag := namedField(eventAddDefinition().formFields(formCheckEnv(), fieldValues{}, choices), "tag")
			tag.WithKeyMap(huh.NewDefaultKeyMap())
			tag.Focus()

			return tag
		}
		It("shows the first five suggestions while nothing is typed", func() {
			Expect(fieldView(sourceWith(seven...))).To(SatisfyAll(
				ContainSubstring("💡 Suggestions: db-1 · db-2 · db-3 · db-4 · db-5"), Not(ContainSubstring("db-6"))))
		})

		It("shows the suggestions typed text starts, in their order, ignoring case", func() {
			source := sourceWith("Mongod", "slack", "mongos", "db-1")

			typeText(source, "MON")

			Expect(fieldView(source)).To(SatisfyAll(ContainSubstring("💡 Suggestions: Mongod · mongos"), Not(ContainSubstring("slack"))))
		})

		It("keeps the match up and down cycle to in its window", func() {
			source := sourceWith(seven...)
			typeText(source, "db")

			press(source, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown)
			Expect(fieldView(source)).To(ContainSubstring("💡 Suggestions: db-3 · db-4 · db-5 · db-6 · db-7"))
			ctrlE(source)

			Expect(source.GetValue()).To(Equal("db-7"))
			Expect(fieldView(source)).To(ContainSubstring("💡 Suggestions: db-7"))
		})

		It("wraps up and down around the matches, as the inline suggestion does", func() {
			down := sourceWith("db-1", "db-2", "db-3")
			typeText(down, "db")
			press(down, tea.KeyDown, tea.KeyDown, tea.KeyDown)
			ctrlE(down)
			Expect(down.GetValue()).To(Equal("db-1"))

			up := sourceWith(seven...)
			typeText(up, "db")
			press(up, tea.KeyUp)
			Expect(fieldView(up)).To(ContainSubstring("💡 Suggestions: db-3 · db-4 · db-5 · db-6 · db-7"))
			ctrlE(up)
			Expect(up.GetValue()).To(Equal("db-7"))
		})

		It("shows the first five suggestions again once a list field adds what was typed on leaving", func() {
			choices := formChoices{suggestions: map[string][]string{"tag": {"election", "escalation", "edge"}}}
			tag := namedField(eventAddDefinition().formFields(formCheckEnv(), fieldValues{}, choices), "tag")
			tag.WithKeyMap(huh.NewDefaultKeyMap())
			tag.Focus()
			typeText(tag, "el")

			tag.Blur()

			Expect(fieldView(tag)).To(ContainSubstring("💡 Suggestions: election · escalation · edge"))
		})

		It("matches what a paste leaves in a list field", func() {
			tag := tagWith("election", "escalation", "edge")
			typeText(tag, "e")
			tag.Update(tea.PasteMsg{Content: "s"})

			ctrlE(tag)
			press(tag, tea.KeyEnter)

			Expect(tag.GetValue()).To(Equal([]string{"escalation"}))
		})

		It("takes nothing on ctrl+e once a paste runs past every match", func() {
			tag := tagWith("election", "escalation", "edge")
			typeText(tag, "ed")
			tag.Update(tea.PasteMsg{Content: "gexyz"})

			Expect(func() { ctrlE(tag) }).NotTo(Panic())
			press(tag, tea.KeyEnter)

			Expect(tag.GetValue()).To(Equal([]string{"edgexyz"}))
		})

		It("opens a suggested field that comes before the tz field", func() {
			source := field{name: "source", usage: "System it came from", check: withoutEnv(checkSource)}
			def := definition{fields: []field{source, tzField()}}
			choices := formChoices{suggestions: map[string][]string{"source": {"mongod"}}}

			Expect(func() { def.formFields(formCheckEnv(), fieldValues{}, choices) }).NotTo(Panic())
		})

		It("shows no row when nothing matches", func() {
			source := sourceWith(seven...)

			typeText(source, "zz")

			Expect(fieldView(source)).NotTo(ContainSubstring("💡 Suggestions:"))
		})

		It("names the cycling keys in its help only while something matches", func() {
			source := sourceWith(seven...)
			typeText(source, "db")
			Expect(helpKeys(source)).To(ContainElements("↑/↓ cycle matches", "ctrl+n/ctrl+p cycle"))

			typeText(source, "z")
			Expect(helpKeys(source)).NotTo(ContainElement("↑/↓ cycle matches"))
		})

		It("binds ctrl+e once on every field of the form commands", func() {
			choices := formChoices{suggestions: map[string][]string{"source": {"mongod"}, "tag": {"election"}}}
			for _, def := range []definition{
				eventAddDefinition(), annotationAddDefinition(), timelineCreateDefinition(), timelineEditDefinition(),
			} {
				in := fieldValues{"source": {"mongo"}, "name": {"Mongo Outage"}}
				for _, formField := range def.formFields(formCheckEnv(), in, choices) {
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
			def := definition{fields: []field{{name: "path", typ: stringsField, list: true, usage: "Files to attach"}}}
			choices := formChoices{completions: map[string]func(string) []string{"path": complete}}
			path := namedField(def.formFields(formCheckEnv(), fieldValues{}, choices), "path")
			path.WithKeyMap(huh.NewDefaultKeyMap())
			path.Focus()

			return path
		}

		It("shows what its completion returns for what is typed, in the row and as the ghost", func() {
			path := pathWith(listing)

			typeText(path, "fi")

			Expect(fieldView(path)).To(SatisfyAll(ContainSubstring("💡 Suggestions: files/"), ContainSubstring("> files/")))
		})

		It("completes what a paste leaves", func() {
			path := pathWith(listing)
			typeText(path, "files/")

			path.Update(tea.PasteMsg{Content: "q"})

			Expect(fieldView(path)).To(ContainSubstring("> files/q01.png"))
		})

		It("takes a real completion on ctrl+e after a paste runs past the match typed", func() {
			path := pathWith(listing)
			typeText(path, "fi")
			path.Update(tea.PasteMsg{Content: "les/p99"})

			Expect(func() { ctrlE(path) }).NotTo(Panic())
			press(path, tea.KeyEnter)

			Expect(path.GetValue()).To(Equal([]string{"files/p99.png"}))
		})

		It("completes nothing again for a key that leaves the typed text as it was", func() {
			calls := 0
			complete := func(string) []string { calls++; return []string{"files/p99.png", "files/q01.png"} }
			def := definition{fields: []field{{name: "path", typ: stringsField, list: true, usage: "Files to attach"}}}
			choices := formChoices{completions: map[string]func(string) []string{"path": complete}}
			path := namedField(def.formFields(formCheckEnv(), fieldValues{}, choices), "path")
			path.WithKeyMap(huh.NewDefaultKeyMap())
			path.Focus()
			typeText(path, "files/")
			before := calls

			press(path, tea.KeyDown, tea.KeyUp, tea.KeyLeft)

			Expect(calls).To(Equal(before))
		})

		It("completes again from what taking the ghost leaves", func() {
			path := pathWith(listing)
			typeText(path, "fi")

			ctrlE(path)

			Expect(fieldView(path)).To(SatisfyAll(
				ContainSubstring("> files/"), ContainSubstring("💡 Suggestions: files/p99.png · files/q01.png")))
		})

		It("completes the empty input again after an add", func() {
			path := pathWith(listing)
			typeText(path, "files/p99.png")
			press(path, tea.KeyEnter)

			typeText(path, "d")

			Expect(fieldView(path)).To(ContainSubstring("💡 Suggestions: docs/"))
		})

		It("ghosts nothing over the empty input after an add when nothing completes it", func() {
			path := pathWith(map[string][]string{"/x/": {"/x/a.png"}})
			typeText(path, "/x/a")

			press(path, tea.KeyEnter)

			Expect(path.GetValue()).To(Equal([]string{"/x/a"}))
			Expect(fieldView(path)).NotTo(ContainSubstring(".png"))
		})

		It("cycles the matches with up and down while text is typed, and picks a value while none is", func() {
			path := pathWith(listing)
			typeText(path, "docs/")
			press(path, tea.KeyEnter)

			typeText(path, "files/")
			Expect(helpKeys(path)).To(ContainElement("↑/↓ cycle matches"))
			press(path, tea.KeyDown)
			Expect(fieldView(path)).To(ContainSubstring("> files/q01.png"))

			for range "files/" {
				press(path, tea.KeyBackspace)
			}
			Expect(helpKeys(path)).To(ContainElement("↑/↓ pick"))
		})
	})

	It("replace a rejected value with the one its field suggests on ctrl+e, clearing its error", func() {
		names := definition{fields: []field{{name: "name", arg: true, check: withoutEnv(checkName), suggest: suggestName}}}
		name := namedField(names.formFields(formCheckEnv(), fieldValues{}, formChoices{}), "name")
		name.WithKeyMap(huh.NewDefaultKeyMap())
		typeInto(name, "Mongo Outage")
		press(name, tea.KeyEnter)
		Expect(name.Error()).To(HaveOccurred())

		name.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})

		Expect(fieldText([]huh.Field{name}, "name")).To(Equal("mongo-outage"))
		Expect(name.Error()).NotTo(HaveOccurred())
	})
})

var _ = DescribeTable("suggestName offers a valid timeline name only for a rejected one",
	func(name, want string) {
		Expect(suggestName(name)).To(Equal(want))
	},
	Entry("spaces and capitals", "Mongo Outage", "mongo-outage"),
	Entry("a valid name", "mongo-outage", ""),
	Entry("nothing a name can hold", "!!!", ""),
)

// ctrlE presses ctrl+e in formField.
func ctrlE(formField huh.Field) { formField.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}) }

// press sends formField each key code.
func press(formField huh.Field, codes ...rune) {
	for _, code := range codes {
		formField.Update(tea.KeyPressMsg{Code: code})
	}
}

// typeText presses a key in formField for each character of text.
func typeText(formField huh.Field, text string) {
	for _, r := range text {
		formField.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// typeKeys presses a key in form for each character of text.
func typeKeys(form *huh.Form, text string) {
	for _, r := range text {
		form.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// pressResizing presses code in form and, when the key asks for the window size, gives form size
// again, as the terminal would.
func pressResizing(form *huh.Form, code rune, size tea.WindowSizeMsg) {
	GinkgoHelper()
	_, cmd := form.Update(tea.KeyPressMsg{Code: code})
	if cmd != nil && reflect.DeepEqual(cmd(), tea.RequestWindowSize()) {
		form.Update(size)
	}
}

// helpKeys returns the enabled key bindings formField shows in its help, as "key description".
func helpKeys(formField huh.Field) []string {
	var keys []string
	for _, binding := range formField.KeyBinds() {
		if binding.Enabled() {
			keys = append(keys, binding.Help().Key+" "+binding.Help().Desc)
		}
	}

	return keys
}

// enter types value into formField and presses enter.
func enter(formField huh.Field, value string) {
	GinkgoHelper()
	typeInto(formField, value)
	press(formField, tea.KeyEnter)
}

// namedField returns the form field keyed name.
func namedField(fields []huh.Field, name string) huh.Field {
	GinkgoHelper()
	for _, formField := range fields {
		if formField.GetKey() == name {
			return formField
		}
	}

	Fail("no form field " + name)
	return nil
}

// fieldKeys returns each form field's key, in order.
func fieldKeys(fields []huh.Field) []string {
	keys := make([]string, len(fields))
	for i, formField := range fields {
		keys[i] = formField.GetKey()
	}

	return keys
}

// typeInto sets the text formField holds, as a fake form fills it.
func typeInto(formField huh.Field, value string) {
	GinkgoHelper()
	switch f := formField.(type) {
	case interface {
		Value(value *string) *huh.Input
	}:
		f.Value(&value)
	case *huh.Text:
		f.Value(&value)
	default:
		Fail(fmt.Sprintf("form field %s takes no text", formField.GetKey()))
	}
}

// renderedDescription returns formField's view once a form has run its description function.
func renderedDescription(formField huh.Field) string {
	GinkgoHelper()
	group := huh.NewGroup(formField)
	_, update := group.Update(nil)
	Expect(update).NotTo(BeNil(), "form field %s has no description function", formField.GetKey())
	group.Update(update())

	return fieldView(formField)
}

func fieldView(formField huh.Field) string {
	GinkgoHelper()
	view, ok := formField.WithWidth(400).(interface{ View() string })
	if !ok {
		Fail(fmt.Sprintf("form field %s has no view", formField.GetKey()))
	}

	return ansi.Strip(view.View())
}
