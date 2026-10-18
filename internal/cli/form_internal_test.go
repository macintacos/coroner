package cli

import (
	"fmt"
	"time"

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

	It("read a line holding only spaces as no value, and check only the others", func() {
		def := eventAddDefinition()
		fields := def.formFields(formCheckEnv(), fieldValues{}, formChoices{})
		tag := namedField(fields, "tag")
		typeInto(tag, "election\n  ")

		tag.Blur()

		Expect(tag.Error()).NotTo(HaveOccurred())
		Expect(def.formValues(fields)).To(Equal(fieldValues{"tag": {"election"}}))
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

		It("shows a failing check's hint under the help of a field with no preview", func() {
			source := namedField(eventAddDefinition().formFields(utc, fieldValues{}, formChoices{}), "source")
			typeInto(source, "DB 2")

			Expect(renderedDescription(source)).To(ContainSubstring("Did you mean `db-2`?"))
		})
	})
})

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

// typeInto sets the text formField holds, as a fake form fills it.
func typeInto(formField huh.Field, value string) {
	GinkgoHelper()
	switch f := formField.(type) {
	case *huh.Input:
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
