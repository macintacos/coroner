package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("add forms", func() {
	DescribeTable("open on a terminal when --interactive asks, or when the command line is not ready and no machine format is asked for",
		func(gate formGate, wantOpen bool) {
			Expect(formMode(gate)).To(Equal(wantOpen))
		},
		Entry("a required field missing on a terminal", formGate{terminal: true}, true),
		Entry("--interactive with every required field on a terminal", formGate{interactive: true, ready: true, terminal: true}, true),
		Entry("--interactive with a required field missing on a terminal", formGate{interactive: true, terminal: true}, true),
		Entry("every required field on a terminal", formGate{ready: true, terminal: true}, false),
		Entry("a required field missing under a machine format on a terminal", formGate{machine: true, terminal: true}, false),
		Entry("--interactive under a machine format on a terminal", formGate{interactive: true, machine: true, terminal: true}, true),
		Entry("a required field missing off a terminal", formGate{}, false),
		Entry("every required field off a terminal", formGate{ready: true}, false),
	)

	DescribeTable("ask a form again when its submission is rejected as usage, not found or a taken name, but not for the timeline it writes to",
		func(err error, want bool) {
			Expect(correctable(err)).To(Equal(want))
		},
		Entry("usage", &Error{Kind: KindUsage}, true),
		Entry("an invalid time", &Error{Kind: KindInvalidTime}, true),
		Entry("an event not found", &Error{Kind: KindEventNotFound}, true),
		Entry("no timeline selected", &Error{Kind: KindTimelineRequired}, false),
		Entry("a timeline not found", &Error{Kind: KindTimelineNotFound}, false),
		Entry("a timeline that exists", &Error{Kind: KindTimelineExists}, true),
		Entry("an error of no kind", errors.New("disk full"), false),
	)

	It("read times in the local zone when the config file's default_timezone is unknown", func() {
		env := baseCheckEnv(formSession{app: App{Now: testNow}, cfg: config.Config{DefaultTimezone: "Mars/Olympus_Mons"}})

		Expect(env.zone.loc).To(Equal(testNow().Location()))
	})

	DescribeTable("find a terminal only when stdin and stdout are one, TERM can draw and input is not turned off",
		func(inTTY, outTTY bool, term string, inputOff, want bool) {
			app := App{InIsTTY: inTTY, OutIsTTY: outTTY, LookupEnv: func(key string) (string, bool) {
				return term, key == "TERM"
			}}

			Expect(interactiveTerminal(app, inputOff)).To(Equal(want))
		},
		Entry("both terminals", true, true, "xterm-256color", false, true),
		Entry("stdin piped", false, true, "xterm-256color", false, false),
		Entry("stdout piped", true, false, "xterm-256color", false, false),
		Entry("a dumb terminal", true, true, "dumb", false, false),
		Entry("input turned off", true, true, "xterm-256color", true, false),
	)

	DescribeTable("turn input off with --no-input, or CORONER_NO_INPUT when the flag is not given",
		func(env map[string]string, args []string, want bool) {
			app := App{LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			}}
			cmd := &cobra.Command{}
			cmd.Flags().AddFlagSet(globalFlags())
			Expect(cmd.ParseFlags(args)).To(Succeed())

			Expect(noInput(cmd, app)).To(Equal(want))
		},
		Entry("neither", nil, nil, false),
		Entry("--no-input", nil, []string{"--no-input"}, true),
		Entry("CORONER_NO_INPUT set", map[string]string{"CORONER_NO_INPUT": "1"}, nil, true),
		Entry("CORONER_NO_INPUT false", map[string]string{"CORONER_NO_INPUT": "false"}, nil, false),
		Entry("--no-input=false over CORONER_NO_INPUT", map[string]string{"CORONER_NO_INPUT": "1"}, []string{"--no-input=false"}, false),
	)

	It("abort the form on Esc and record it as cancelled, passing other keys through", func() {
		cancelled := false
		text := huh.NewInput().Key("text")
		filter := escCancels(&cancelled, []huh.Field{text})
		form := huh.NewForm(huh.NewGroup(text))
		letter := tea.KeyPressMsg{Code: 'a', Text: "a"}

		Expect(filter(nil, letter)).To(Equal(letter))
		Expect(cancelled).To(BeFalse())

		form.Update(filter(nil, tea.KeyPressMsg{Code: tea.KeyEscape}))
		Expect(cancelled).To(BeTrue())
		Expect(form.State).To(Equal(huh.StateAborted))
	})

	It("pass Esc through to a select filtering its options", func() {
		cancelled := false
		pick := huh.NewSelect[string]().Key("event").Options(huh.NewOptions("1", "2")...)
		pick.WithKeyMap(huh.NewDefaultKeyMap())
		pick.Focus()
		pick.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		Expect(pick.GetFiltering()).To(BeTrue())
		esc := tea.KeyPressMsg{Code: tea.KeyEscape}

		Expect(escCancels(&cancelled, []huh.Field{pick})(nil, esc)).To(Equal(esc))
		Expect(cancelled).To(BeFalse())
	})

	It("carry forward everything but the fields another entry clears", func() {
		values := fieldValues{
			"timeline": {"mongo-outage"}, "text": {"Primary stepped down"}, "at": {"03:14"}, "ended": {"03:20"},
			"approximate": {"true"}, "source": {"mongod"}, "tag": {"election"}, "url": {"https://example.com/1"},
			"author": {"sre"}, "tz": {"UTC"},
		}

		Expect(carryForward(values, eventAddDefinition())).To(Equal(fieldValues{
			"timeline": {"mongo-outage"}, "source": {"mongod"}, "tag": {"election"}, "author": {"sre"}, "tz": {"UTC"},
		}))
		Expect(values).To(HaveKey("text"))
	})

	Describe("session", func() {
		var (
			out, errOut bytes.Buffer
			app         App
			db          string
			ids         int
			asked       [][]huh.Field
		)

		// runArgs runs exactly args through Run.
		runArgs := func(ctx SpecContext, args ...string) int {
			GinkgoHelper()
			out.Reset()
			errOut.Reset()
			a := app
			a.Args = args
			return Run(ctx, a)
		}
		run := func(ctx SpecContext, args ...string) int {
			GinkgoHelper()
			return runArgs(ctx, append([]string{"--db", db}, args...)...)
		}

		// eventOptions returns what the annotation form's event field picks from in mongo-outage.
		eventOptions := func(ctx SpecContext) []huh.Option[string] {
			GinkgoHelper()
			var choices formChoices
			Expect(withStorePath(ctx, app, db, func(s *store.Store) error {
				t, err := s.TimelineByName(ctx, "mongo-outage")
				if err != nil {
					return err
				}
				choices, err = annotationChoices(ctx, s, t, zone{loc: time.UTC, label: "UTC"})
				return err
			})).To(Succeed())

			return choices.options["event"]
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

		// runForm runs args against a root holding parent, whose add command runs its form as RunE does
		// on a terminal.
		runForm := func(ctx SpecContext, parent *cobra.Command, args ...string) error {
			GinkgoHelper()
			out.Reset()
			errOut.Reset()
			root := &cobra.Command{Use: "coroner", SilenceErrors: true, SilenceUsage: true}
			root.PersistentFlags().AddFlagSet(globalFlags())
			root.AddCommand(parent)
			root.SetArgs(append([]string{"--db", db}, args...))
			return root.ExecuteContext(ctx)
		}

		addCommand := func() *cobra.Command {
			return &cobra.Command{Use: "add", Annotations: map[string]string{formatsAnnotation: "text,json"}}
		}
		eventAdd := func(ctx SpecContext, forms func(bool, graphics.Protocol) askFunc) *cobra.Command {
			event := &cobra.Command{Use: "event"}
			addTimelineFlag(event.PersistentFlags())
			event.AddCommand(eventAddForm().attach(ctx, addCommand(), app, forms))
			return event
		}
		annotationAdd := func(ctx SpecContext, forms func(bool, graphics.Protocol) askFunc) *cobra.Command {
			annotation := &cobra.Command{Use: "annotation"}
			annotation.AddCommand(annotationAddForm().attach(ctx, addCommand(), app, forms))
			return annotation
		}

		BeforeEach(func(ctx SpecContext) {
			ids = 0
			asked = nil
			home := GinkgoT().TempDir()
			db = filepath.Join(home, "coroner.db")
			app = scratchApp(home, &out, &errOut, func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			})
			app.InIsTTY, app.OutIsTTY = true, true

			Expect(run(ctx, "timeline", "create", "mongo-outage")).To(Equal(0), errOut.String())
		})

		When("a timeline is in use", func() {
			BeforeEach(func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())
			})

			It("open --interactive prefilled from the command line's arguments and flags", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(cancel, decline))

				Expect(runForm(ctx, command, "event", "add", "Primary stepped down", "--interactive", "--source", "mongod")).To(Succeed())

				Expect(fieldText(asked[0], "text")).To(Equal("Primary stepped down"))
				Expect(fieldText(asked[0], "source")).To(Equal("mongod"))
			})

			It("write what the form submits and echo a command that dry-runs to the same event", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(submit(map[string]string{
					"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z", "source": "mongod", "tag": "election\nphase:detect",
				}), decline))

				Expect(runForm(ctx, command, "event", "add", "--interactive", "--json")).To(Succeed())
				written := withoutRecordKeys(out.Bytes())

				Expect(runArgs(ctx, append(echoedArgs(ctx, errOut.String())[1:], "--dry-run", "--json")...)).To(Equal(0), errOut.String())
				Expect(withoutRecordKeys(out.Bytes())).To(Equal(written))
				Expect(written).To(HaveKeyWithValue("text", "Primary stepped down"))
			})

			It("open when a required flag is missing, prefilled from the argument", func(ctx SpecContext) {
				command := annotationAdd(ctx, answers(cancel, decline))

				Expect(runForm(ctx, command, "annotation", "add", "replSetStepDown")).To(Succeed())

				Expect(asked).NotTo(BeEmpty())
				Expect(fieldText(asked[0], "body")).To(Equal("replSetStepDown"))
			})

			It("open with no kind chosen", func(ctx SpecContext) {
				command := annotationAdd(ctx, answers(cancel, decline))

				Expect(runForm(ctx, command, "annotation", "add")).To(Succeed())

				Expect(fieldText(asked[0], "kind")).To(BeEmpty())
			})

			It("keep an event given by id prefix, and write the annotation on it", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Primary stepped down", "--json")).To(Equal(0), errOut.String())
				var event struct {
					ID string `json:"id"`
				}
				Expect(json.Unmarshal(out.Bytes(), &event)).To(Succeed())
				prefix := event.ID[:8]
				command := annotationAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					Expect(fieldText(fields, "event")).To(Equal(prefix))
					return true, nil
				}, decline))

				Expect(runForm(ctx, command, "annotation", "add", "--interactive", "-e", prefix, "--kind", "evidence", "body", "--json")).
					To(Succeed())

				Expect(withoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("event_id", event.ID))
			})

			It("write an annotation on the picked event and echo a command that dry-runs to the same one", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z")).To(Equal(0), errOut.String())
				var offered []string
				command := annotationAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					offered = optionValues(namedField(fields, "event"))
					return submit(map[string]string{"body": "replSetStepDown", "event": "1", "kind": "evidence"})(fields)
				}, decline))

				Expect(runForm(ctx, command, "annotation", "add", "--interactive", "--json")).To(Succeed())
				written := withoutRecordKeys(out.Bytes())

				Expect(offered).To(Equal([]string{"", "1"}))
				Expect(runArgs(ctx, append(echoedArgs(ctx, errOut.String())[1:], "--dry-run", "--json")...)).To(Equal(0), errOut.String())
				Expect(withoutRecordKeys(out.Bytes())).To(Equal(written))
				Expect(written).To(HaveKey("event_id"))
			})

			It("open another entry with the timeline, source and tags kept, and the text, time and url cleared",
				func(ctx SpecContext) {
					entry := map[string]string{
						"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z", "source": "mongod", "tag": "election",
						"url": "https://example.com/1",
					}
					command := eventAdd(ctx, answers(submit(entry), accept, cancel, decline))

					Expect(runForm(ctx, command, "event", "add", "-t", "mongo-outage")).To(Succeed())

					second := asked[2]
					Expect(fieldText(second, "source")).To(Equal("mongod"))
					Expect(fieldText(second, "tag")).To(Equal("election"))
					Expect(fieldText(second, "text")).To(BeEmpty())
					Expect(fieldText(second, "at")).To(BeEmpty())
					Expect(fieldText(second, "url")).To(BeEmpty())
					Expect(errOut.String()).To(ContainSubstring("-t mongo-outage"))
				})

			It("write nothing and report interrupted when Ctrl-C ends the form", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					fill(fields, map[string]string{"text": "Primary stepped down"})
					return false, huh.ErrUserAborted
				}))

				err := runForm(ctx, command, "event", "add")

				Expect(err).To(HaveField("Kind", KindInterrupted))
				Expect(run(ctx, "event", "list", "--json")).To(Equal(0), errOut.String())
				Expect(withoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("total", 0.0))
			})

			It("skip an entry Esc cancels and end when no other is wanted", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(cancel, decline))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(asked).To(HaveLen(2))
				Expect(errOut.String()).NotTo(ContainSubstring("Equivalent command"))
			})

			It("ask the entry again, as typed, when the add rejects it", func(ctx SpecContext) {
				entry := map[string]string{"text": "Deploy", "at": "2026-09-11T04:00:00Z", "ended": "2026-09-11T03:00:00Z"}
				command := eventAdd(ctx, answers(submit(entry), cancel, decline))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(errOut.String()).To(ContainSubstring("--ended"))
				Expect(fieldText(asked[1], "ended")).To(Equal("2026-09-11T03:00:00Z"))
			})

			It("print a rejected entry's error for a person, even under --json", func(ctx SpecContext) {
				app.Args = []string{"--db", db, "event", "add", "--json"}
				entry := map[string]string{"text": "Deploy", "at": "2026-09-11T04:00:00Z", "ended": "2026-09-11T03:00:00Z"}
				command := eventAdd(ctx, answers(submit(entry), cancel, decline))

				Expect(runForm(ctx, command, "event", "add", "--interactive", "--json")).To(Succeed())

				Expect(errOut.String()).To(ContainSubstring("--ended"))
				Expect(errOut.String()).NotTo(ContainSubstring(`"kind"`))
			})
		})

		It("end without asking more when Esc cancels the timeline pick", func(ctx SpecContext) {
			command := eventAdd(ctx, answers(cancel))

			Expect(runForm(ctx, command, "event", "add")).To(Succeed())

			Expect(asked).To(HaveLen(1))
		})

		It("label each event to pick on one printable line", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "\x1b[31mred\x1b[0m\r\nsecond line", "-t", "mongo-outage")).To(Equal(0), errOut.String())

			label := eventOptions(ctx)[1].Key

			Expect(label).NotTo(ContainSubstring("\x1b"))
			Expect(label).NotTo(ContainSubstring("\r"))
			Expect(label).To(HaveSuffix("red second line"))
		})

		It("pick from the last 200 events", func(ctx SpecContext) {
			for i := range 201 {
				at := fmt.Sprintf("2026-09-11T03:%02d:%02dZ", i/60, i%60)
				Expect(run(ctx, "event", "add", "Probe", "-t", "mongo-outage", "--at", at)).To(Equal(0), errOut.String())
			}

			options := eventOptions(ctx)

			Expect(options).To(HaveLen(201))
			Expect(options[1].Value).To(Equal("2"))
			Expect(options[200].Value).To(Equal("201"))
		})

		It("ask for the timeline when none is selected, and echo it", func(ctx SpecContext) {
			command := eventAdd(ctx, answers(
				func(fields []huh.Field) (bool, error) {
					Expect(fields).To(HaveLen(1))
					Expect(optionValues(namedField(fields, "timeline"))).To(Equal([]string{"mongo-outage"}))
					return submit(map[string]string{"timeline": "mongo-outage"})(fields)
				},
				submit(map[string]string{"text": "Pager fires"}),
				decline,
			))

			Expect(runForm(ctx, command, "event", "add")).To(Succeed())

			Expect(errOut.String()).To(ContainSubstring(" event add 'Pager fires' -t mongo-outage"))
		})

		It("refuse --interactive off a terminal before opening anything", func(ctx SpecContext) {
			app.InIsTTY = false

			Expect(run(ctx, "event", "add", "Pager fires", "--interactive")).To(Equal(2))
			Expect(errOut.String()).To(ContainSubstring("--interactive needs a terminal"))
		})

		DescribeTable("run the command line as given on a terminal, asking nothing, when input is turned off or a machine format is asked for",
			func(ctx SpecContext, env map[string]string, args ...string) {
				lookupEnv := app.LookupEnv
				app.LookupEnv = func(key string) (string, bool) {
					if value, ok := env[key]; ok {
						return value, true
					}
					return lookupEnv(key)
				}

				err := runForm(ctx, eventAdd(ctx, answers()), append([]string{"event", "add"}, args...)...)

				Expect(err).To(HaveField("Kind", KindUsage))
				Expect(err).To(HaveField("Message", "event add needs the event's text"))
			},
			Entry("--no-input", nil, "--no-input"),
			Entry("CORONER_NO_INPUT", map[string]string{"CORONER_NO_INPUT": "1"}),
			Entry("--json", nil, "--json"),
		)

		DescribeTable("refuse --interactive when input is turned off, naming what turned it off, before opening anything",
			func(ctx SpecContext, env map[string]string, flags []string, message string) {
				app.LookupEnv = func(key string) (string, bool) {
					value, ok := env[key]
					return value, ok
				}

				err := runForm(ctx, eventAdd(ctx, answers()), append([]string{"event", "add", "Pager fires", "--interactive"}, flags...)...)

				Expect(err).To(HaveField("Kind", KindUsage))
				Expect(err).To(HaveField("Message", message))
			},
			Entry("--no-input", nil, []string{"--no-input"}, "--interactive conflicts with --no-input"),
			Entry("CORONER_NO_INPUT", map[string]string{"CORONER_NO_INPUT": "1"}, nil, "--interactive conflicts with CORONER_NO_INPUT"),
		)
	})

	DescribeTable("preview a time as event add --dry-run --json resolves it",
		func(ctx SpecContext, input string) {
			var out, errOut bytes.Buffer
			home := GinkgoT().TempDir()
			app := scratchApp(home, &out, &errOut, func() (uuid.UUID, error) { return uuid.New(), nil })
			db := filepath.Join(home, "coroner.db")
			app.Args = []string{"--db", db, "timeline", "create", "mongo-outage"}
			Expect(Run(ctx, app)).To(Equal(0), errOut.String())
			out.Reset()
			app.Args = []string{"--db", db, "event", "add", "Pager fires", "-t", "mongo-outage", "--at", input, "--dry-run", "--json"}
			Expect(Run(ctx, app)).To(Equal(0), errOut.String())

			var resolved struct {
				OccurredAt  string `json:"occurred_at"`
				Precision   string `json:"precision"`
				Approximate bool   `json:"approximate"`
			}
			Expect(json.Unmarshal(out.Bytes(), &resolved)).To(Succeed())
			def := eventAddDefinition()
			at := def.fields[slices.IndexFunc(def.fields, func(f field) bool { return f.name == "at" })]
			preview := at.preview(input, formCheckEnv())
			occurredAt, err := timeline.ParseInstant(resolved.OccurredAt)
			Expect(err).NotTo(HaveOccurred())
			shown, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(strings.Fields(preview)[0], "~"))
			Expect(err).NotTo(HaveOccurred(), preview)

			Expect(shown).To(BeTemporally("==", occurredAt))
			Expect(preview).To(ContainSubstring(resolved.Precision + " precision"))
			Expect(strings.Contains(preview, "approximate")).To(Equal(resolved.Approximate))
		},
		Entry("RFC 3339", "2026-09-11T03:14:07.412Z"),
		Entry("a naive time", "2026-09-11 23:02"),
		Entry("natural language, approximately", "~ yesterday 23:02"),
		Entry("epoch seconds", "1789096447"),
	)
})

// scratchApp returns an App on home, with no config file, the fixed clock and a real store.
func scratchApp(home string, out, errOut io.Writer, newID func() (uuid.UUID, error)) App {
	return App{
		Out: out, Err: errOut,
		LookupEnv:         func(key string) (string, bool) { return map[string]string{"HOME": home}[key], key == "HOME" },
		ReadFile:          func(string) ([]byte, error) { return nil, fs.ErrNotExist },
		Now:               testNow,
		NewID:             newID,
		OpenStore:         store.Open,
		OpenStoreReadOnly: store.OpenReadOnly,
	}
}

// submit returns an answer that fills a form's fields with values and submits it.
func submit(values map[string]string) func([]huh.Field) (bool, error) {
	return func(fields []huh.Field) (bool, error) {
		fill(fields, values)
		return true, nil
	}
}

// cancel answers a form as Esc does.
func cancel([]huh.Field) (bool, error) { return false, nil }

// accept answers "add another" yes.
func accept(fields []huh.Field) (bool, error) {
	yes := true
	confirm, ok := fields[0].(*huh.Confirm)
	Expect(ok).To(BeTrue(), "expected the add-another confirm, got %T", fields[0])
	confirm.Value(&yes)
	return true, nil
}

// decline answers "add another" no.
func decline(fields []huh.Field) (bool, error) {
	no := false
	confirm, ok := fields[0].(*huh.Confirm)
	Expect(ok).To(BeTrue(), "expected the add-another confirm, got %T", fields[0])
	confirm.Value(&no)
	return true, nil
}

// fill sets each field named in values to that value, as typing and picking would.
func fill(fields []huh.Field, values map[string]string) {
	GinkgoHelper()
	for name, value := range values {
		switch f := namedField(fields, name).(type) {
		case *huh.Select[string]:
			f.Value(&value)
		case *huh.Confirm:
			b := value == "true"
			f.Value(&b)
		default:
			typeInto(f, value)
		}
	}
}

// fieldText returns the value the field called name opened with or holds, as text.
func fieldText(fields []huh.Field, name string) string {
	GinkgoHelper()
	return fmt.Sprint(namedField(fields, name).GetValue())
}

// optionValues returns the values a select picks from, read by moving through its options.
func optionValues(formField huh.Field) []string {
	GinkgoHelper()
	selectField, ok := formField.(*huh.Select[string])
	Expect(ok).To(BeTrue(), "form field %s is not a select", formField.GetKey())
	var values []string
	selectField.WithKeyMap(huh.NewDefaultKeyMap())
	selectField.Focus()
	for {
		value, _ := selectField.Hovered()
		if slices.Contains(values, value) {
			return values
		}
		values = append(values, value)
		selectField.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
}

// echoedArgs returns the args of the last equivalent command in stderr, split as a shell splits them.
func echoedArgs(ctx context.Context, stderr string) []string {
	GinkgoHelper()
	const prefix = "Equivalent command: "
	i := strings.LastIndex(stderr, prefix)
	Expect(i).NotTo(Equal(-1), "no equivalent command in %q", stderr)
	line, _, _ := strings.Cut(stderr[i+len(prefix):], "\n")
	split, err := exec.CommandContext(ctx, "sh", "-c", "printf '%s\\0' "+line).Output() //nolint:gosec // the spec hands the echoed line to a shell on purpose
	Expect(err).NotTo(HaveOccurred())

	return strings.Split(strings.TrimSuffix(string(split), "\x00"), "\x00")
}

// withoutRecordKeys returns the JSON object in data without the keys a new record gets.
func withoutRecordKeys(data []byte) map[string]any {
	GinkgoHelper()
	var object map[string]any
	Expect(json.Unmarshal(data, &object)).To(Succeed(), string(data))
	for _, key := range []string{"id", "seq", "created_at", "updated_at"} {
		delete(object, key)
	}

	return object
}
