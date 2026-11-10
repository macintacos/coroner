package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing/fstest"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/annotation"
	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/clitest"
	"github.com/macintacos/coroner/internal/cli/event"
	timelinecmd "github.com/macintacos/coroner/internal/cli/timeline"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("add forms", func() {
	DescribeTable("open on a terminal when --interactive asks, or when the command line is not ready and no machine format is asked for",
		func(gate clikit.FormGate, wantOpen bool) {
			Expect(clikit.FormMode(gate)).To(Equal(wantOpen))
		},
		Entry("a required field missing on a terminal", clikit.FormGate{Terminal: true}, true),
		Entry("--interactive with every required field on a terminal", clikit.FormGate{Interactive: true, Ready: true, Terminal: true}, true),
		Entry("--interactive with a required field missing on a terminal", clikit.FormGate{Interactive: true, Terminal: true}, true),
		Entry("every required field on a terminal", clikit.FormGate{Ready: true, Terminal: true}, false),
		Entry("a required field missing under a machine format on a terminal", clikit.FormGate{Machine: true, Terminal: true}, false),
		Entry("--interactive under a machine format on a terminal", clikit.FormGate{Interactive: true, Machine: true, Terminal: true}, true),
		Entry("a required field missing off a terminal", clikit.FormGate{}, false),
		Entry("every required field off a terminal", clikit.FormGate{Ready: true}, false),
	)

	DescribeTable("ask a form again when its submission is rejected as usage, not found or a taken name, but not for the timeline it writes to",
		func(err error, want bool) {
			Expect(clikit.Correctable(err)).To(Equal(want))
		},
		Entry("usage", &clikit.Error{Kind: clikit.KindUsage}, true),
		Entry("an invalid time", &clikit.Error{Kind: clikit.KindInvalidTime}, true),
		Entry("an event not found", &clikit.Error{Kind: clikit.KindEventNotFound}, true),
		Entry("no timeline selected", &clikit.Error{Kind: clikit.KindTimelineRequired}, false),
		Entry("a timeline not found", &clikit.Error{Kind: clikit.KindTimelineNotFound}, false),
		Entry("a timeline that exists", &clikit.Error{Kind: clikit.KindTimelineExists}, true),
		Entry("an error of no kind", errors.New("disk full"), false),
	)

	DescribeTable("read times in UTC, not the clock's zone, when the config file's default_timezone is UTC or unknown",
		func(defaultTimezone string) {
			now := func() time.Time { return clitest.TestNow().In(time.FixedZone("Test/Local", -3*3600)) }
			env := clikit.BaseCheckEnv(clikit.FormSession{App: clikit.App{Now: now}, Inv: clikit.Invocation{Cfg: config.Config{DefaultTimezone: defaultTimezone}}})

			Expect(env.Zone).To(Equal(clikit.Zone{Loc: time.UTC, Label: "UTC"}))
		},
		Entry("UTC", "UTC"),
		Entry("unknown", "Mars/Olympus_Mons"),
	)

	It("read times in --tz over the config file's default_timezone", func() {
		tz := "UTC"
		env := clikit.BaseCheckEnv(clikit.FormSession{App: clikit.App{Now: clitest.TestNow}, Inv: clikit.Invocation{TZ: &tz, Cfg: config.Config{DefaultTimezone: "Asia/Tokyo"}}})

		Expect(env.Zone.Loc).To(Equal(time.UTC))
	})

	It("abort the form on Esc and record it as cancelled, passing other keys through", func() {
		cancelled := false
		text := huh.NewInput().Key("text")
		filter := clikit.EscCancels(&cancelled, []huh.Field{text})
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

		Expect(clikit.EscCancels(&cancelled, []huh.Field{pick})(nil, esc)).To(Equal(esc))
		Expect(cancelled).To(BeFalse())
	})

	It("leave an entry on ctrl+t as the form's own abort, which its ask returns as a timeline switch", func() {
		var keys clikit.FormKeys
		text := huh.NewInput().Key("text")
		form := huh.NewForm(huh.NewGroup(text))
		ctrlT := tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}

		form.Update(keys.Filter(clikit.FormFrame{Entry: true}, []huh.Field{text})(nil, ctrlT))

		Expect(form.State).To(Equal(huh.StateAborted))
		Expect(keys.Ended(form.State, nil)).Error().To(MatchError(clikit.ErrSwitchTimeline))
	})

	It("pass ctrl+t through to a form other than an entry", func() {
		var keys clikit.FormKeys
		ctrlT := tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}

		Expect(keys.Filter(clikit.FormFrame{Timeline: "mongo-outage"}, nil)(nil, ctrlT)).To(Equal(ctrlT))
	})

	It("carry forward everything but the fields another entry clears", func() {
		values := clikit.FieldValues{
			"timeline": {"mongo-outage"}, "text": {"Primary stepped down"}, "at": {"03:14"}, "ended": {"03:20"},
			"approximate": {"true"}, "source": {"mongod"}, "tag": {"election"}, "url": {"https://example.com/1"},
			"author": {"sre"}, "tz": {"UTC"},
		}

		Expect(clikit.CarryForward(values, event.AddDefinition())).To(Equal(clikit.FieldValues{
			"timeline": {"mongo-outage"}, "source": {"mongod"}, "tag": {"election"}, "author": {"sre"}, "tz": {"UTC"},
		}))
		Expect(values).To(HaveKey("text"))
	})

	It("align a summary's columns to their widest cell", func() {
		rows := clikit.SummaryRows([]clikit.SummaryLine{
			{Cells: []string{"#9", "03:14:07Z", "Primary stepped down"}},
			{Cells: []string{"#10", "03:15:00Z", "Election timeout on db-3"}},
		}, lipgloss.NewStyle())

		Expect(rows).To(Equal("  #9   03:14:07Z  Primary stepped down\n  #10  03:15:00Z  Election timeout on db-3"))
	})

	Describe("session", func() {
		var (
			out, errOut bytes.Buffer
			app         clikit.App
			db          string
			ids         int
			asked       [][]huh.Field
			framed      []clikit.FormFrame
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
			var choices clikit.FormChoices
			Expect(clikit.WithStorePath(ctx, app, db, func(s *store.Store) error {
				t, err := s.TimelineByName(ctx, "mongo-outage")
				if err != nil {
					return err
				}
				choices, err = annotation.Choices(ctx, s, t, clikit.Zone{Loc: time.UTC, Label: "UTC"})
				return err
			})).To(Succeed())

			return choices.Options["event"]
		}

		answers := func(answers ...func([]huh.Field) (bool, error)) func(bool, graphics.Protocol) clikit.AskFunc {
			return clitest.ScriptedForms(&asked, &framed, answers...)
		}

		// runForm runs args against a root holding parent, whose add command runs its form as RunE does
		// on a terminal.
		runForm := func(ctx SpecContext, parent *cobra.Command, args ...string) error {
			GinkgoHelper()
			out.Reset()
			errOut.Reset()
			root := &cobra.Command{Use: "coroner", SilenceErrors: true, SilenceUsage: true}
			root.PersistentFlags().AddFlagSet(clikit.GlobalFlags())
			root.AddCommand(parent)
			root.SetArgs(append([]string{"--db", db}, args...))
			return root.ExecuteContext(ctx)
		}

		addCommand := func() *cobra.Command {
			return &cobra.Command{Use: "add", Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"}}
		}
		eventAdd := func(ctx SpecContext, forms func(bool, graphics.Protocol) clikit.AskFunc) *cobra.Command {
			events := &cobra.Command{Use: "event"}
			clikit.AddTimelineFlag(events.PersistentFlags())
			events.AddCommand(event.AddForm(timelinecmd.AskCreate).Attach(ctx, addCommand(), app, forms))
			return events
		}
		annotationAdd := func(ctx SpecContext, forms func(bool, graphics.Protocol) clikit.AskFunc) *cobra.Command {
			annotations := &cobra.Command{Use: "annotation"}
			annotations.AddCommand(annotation.AddForm(timelinecmd.AskCreate).Attach(ctx, addCommand(), app, forms))
			return annotations
		}

		BeforeEach(func(ctx SpecContext) {
			ids = 0
			asked, framed = nil, nil
			home := GinkgoT().TempDir()
			db = filepath.Join(home, "coroner.db")
			app = clitest.ScratchApp(home, &out, &errOut, clitest.SequentialIDs(&ids))
			app.InIsTTY, app.OutIsTTY = true, true
			clitest.WriteMigratedDatabase(db)

			Expect(run(ctx, "timeline", "create", "mongo-outage")).To(Equal(0), errOut.String())
		})

		When("a timeline is in use", func() {
			BeforeEach(func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())
			})

			It("open --interactive prefilled from the command line's arguments and flags", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add", "Primary stepped down", "--interactive", "--source", "mongod")).To(Succeed())

				Expect(clitest.FieldText(asked[0], "text")).To(Equal("Primary stepped down"))
				Expect(clitest.FieldText(asked[0], "source")).To(Equal("mongod"))
			})

			It("write what the form submits and echo a command that dry-runs to the same event", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(clitest.Submit(map[string]string{
					"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z", "source": "mongod", "tag": "election\nphase:detect",
				}), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add", "--interactive", "--json")).To(Succeed())
				written := clitest.WithoutRecordKeys(out.Bytes())

				Expect(runArgs(ctx, append(clitest.EchoedArgs(ctx, errOut.String())[1:], "--dry-run", "--json")...)).To(Equal(0), errOut.String())
				Expect(clitest.WithoutRecordKeys(out.Bytes())).To(Equal(written))
				Expect(written).To(HaveKeyWithValue("text", "Primary stepped down"))
			})

			It("log the same change lines as the command they echo", func(ctx SpecContext) {
				app.Log = clikit.NewDiagnosticLog(app)
				command := eventAdd(ctx, answers(clitest.Submit(map[string]string{
					"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z", "source": "mongod", "tag": "election\nphase:detect",
				}), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add", "--interactive")).To(Succeed())
				submitted := clitest.ChangeShapes(app.Log.Lines())
				echoed := clitest.EchoedArgs(ctx, errOut.String())[1:]
				Expect(echoed[:2]).To(Equal([]string{"--db", db}))

				app.Log = nil
				fresh := filepath.Join(GinkgoT().TempDir(), "coroner.db")
				clitest.WriteMigratedDatabase(fresh)
				Expect(runArgs(ctx, "--db", fresh, "timeline", "create", "mongo-outage")).To(Equal(0), errOut.String())
				Expect(runArgs(ctx, "--db", fresh, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())
				logPath := config.DiagnosticLogPath(fresh)
				before := clitest.FileSize(logPath)
				Expect(runArgs(ctx, append([]string{"--db", fresh}, echoed[2:]...)...)).To(Equal(0), errOut.String())

				Expect(submitted).To(Equal(clitest.ChangeShapes(clitest.AppendedLines(logPath, before))))
				Expect(submitted).To(ContainElement(HaveKeyWithValue("msg", "event added")))
			})

			It("store the time typed in the zone typed under More options", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(clitest.Submit(map[string]string{
					"text": "Pager fires", "at": "03:25", "tz": "Asia/Tokyo", "more-options": "true",
				}), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add", "--interactive", "--json")).To(Succeed())

				Expect(clitest.WithoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("occurred_at", "2026-09-12T18:25:00.000000000Z"))
			})

			It("apply the zone a later entry carries when More options is No", func(ctx SpecContext) {
				first := map[string]string{"text": "First", "at": "03:25", "tz": "Asia/Tokyo", "more-options": "true"}
				second := map[string]string{"text": "Second", "at": "03:25", "more-options": "false"}
				command := eventAdd(ctx, answers(clitest.Submit(first), clitest.Confirm(true), clitest.Submit(second), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(errOut.String()).To(ContainSubstring("add Second --tz Asia/Tokyo --at 03:25"))
				Expect(run(ctx, "event", "list", "--json")).To(Equal(0), errOut.String())
				Expect(strings.Count(out.String(), "2026-09-12T18:25:00.000000000Z")).To(Equal(2))
			})

			It("apply --tz when More options is No", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(clitest.Submit(map[string]string{
					"text": "Pager fires", "at": "03:25", "more-options": "false",
				}), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "--tz", "Asia/Tokyo", "event", "add", "--interactive", "--json")).To(Succeed())

				Expect(clitest.WithoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("occurred_at", "2026-09-12T18:25:00.000000000Z"))
			})

			It("show the resolved author as a placeholder", func(ctx SpecContext) {
				clitest.WithEnv(&app, map[string]string{"USER": "sre"})
				command := eventAdd(ctx, answers(clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add", "--interactive")).To(Succeed())

				Expect(clitest.FieldView(clitest.NamedField(asked[0], "author"))).To(ContainSubstring("sre"))
				Expect(clitest.FieldText(asked[0], "author")).To(BeEmpty())
			})

			It("show the config author as the placeholder when one is set", func(ctx SpecContext) {
				clitest.WithEnv(&app, map[string]string{"USER": "sre"})
				config := filepath.Join(filepath.Dir(db), ".config", "coroner", "config.toml")
				app.ReadFile = func(name string) ([]byte, error) {
					if name != config {
						return nil, fs.ErrNotExist
					}
					return []byte("author = \"Priya\"\n"), nil
				}
				command := eventAdd(ctx, answers(clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add", "--interactive")).To(Succeed())

				Expect(clitest.FieldView(clitest.NamedField(asked[0], "author"))).To(ContainSubstring("Priya"))
			})

			It("attach a Files path starting with ~/ from HOME, echoing its full path", func(ctx SpecContext) {
				home := GinkgoT().TempDir()
				clitest.WithEnv(&app, map[string]string{"HOME": home})
				Expect(os.WriteFile(filepath.Join(home, "log.txt"), []byte("stepdown\n"), 0o600)).To(Succeed())
				app.ReadFile = os.ReadFile
				command := eventAdd(ctx, answers(clitest.Submit(map[string]string{"text": "Primary stepped down", "attach": "~/log.txt"}), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(clitest.EchoedArgs(ctx, errOut.String())).To(ContainElements("--attach", filepath.Join(home, "log.txt")))
			})

			It("echo the flags-only command with every field in the flags' order", func(ctx SpecContext) {
				app.ReadFile = func(name string) ([]byte, error) {
					if name != "log.txt" {
						return nil, fs.ErrNotExist
					}
					return []byte("stepdown\n"), nil
				}
				command := eventAdd(ctx, answers(clitest.Submit(map[string]string{
					"text": "Primary stepped down", "tz": "UTC", "at": "2026-09-11T03:14:07Z", "ended": "2026-09-11T03:20:00Z",
					"approximate": "true", "source": "mongod", "tag": "election\nphase:detect", "url": "https://example.com/log",
					"author": "Priya", "dry-run": "true", "more-options": "true",
				}), clitest.Confirm(false)))

				// A list field takes its values by keys, and the form asks no id, so both are given by flag.
				Expect(runForm(ctx, command, "event", "add", "--interactive", "--attach", "log.txt",
					"--id", "01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2")).To(Succeed())

				Expect(clitest.EchoedArgs(ctx, errOut.String())).To(Equal([]string{
					"coroner", "--db", db, "event", "add", "Primary stepped down", "--tz", "UTC", "--at", "2026-09-11T03:14:07Z",
					"--ended", "2026-09-11T03:20:00Z", "--approximate", "--source", "mongod", "--tag", "election", "--tag", "phase:detect",
					"--url", "https://example.com/log", "--author", "Priya", "--attach", "log.txt",
					"--id", "01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2", "--dry-run",
				}))
			})

			It("open when a required flag is missing, prefilled from the argument", func(ctx SpecContext) {
				command := annotationAdd(ctx, answers(clitest.Cancel))

				Expect(runForm(ctx, command, "annotation", "add", "replSetStepDown")).To(Succeed())

				Expect(asked).NotTo(BeEmpty())
				Expect(clitest.FieldText(asked[0], "body")).To(Equal("replSetStepDown"))
			})

			It("open with no kind chosen", func(ctx SpecContext) {
				command := annotationAdd(ctx, answers(clitest.Cancel))

				Expect(runForm(ctx, command, "annotation", "add")).To(Succeed())

				Expect(clitest.FieldText(asked[0], "kind")).To(BeEmpty())
			})

			It("keep an event given by id prefix, and write the annotation on it", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Primary stepped down", "--json")).To(Equal(0), errOut.String())
				var event struct {
					ID string `json:"id"`
				}
				Expect(json.Unmarshal(out.Bytes(), &event)).To(Succeed())
				prefix := event.ID[:8]
				command := annotationAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					Expect(clitest.FieldText(fields, "event")).To(Equal(prefix))
					return true, nil
				}, clitest.Confirm(false)))

				Expect(runForm(ctx, command, "annotation", "add", "--interactive", "-e", prefix, "--kind", "evidence", "body", "--json")).
					To(Succeed())

				Expect(clitest.WithoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("event_id", event.ID))
			})

			It("write an annotation on the picked event and echo a command that dry-runs to the same one", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z")).To(Equal(0), errOut.String())
				var offered []string
				command := annotationAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					offered = clitest.OptionValues(clitest.NamedField(fields, "event"))
					return clitest.Submit(map[string]string{"body": "replSetStepDown", "event": "1", "kind": "evidence"})(fields)
				}, clitest.Confirm(false)))

				Expect(runForm(ctx, command, "annotation", "add", "--interactive", "--json")).To(Succeed())
				written := clitest.WithoutRecordKeys(out.Bytes())

				Expect(offered).To(Equal([]string{"", "1"}))
				Expect(runArgs(ctx, append(clitest.EchoedArgs(ctx, errOut.String())[1:], "--dry-run", "--json")...)).To(Equal(0), errOut.String())
				Expect(clitest.WithoutRecordKeys(out.Bytes())).To(Equal(written))
				Expect(written).To(HaveKey("event_id"))
			})

			It("open another entry with the timeline, source and tags kept, and the text, time and url cleared",
				func(ctx SpecContext) {
					entry := map[string]string{
						"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z", "source": "mongod", "tag": "election",
						"url": "https://example.com/1", "more-options": "true",
					}
					command := eventAdd(ctx, answers(clitest.Submit(entry), clitest.Confirm(true), clitest.Cancel))

					Expect(runForm(ctx, command, "event", "add", "-t", "mongo-outage")).To(Succeed())

					second := asked[2]
					Expect(clitest.FieldText(second, "source")).To(Equal("mongod"))
					Expect(clitest.NamedField(second, "tag").GetValue()).To(Equal([]string{"election"}))
					Expect(clitest.FieldText(second, "text")).To(BeEmpty())
					Expect(clitest.FieldText(second, "at")).To(BeEmpty())
					Expect(clitest.FieldText(second, "url")).To(BeEmpty())
					Expect(errOut.String()).To(ContainSubstring("-t mongo-outage"))
				})

			Describe("switching the timeline with ctrl+t", func() {
				BeforeEach(func(ctx SpecContext) {
					Expect(run(ctx, "timeline", "create", "api-latency")).To(Equal(0), errOut.String())
				})

				// listed returns the texts of the events event list prints for name.
				listed := func(ctx SpecContext, name string) []string {
					GinkgoHelper()
					Expect(run(ctx, "event", "list", "-t", name, "--json")).To(Equal(0), errOut.String())
					var list struct {
						Events []struct {
							Text string `json:"text"`
						} `json:"events"`
					}
					Expect(json.Unmarshal(out.Bytes(), &list)).To(Succeed())
					var texts []string
					for _, e := range list.Events {
						texts = append(texts, e.Text)
					}
					return texts
				}

				It("reopen the entry on the timeline picked, as typed, and echo the timeline", func(ctx SpecContext) {
					var opened string
					var offered []string
					command := eventAdd(ctx, answers(
						clitest.CtrlT(map[string]string{"text": "Edge slow", "source": "cloudflare"}),
						func(fields []huh.Field) (bool, error) {
							opened = clitest.FieldText(fields, "timeline")
							offered = clitest.OptionValues(clitest.NamedField(fields, "timeline"))
							return clitest.Submit(map[string]string{"timeline": "api-latency"})(fields)
						},
						clitest.Submit(nil), clitest.Confirm(false),
					))

					Expect(runForm(ctx, command, "event", "add")).To(Succeed())

					Expect(opened).To(Equal("mongo-outage"))
					Expect(offered).To(Equal([]string{"", "api-latency", "mongo-outage"}))
					Expect(framed[1]).To(Equal(clikit.FormFrame{Noun: "event"}))
					Expect(clitest.FieldText(asked[2], "text")).To(Equal("Edge slow"))
					Expect(clitest.FieldText(asked[2], "source")).To(Equal("cloudflare"))
					Expect(framed[2].Timeline).To(Equal("api-latency"))
					Expect(errOut.String()).To(ContainSubstring(" event add 'Edge slow' -t api-latency --source cloudflare\n"))
					Expect(listed(ctx, "api-latency")).To(Equal([]string{"Edge slow"}))
				})

				It("end with a block for each timeline added to, in the order each first got an event, and one command viewing them all",
					func(ctx SpecContext) {
						first := map[string]string{"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z"}
						second := map[string]string{"text": "Edge slow", "at": "2026-09-11T03:15:00Z"}
						third := map[string]string{"text": "Failover", "at": "2026-09-11T03:20:00Z"}
						command := eventAdd(ctx, answers(
							clitest.Submit(first), clitest.Confirm(true),
							clitest.CtrlT(nil), clitest.Submit(map[string]string{"timeline": "api-latency"}), clitest.Submit(second), clitest.Confirm(true),
							clitest.CtrlT(nil), clitest.Submit(map[string]string{"timeline": "mongo-outage"}), clitest.Submit(third), clitest.Confirm(false),
						))

						Expect(runForm(ctx, command, "event", "add")).To(Succeed())

						Expect(errOut.String()).To(HaveSuffix("\n" +
							"✓ Added 2 events to timeline `mongo-outage`\n" +
							"  #1  2026-09-11T03:14:07Z  Primary stepped down\n" +
							"  #2  2026-09-11T03:20:00Z  Failover\n" +
							"✓ Added 1 event to timeline `api-latency`\n" +
							"  #1  2026-09-11T03:15:00Z  Edge slow\n" +
							"\n" +
							"  View the timelines: coroner --db " + db + " timeline view -t mongo-outage -t api-latency\n"))
					})

				It("reopen the entry on its own timeline, as typed, when Esc leaves the picker", func(ctx SpecContext) {
					command := eventAdd(ctx, answers(clitest.CtrlT(map[string]string{"text": "Edge slow"}), clitest.Cancel, clitest.Submit(nil), clitest.Confirm(false)))

					Expect(runForm(ctx, command, "event", "add")).To(Succeed())

					Expect(clitest.FieldText(asked[2], "text")).To(Equal("Edge slow"))
					Expect(framed[2].Timeline).To(Equal("mongo-outage"))
					Expect(clitest.EchoedArgs(ctx, errOut.String())).NotTo(ContainElement("-t"))
					Expect(listed(ctx, "mongo-outage")).To(Equal([]string{"Edge slow"}))
				})

				It("create the timeline ✨ New timeline asks for, without making it current, and reopen the entry on it", func(ctx SpecContext) {
					var label string
					command := eventAdd(ctx, answers(
						clitest.CtrlT(map[string]string{"text": "Migration paused"}),
						func(fields []huh.Field) (bool, error) {
							label = clitest.FieldView(clitest.NamedField(fields, "timeline"))
							return clitest.Submit(map[string]string{"timeline": ""})(fields)
						},
						clitest.Submit(map[string]string{"name": "db-migration"}),
						clitest.Submit(nil), clitest.Confirm(false),
					))

					Expect(runForm(ctx, command, "event", "add")).To(Succeed())

					Expect(label).To(ContainSubstring("✨ New timeline"))
					Expect(framed[2]).To(Equal(clikit.FormFrame{Title: "📒 New timeline", Noun: "event"}))
					Expect(errOut.String()).To(ContainSubstring("Created timeline `db-migration`."))
					Expect(errOut.String()).To(ContainSubstring("Equivalent command: coroner --db " + db + " timeline create db-migration\n"))
					Expect(framed[3].Timeline).To(Equal("db-migration"))
					Expect(clitest.FieldText(asked[3], "text")).To(Equal("Migration paused"))
					Expect(listed(ctx, "db-migration")).To(Equal([]string{"Migration paused"}))
					Expect(run(ctx, "status", "--json")).To(Equal(0), errOut.String())
					Expect(clitest.WithoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("timeline", HaveKeyWithValue("name", "mongo-outage")))
				})

				It("reopen the entry on its own timeline when the new timeline form only dry-runs", func(ctx SpecContext) {
					command := eventAdd(ctx, answers(
						clitest.CtrlT(map[string]string{"text": "Migration paused"}), clitest.Submit(map[string]string{"timeline": ""}),
						clitest.Submit(map[string]string{"name": "db-migration", "dry-run": "true"}),
						clitest.Submit(nil), clitest.Confirm(false),
					))

					Expect(runForm(ctx, command, "event", "add")).To(Succeed())

					Expect(errOut.String()).To(ContainSubstring("Would create timeline `db-migration`."))
					Expect(framed[3].Timeline).To(Equal("mongo-outage"))
				})

				It("reopen the entry on its own timeline when Esc leaves the new timeline form", func(ctx SpecContext) {
					command := eventAdd(ctx, answers(
						clitest.CtrlT(map[string]string{"text": "Migration paused"}), clitest.Submit(map[string]string{"timeline": ""}), clitest.Cancel,
						clitest.Submit(nil), clitest.Confirm(false),
					))

					Expect(runForm(ctx, command, "event", "add")).To(Succeed())

					Expect(framed[3].Timeline).To(Equal("mongo-outage"))
					Expect(clitest.FieldText(asked[3], "text")).To(Equal("Migration paused"))
				})
			})

			It("open --interactive with More options shown and the url filled", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add", "-t", "mongo-outage", "--interactive", "--url", "https://e.example")).To(Succeed())

				Expect(clitest.FieldText(asked[0], "more-options")).To(Equal("true"))
				Expect(clitest.FieldText(asked[0], "url")).To(Equal("https://e.example"))
			})

			It("open the next entry with More options shown and dry run Yes after a dry run", func(ctx SpecContext) {
				entry := map[string]string{"text": "Pager fires", "more-options": "true", "dry-run": "true"}
				command := eventAdd(ctx, answers(clitest.Submit(entry), clitest.Confirm(true), clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(clitest.FieldText(asked[2], "more-options")).To(Equal("true"))
				Expect(clitest.FieldText(asked[2], "dry-run")).To(Equal("true"))
			})

			It("submit none of the fields More options hides when it is No, except the zone", func(ctx SpecContext) {
				hidden := map[string]string{"text": "Pager fires", "url": "https://e.example/1", "dry-run": "true"}
				shown := map[string]string{"text": "Failover", "url": "https://e.example/2", "dry-run": "true", "more-options": "true"}
				command := eventAdd(ctx, answers(clitest.Submit(hidden), clitest.Confirm(true), clitest.Submit(shown), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())
				echoed := errOut.String()

				Expect(echoed).NotTo(ContainSubstring("https://e.example/1"))
				Expect(echoed).To(ContainSubstring("add Failover --url https://e.example/2 --dry-run"))
				Expect(run(ctx, "event", "list", "--json")).To(Equal(0), errOut.String())
				Expect(out.String()).To(SatisfyAll(ContainSubstring("Pager fires"), Not(ContainSubstring("e.example")), Not(ContainSubstring("Failover"))))
			})

			It("write nothing and report interrupted when Ctrl-C ends the form", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					clitest.Fill(fields, map[string]string{"text": "Primary stepped down"})
					return false, huh.ErrUserAborted
				}))

				err := runForm(ctx, command, "event", "add")

				Expect(err).To(HaveField("Kind", clikit.KindInterrupted))
				Expect(run(ctx, "event", "list", "--json")).To(Equal(0), errOut.String())
				Expect(clitest.WithoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("total", 0.0))
			})

			It("end with a summary of the events added and the command that views their timeline", func(ctx SpecContext) {
				first := map[string]string{"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z"}
				second := map[string]string{"text": "Election timeout on db-3", "at": "2026-09-11T03:15:00Z"}
				command := eventAdd(ctx, answers(clitest.Submit(first), clitest.Confirm(true), clitest.Submit(second), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(errOut.String()).To(HaveSuffix("Equivalent command: coroner --db " + db + " event add 'Election timeout on db-3' --at 2026-09-11T03:15:00Z\n" +
					"\n" +
					"✓ Added 2 events to timeline `mongo-outage`\n" +
					"  #1  2026-09-11T03:14:07Z  Primary stepped down\n" +
					"  #2  2026-09-11T03:15:00Z  Election timeout on db-3\n" +
					"\n" +
					"  View the timeline: coroner --db " + db + " timeline view -t mongo-outage\n"))
			})

			It("count in the summary only the entries that added an event, not a dry run or an --id already stored", func(ctx SpecContext) {
				id := "01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2"
				Expect(run(ctx, "event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z", "--id", id)).To(Equal(0), errOut.String())
				stored := map[string]string{"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z"}
				dryRun := map[string]string{"text": "Pager fires", "more-options": "true", "dry-run": "true"}
				added := map[string]string{"text": "Failover", "at": "2026-09-11T03:20:00Z"}
				command := eventAdd(ctx, answers(clitest.Submit(stored), clitest.Confirm(true), clitest.Submit(added), clitest.Confirm(true), clitest.Submit(dryRun), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add", "--id", id)).To(Succeed())

				echoes := strings.Split(errOut.String(), "Equivalent command: ")
				Expect(echoes).To(HaveLen(4))
				Expect(echoes[2]).NotTo(ContainSubstring("--id"))

				Expect(errOut.String()).To(HaveSuffix("✓ Added 1 event to timeline `mongo-outage`\n" +
					"  #2  2026-09-11T03:20:00Z  Failover\n" +
					"\n" +
					"  View the timeline: coroner --db " + db + " timeline view -t mongo-outage\n"))
			})

			It("frame each entry and Add another with the timeline and the events added so far", func(ctx SpecContext) {
				first := map[string]string{"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z"}
				dryRun := map[string]string{"text": "Pager fires", "more-options": "true", "dry-run": "true"}
				command := eventAdd(ctx, answers(clitest.Submit(first), clitest.Confirm(true), clitest.Submit(dryRun), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(framed).To(Equal([]clikit.FormFrame{
					{Timeline: "mongo-outage", Entry: true, Noun: "event"},
					{Timeline: "mongo-outage", Added: 1, Noun: "event"},
					{Timeline: "mongo-outage", Entry: true, Added: 1, Noun: "event"},
					{Timeline: "mongo-outage", Added: 1, Noun: "event"},
				}))
			})

			It("print no summary when no entry added an event", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(clitest.Submit(map[string]string{"text": "Pager fires", "more-options": "true", "dry-run": "true"}), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(errOut.String()).To(ContainSubstring("Equivalent command"))
				Expect(errOut.String()).NotTo(ContainSubstring("View the timeline"))
			})

			It("end annotation add's session with a summary of the annotations added", func(ctx SpecContext) {
				command := annotationAdd(ctx, answers(clitest.Submit(map[string]string{"body": "replSetStepDown", "kind": "evidence"}), clitest.Confirm(false)))

				Expect(runForm(ctx, command, "annotation", "add")).To(Succeed())

				Expect(errOut.String()).To(HaveSuffix("✓ Added 1 annotation to timeline `mongo-outage`\n" +
					"  evidence  replSetStepDown\n" +
					"\n" +
					"  View the timeline: coroner --db " + db + " timeline view -t mongo-outage\n"))
			})

			It("end quietly, asking nothing more, when Esc leaves the first entry", func(ctx SpecContext) {
				command := eventAdd(ctx, answers(clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(asked).To(HaveLen(1))
				Expect(errOut.String()).To(BeEmpty())
			})

			It("end with the summary, asking nothing more, when Esc leaves an entry after one was added", func(ctx SpecContext) {
				first := map[string]string{"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z"}
				command := eventAdd(ctx, answers(clitest.Submit(first), clitest.Confirm(true), clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(asked).To(HaveLen(3))
				Expect(errOut.String()).To(HaveSuffix("✓ Added 1 event to timeline `mongo-outage`\n" +
					"  #1  2026-09-11T03:14:07Z  Primary stepped down\n" +
					"\n" +
					"  View the timeline: coroner --db " + db + " timeline view -t mongo-outage\n"))
			})

			It("ask the entry again, as typed, when the add rejects it", func(ctx SpecContext) {
				entry := map[string]string{"text": "Deploy", "at": "2026-09-11T04:00:00Z", "ended": "2026-09-11T03:00:00Z", "more-options": "true"}
				command := eventAdd(ctx, answers(clitest.Submit(entry), clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(errOut.String()).To(ContainSubstring("--ended"))
				Expect(clitest.FieldText(asked[1], "ended")).To(Equal("2026-09-11T03:00:00Z"))
			})

			It("print a rejected entry's error for a person, even under --json", func(ctx SpecContext) {
				entry := map[string]string{"text": "Deploy", "at": "2026-09-11T04:00:00Z", "ended": "2026-09-11T03:00:00Z", "more-options": "true"}
				command := eventAdd(ctx, answers(clitest.Submit(entry), clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add", "--interactive", "--json")).To(Succeed())

				Expect(errOut.String()).To(ContainSubstring("--ended"))
				Expect(errOut.String()).NotTo(ContainSubstring(`"kind"`))
			})

			It("complete annotation add's attach paths the same way", func(ctx SpecContext) {
				fsys := fstest.MapFS{"files/p99.png": {}}
				app.ReadDir = func(name string) ([]fs.DirEntry, error) { return fs.ReadDir(fsys, name) }
				Expect(run(ctx, "event", "add", "Primary stepped down")).To(Equal(0), errOut.String())
				var view string
				command := annotationAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					attach := clitest.NamedField(fields, "attach")
					attach.WithKeyMap(huh.NewDefaultKeyMap())
					attach.Focus()
					clitest.TypeText(attach, "files/p")
					view = clitest.FieldView(attach)
					return clitest.Cancel(fields)
				}))

				Expect(runForm(ctx, command, "annotation", "add")).To(Succeed())

				Expect(view).To(ContainSubstring(clikit.SuggestionsLabel + "files/p99.png"))
			})

			It("reject an attach path that does not exist as --attach does", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Deploy", "-t", "mongo-outage", "--attach", "missing.png")).To(Equal(2))
				Expect(errOut.String()).To(ContainSubstring("cannot read --attach missing.png"))
				command := eventAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					clitest.Fill(fields, map[string]string{"text": "Deploy", "at": "2026-09-11T04:00:00Z"})
					clitest.Enter(clitest.NamedField(fields, "attach"), "missing.png")
					return true, nil
				}, clitest.Cancel))

				Expect(runForm(ctx, command, "event", "add")).To(Succeed())

				Expect(errOut.String()).To(ContainSubstring("cannot read --attach missing.png"))
				Expect(errOut.String()).To(ContainSubstring("Pass a file that exists and you can read."))
				Expect(asked).To(HaveLen(2))
			})
		})

		Describe("source suggestions", func() {
			var s *store.Store
			sourcesFor := func(ctx SpecContext, choices func(context.Context, *store.Store, timeline.Timeline, clikit.Zone) (clikit.FormChoices, error), name string) []string {
				GinkgoHelper()
				t, err := s.TimelineByName(ctx, name)
				Expect(err).NotTo(HaveOccurred())
				got, err := choices(ctx, s, t, clikit.Zone{Loc: time.UTC, Label: "UTC"})
				Expect(err).NotTo(HaveOccurred())
				return got.Suggestions["source"]
			}

			BeforeEach(func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "create", "api-latency")).To(Equal(0), errOut.String())
				Expect(run(ctx, "event", "add", "Node down", "-t", "mongo-outage", "--source", "cassandra")).To(Equal(0), errOut.String())
				Expect(run(ctx, "event", "add", "Edge slow", "-t", "api-latency", "--source", "cloudflare")).To(Equal(0), errOut.String())
				var err error
				s, err = store.Open(ctx, db)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(s.Close)
			})

			It("suggest the timeline's own sources first, then every other", func(ctx SpecContext) {
				choices := event.AddForm(timelinecmd.AskCreate).Choices
				mongo := sourcesFor(ctx, choices, "mongo-outage")
				Expect(mongo).To(Equal([]string{"cassandra", "cloudflare"}))
				Expect(sourcesFor(ctx, choices, "api-latency")).To(Equal([]string{"cloudflare", "cassandra"}))

				source := clitest.NamedField(event.AddDefinition().FormFields(clitest.FormCheckEnv(), clikit.FieldValues{},
					clikit.FormChoices{Suggestions: map[string][]string{"source": mongo}}), "source")
				source.WithKeyMap(huh.NewDefaultKeyMap())
				source.Focus()
				clitest.TypeText(source, "c")
				Expect(clitest.FieldView(source)).To(ContainSubstring(clikit.SuggestionsLabel + "cassandra · cloudflare"))
			})

			It("mark the source typed as one already stored, or as a new one submitting creates", func(ctx SpecContext) {
				var source huh.Field
				command := eventAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
					source = clitest.NamedField(fields, "source")
					return false, nil
				}))
				Expect(runForm(ctx, command, "event", "add", "-t", "mongo-outage")).To(Succeed())
				source.WithKeyMap(huh.NewDefaultKeyMap())
				source.Focus()

				clitest.TypeText(source, "cassandra")
				Expect(clitest.FieldView(source)).To(ContainSubstring("Existing source"))

				clitest.TypeText(source, "-ring")
				Expect(clitest.FieldView(source)).To(ContainSubstring("New source"))

				clitest.TypeText(source, "!")
				Expect(clitest.FieldView(source)).To(SatisfyAll(ContainSubstring("Did you mean `cassandra-ring`?"), Not(ContainSubstring("New source"))))
			})

			It("scope annotation add's source suggestions to the timeline the same way", func(ctx SpecContext) {
				choices := annotation.AddForm(timelinecmd.AskCreate).Choices
				Expect(sourcesFor(ctx, choices, "mongo-outage")).To(Equal([]string{"cassandra", "cloudflare"}))
				Expect(sourcesFor(ctx, choices, "api-latency")).To(Equal([]string{"cloudflare", "cassandra"}))
			})
		})

		Describe("tag suggestions", func() {
			tagsFor := func(ctx SpecContext, name string) []string {
				GinkgoHelper()
				var got clikit.FormChoices
				Expect(clikit.WithStorePath(ctx, app, db, func(s *store.Store) error {
					t, err := s.TimelineByName(ctx, name)
					if err != nil {
						return err
					}
					got, err = event.AddForm(timelinecmd.AskCreate).Choices(ctx, s, t, clikit.Zone{Loc: time.UTC, Label: "UTC"})
					return err
				})).To(Succeed())
				return got.Suggestions["tag"]
			}

			BeforeEach(func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "create", "api-latency")).To(Equal(0), errOut.String())
				Expect(run(ctx, "event", "add", "Paged", "-t", "mongo-outage", "--tag", "escalation", "--at", "2026-09-11T03:00:00Z")).To(Equal(0), errOut.String())
				Expect(run(ctx, "event", "add", "Election", "-t", "mongo-outage", "--tag", "election", "--at", "2026-09-11T03:10:00Z")).To(Equal(0), errOut.String())
				Expect(run(ctx, "event", "add", "Edge slow", "-t", "api-latency", "--tag", "edge", "--at", "2026-09-11T03:20:00Z")).To(Equal(0), errOut.String())
				Expect(run(ctx, "timeline", "create", "empty")).To(Equal(0), errOut.String())
			})

			It("suggest every stored tag on a timeline with no tags", func(ctx SpecContext) {
				Expect(tagsFor(ctx, "empty")).To(Equal([]string{"edge", "election", "escalation"}))
			})

			It("suggest the timeline's tags, the most recently used first, then every other", func(ctx SpecContext) {
				Expect(tagsFor(ctx, "mongo-outage")).To(Equal([]string{"election", "escalation", "edge"}))
			})

			Context("in the form", func() {
				var fields []huh.Field

				BeforeEach(func(ctx SpecContext) {
					command := eventAdd(ctx, answers(func(asked []huh.Field) (bool, error) {
						fields = asked
						return false, nil
					}))
					Expect(runForm(ctx, command, "event", "add", "-t", "mongo-outage")).To(Succeed())
				})

				focused := func(name string) huh.Field {
					GinkgoHelper()
					formField := clitest.NamedField(fields, name)
					formField.WithKeyMap(huh.NewDefaultKeyMap())
					formField.Focus()
					return formField
				}

				It("mark the tag typed as one already stored, or as a new one", func() {
					tag := focused("tag")
					clitest.TypeText(tag, "elect")
					Expect(clitest.FieldView(tag)).To(ContainSubstring("✨ New tag"))

					clitest.TypeText(tag, "ion")
					Expect(clitest.FieldView(tag)).To(SatisfyAll(ContainSubstring("✅ Existing tag"), Not(ContainSubstring("New tag"))))
				})

				It("show the hint of a tag the check rejects in place of the mark", func() {
					tag := focused("tag")
					clitest.TypeText(tag, "Phase")
					Expect(clitest.FieldView(tag)).To(SatisfyAll(ContainSubstring("Did you mean `phase`?"), Not(ContainSubstring("New tag"))))
				})

				It("mark each held tag not stored as new", func() {
					tag := focused("tag")
					clitest.Enter(tag, "election")
					clitest.Enter(tag, "phase:detect")
					Expect(clitest.FieldView(tag)).To(SatisfyAll(
						ContainSubstring("✓ phase:detect  ✨ New tag"),
						MatchRegexp(`✓ election\s*\n`),
						Not(ContainSubstring("Existing tag")),
					))
				})

				It("mark no file attached", func() {
					attach := focused("attach")
					clitest.TypeText(attach, "a.txt")
					Expect(clitest.FieldView(attach)).NotTo(Or(ContainSubstring("New"), ContainSubstring("Existing")))
				})
			})
		})

		It("echo two tags given to the form as two --tag flags", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())
			command := eventAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
				clitest.Fill(fields, map[string]string{"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z"})
				tag := clitest.NamedField(fields, "tag")
				clitest.Enter(tag, "election")
				clitest.Enter(tag, "db")
				return true, nil
			}, clitest.Confirm(false)))

			Expect(runForm(ctx, command, "event", "add", "--interactive", "--json")).To(Succeed())

			Expect(clitest.WithoutRecordKeys(out.Bytes())).To(HaveKeyWithValue("tags", ConsistOf("election", "db")))
			Expect(strings.Join(clitest.EchoedArgs(ctx, errOut.String()), " ")).To(ContainSubstring("--tag election --tag db"))
		})

		It("echo three files given to the form as three --attach flags", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())
			app.ReadFile = func(name string) ([]byte, error) {
				if slices.Contains([]string{"a.txt", "b.txt", "c.txt"}, name) {
					return []byte(name), nil
				}
				return nil, fs.ErrNotExist
			}
			command := eventAdd(ctx, answers(func(fields []huh.Field) (bool, error) {
				clitest.Fill(fields, map[string]string{"text": "Primary stepped down", "at": "2026-09-11T03:14:07Z"})
				attach := clitest.NamedField(fields, "attach")
				clitest.Enter(attach, "a.txt")
				clitest.Enter(attach, "b.txt")
				clitest.Enter(attach, "c.txt")
				return true, nil
			}, clitest.Confirm(false)))

			Expect(runForm(ctx, command, "event", "add", "--interactive")).To(Succeed())

			Expect(strings.Join(clitest.EchoedArgs(ctx, errOut.String()), " ")).To(ContainSubstring("--attach a.txt --attach b.txt --attach c.txt"))
		})

		It("end without asking more when Esc cancels the timeline pick", func(ctx SpecContext) {
			command := eventAdd(ctx, answers(clitest.Cancel))

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
					Expect(clitest.OptionValues(clitest.NamedField(fields, "timeline"))).To(Equal([]string{"", "mongo-outage"}))
					return clitest.Submit(map[string]string{"timeline": "mongo-outage"})(fields)
				},
				clitest.Submit(map[string]string{"text": "Pager fires"}),
				clitest.Confirm(false),
			))

			Expect(runForm(ctx, command, "event", "add")).To(Succeed())

			Expect(errOut.String()).To(ContainSubstring(" event add 'Pager fires' -t mongo-outage"))
		})

		It("returns timeline_required naming coroner timeline create when there is no timeline", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "delete", "mongo-outage", "--yes")).To(Equal(0), errOut.String())
			command := eventAdd(ctx, answers())

			err := runForm(ctx, command, "event", "add")

			Expect(err).To(HaveField("Kind", clikit.KindTimelineRequired))
			Expect(err).To(HaveField("Hint", "Run 'coroner timeline create <name>' to create one."))
		})

		It("refuse --interactive off a terminal before opening anything", func(ctx SpecContext) {
			app.InIsTTY = false

			Expect(run(ctx, "event", "add", "Pager fires", "--interactive")).To(Equal(2))
			Expect(errOut.String()).To(ContainSubstring("--interactive needs a terminal"))
		})

		DescribeTable("run the command line as given on a terminal, asking nothing, when input is turned off or a machine format is asked for",
			func(ctx SpecContext, env map[string]string, args ...string) {
				clitest.WithEnv(&app, env)

				err := runForm(ctx, eventAdd(ctx, answers()), append([]string{"event", "add"}, args...)...)

				Expect(err).To(HaveField("Kind", clikit.KindUsage))
				Expect(err).To(HaveField("Message", "event add needs the event's text"))
			},
			Entry("--no-input", nil, "--no-input"),
			Entry("CORONER_NO_INPUT", map[string]string{"CORONER_NO_INPUT": "1"}),
			Entry("--json", nil, "--json"),
		)

		It("refuse an invalid --id given with --interactive as usage, before asking anything", func(ctx SpecContext) {
			command := eventAdd(ctx, answers())

			err := runForm(ctx, command, "event", "add", "--interactive", "-t", "mongo-outage", "--id", "not-a-uuid")

			Expect(err).To(HaveField("Kind", clikit.KindUsage))
			Expect(asked).To(BeEmpty())
		})

		DescribeTable("refuse --interactive when input is turned off, naming what turned it off, before opening anything",
			func(ctx SpecContext, env map[string]string, flags []string, message string) {
				clitest.WithEnv(&app, env)

				err := runForm(ctx, eventAdd(ctx, answers()), append([]string{"event", "add", "Pager fires", "--interactive"}, flags...)...)

				Expect(err).To(HaveField("Kind", clikit.KindUsage))
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
			app := clitest.ScratchApp(home, &out, &errOut, func() (uuid.UUID, error) { return uuid.New(), nil })
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
			def := event.AddDefinition()
			at := clitest.FieldNamed(def, "at")
			preview := at.Preview(input, clitest.FormCheckEnv())
			occurredAt, err := timeline.ParseInstant(resolved.OccurredAt)
			Expect(err).NotTo(HaveOccurred())
			shown, err := time.Parse(time.RFC3339Nano, strings.Trim(strings.Fields(preview)[0], "~,"))
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

var _ = Describe("inlineImages", func() {
	DescribeTable("decides how a form shows images, and logs it under --debug",
		func(outColor bool, env map[string]string, want graphics.Protocol, logged string) {
			log := &clikit.DiagnosticLog{Debug: true, Now: clitest.TestNow}
			app := clikit.App{Log: log, Err: &bytes.Buffer{}, LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			}}

			Expect(clikit.InlineImages(app, clikit.Invocation{OutColor: outColor})).To(Equal(want))
			Expect(log.Lines()).To(ContainElement(And(
				ContainSubstring(`"msg":"inline images decided"`),
				ContainSubstring(`"protocol":"`+logged+`"`),
			)))
		},
		Entry("color on a kitty terminal", true, map[string]string{"KITTY_WINDOW_ID": "1"}, graphics.Kitty, "kitty"),
		Entry("no color on a kitty terminal", false, map[string]string{"KITTY_WINDOW_ID": "1"}, graphics.None, "none"),
		Entry("color inside tmux", true, map[string]string{"TMUX": "/tmp/tmux-501/default,1,0", "KITTY_WINDOW_ID": "1"}, graphics.None, "none"),
	)
})
