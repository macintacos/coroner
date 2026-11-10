package clitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
)

// ScratchApp returns an App on home, with no config file, the fixed clock and a real store.
func ScratchApp(home string, out, errOut io.Writer, newID func() (uuid.UUID, error)) clikit.App {
	return clikit.App{
		Out: out, Err: errOut,
		LookupEnv:         func(key string) (string, bool) { return map[string]string{"HOME": home}[key], key == "HOME" },
		ReadFile:          func(string) ([]byte, error) { return nil, fs.ErrNotExist },
		ReadDir:           func(string) ([]fs.DirEntry, error) { return nil, fs.ErrNotExist },
		OpenFile:          OpenFile,
		Stat:              os.Stat,
		Rename:            os.Rename,
		Now:               TestNow,
		NewID:             newID,
		OpenStore:         store.Open,
		OpenStoreReadOnly: store.OpenReadOnly,
	}
}

// ChangeShape is a change line without what differs between two databases: its level, message,
// keys and fields.
func ChangeShape(line map[string]any) map[string]any {
	return map[string]any{
		"level": line["level"], "msg": line["msg"], "keys": slices.Sorted(maps.Keys(line)), "fields": line["fields"],
	}
}

// ChangeShapes returns the shape of each JSON line in lines but command finished.
func ChangeShapes(lines [][]byte) []map[string]any {
	GinkgoHelper()
	var shapes []map[string]any
	for _, raw := range lines {
		if len(raw) == 0 {
			continue
		}
		var line map[string]any
		Expect(json.Unmarshal(raw, &line)).To(Succeed())
		if line["msg"] != "command finished" {
			shapes = append(shapes, ChangeShape(line))
		}
	}

	return shapes
}

// AppendedLines returns the lines of the file at path past its first skip bytes.
func AppendedLines(path string, skip int) [][]byte {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred())

	return bytes.Split(bytes.TrimSpace(data[skip:]), []byte("\n"))
}

// FileSize returns the size of the file at path, or 0 when there is none.
func FileSize(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return int(info.Size())
}

// StorelessApp returns an App that runs args, with no config file, and fails the spec when it
// opens the store.
func StorelessApp(args []string) clikit.App {
	return clikit.App{
		Args: args, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard,
		LookupEnv: EnvOf(map[string]string{"HOME": "/nonexistent", "CORONER_DB": "/nonexistent/coroner.db"}),
		ReadFile:  func(string) ([]byte, error) { return nil, fs.ErrNotExist },
		Now:       TestNow,
		NewID:     func() (uuid.UUID, error) { return uuid.UUID{}, nil },
		OpenStore: func(context.Context, string, ...store.Option) (*store.Store, error) {
			Fail(strings.TrimSpace("coroner "+strings.Join(args, " ")) + " opened the store before rejecting its input")
			return nil, errors.New("unreachable")
		},
	}
}

// TestNow is the suite's fixed clock.
func TestNow() time.Time {
	return time.Date(2026, 9, 12, 21, 30, 0, 0, time.UTC)
}

// SequentialIDs returns ids numbered from *n+1, counting in *n.
func SequentialIDs(n *int) func() (uuid.UUID, error) {
	return func() (uuid.UUID, error) {
		*n++
		return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", *n))
	}
}

// EnvOf returns a LookupEnv that reads env.
func EnvOf(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

// WithEnv sets env over app's environment.
func WithEnv(app *clikit.App, env map[string]string) {
	lookupEnv := app.LookupEnv
	app.LookupEnv = func(key string) (string, bool) {
		if value, ok := env[key]; ok {
			return value, true
		}
		return lookupEnv(key)
	}
}

// FieldNamed returns def's field called name.
func FieldNamed(def clikit.Definition, name string) clikit.Field {
	GinkgoHelper()
	i := def.FieldIndex(name)
	if i == -1 {
		Fail("no field " + name)
	}

	return def.Fields[i]
}

// ShellSplit returns line's words, split as a shell splits them.
func ShellSplit(ctx context.Context, line string) []string {
	GinkgoHelper()
	split, err := exec.CommandContext(ctx, "sh", "-c", "printf '%s\\0' "+line).Output() //nolint:gosec // the spec hands the line to a shell on purpose
	Expect(err).NotTo(HaveOccurred())

	return strings.Split(strings.TrimSuffix(string(split), "\x00"), "\x00")
}

// ScriptedForms returns forms that answer each ask in turn with the next of answers, appending
// what each ask showed to asked, and the frame it was asked in to framed.
func ScriptedForms(asked *[][]huh.Field, framed *[]clikit.FormFrame, answers ...func([]huh.Field) (bool, error)) func(bool, graphics.Protocol) clikit.AskFunc {
	DeferCleanup(func() {
		Expect(answers).To(BeEmpty(), "%d scripted answers were never asked for", len(answers))
	})
	return func(bool, graphics.Protocol) clikit.AskFunc {
		return func(_ context.Context, f clikit.FormFrame, groups ...clikit.FormGroup) (bool, error) {
			GinkgoHelper()
			fields := clikit.FieldsOf(groups)
			*asked = append(*asked, fields)
			*framed = append(*framed, f)
			Expect(answers).NotTo(BeEmpty(), "asked a form no answer was scripted for")
			answer := answers[0]
			answers = answers[1:]
			return answer(fields)
		}
	}
}

// Submit returns an answer that fills a form's fields with values and submits it.
func Submit(values map[string]string) func([]huh.Field) (bool, error) {
	return func(fields []huh.Field) (bool, error) {
		Fill(fields, values)
		return true, nil
	}
}

// CtrlT returns an answer that fills a form's fields with values and leaves it as ctrl+t
// does.
func CtrlT(values map[string]string) func([]huh.Field) (bool, error) {
	return func(fields []huh.Field) (bool, error) {
		Fill(fields, values)
		return false, clikit.ErrSwitchTimeline
	}
}

// Cancel answers a form as Esc does.
func Cancel([]huh.Field) (bool, error) { return false, nil }

// Confirm returns an answer to "add another": yes or no.
func Confirm(yes bool) func([]huh.Field) (bool, error) {
	return func(fields []huh.Field) (bool, error) {
		GinkgoHelper()
		another, ok := fields[0].(*huh.Confirm)
		Expect(ok).To(BeTrue(), "expected the add-another confirm, got %T", fields[0])
		another.Value(&yes)
		return true, nil
	}
}

// Fill sets each field named in values to that value, as typing and picking would.
func Fill(fields []huh.Field, values map[string]string) {
	GinkgoHelper()
	for name, value := range values {
		switch f := NamedField(fields, name).(type) {
		case *huh.Select[string]:
			f.Value(&value)
		case *huh.Confirm:
			b := value == "true"
			f.Value(&b)
		case *clikit.ListField:
			for line := range strings.Lines(value) {
				Enter(f, strings.TrimSuffix(line, "\n"))
			}
		default:
			TypeInto(f, value)
		}
	}
}

// FieldText returns the value the field called name opened with or holds, as text.
func FieldText(fields []huh.Field, name string) string {
	GinkgoHelper()
	return fmt.Sprint(NamedField(fields, name).GetValue())
}

// OptionValues returns the values a select picks from, read by moving through its options from the
// first.
func OptionValues(formField huh.Field) []string {
	GinkgoHelper()
	selectField, ok := formField.(*huh.Select[string])
	Expect(ok).To(BeTrue(), "form field %s is not a select", formField.GetKey())
	var values []string
	selectField.WithKeyMap(huh.NewDefaultKeyMap())
	selectField.Focus()
	selectField.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	for {
		value, _ := selectField.Hovered()
		if slices.Contains(values, value) {
			return values
		}
		values = append(values, value)
		selectField.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
}

// EchoedArgs returns the args of the last equivalent command in stderr, split as a shell splits them.
func EchoedArgs(ctx context.Context, stderr string) []string {
	GinkgoHelper()
	const prefix = "Equivalent command: "
	i := strings.LastIndex(stderr, prefix)
	Expect(i).NotTo(Equal(-1), "no equivalent command in %q", stderr)
	line, _, _ := strings.Cut(stderr[i+len(prefix):], "\n")

	return ShellSplit(ctx, line)
}

// WithoutRecordKeys returns the JSON object in data without the keys a new record gets.
func WithoutRecordKeys(data []byte) map[string]any {
	GinkgoHelper()
	var object map[string]any
	Expect(json.Unmarshal(data, &object)).To(Succeed(), string(data))
	for _, key := range []string{"id", "seq", "created_at", "updated_at"} {
		delete(object, key)
	}

	return object
}

// ErrDiskFull is the error DiskFullWriter fails with.
var ErrDiskFull = errors.New("disk full")

// DiskFullWriter is a file whose every write fails with errDiskFull.
type DiskFullWriter struct{ io.WriteCloser }

func (DiskFullWriter) Write([]byte) (int, error) { return 0, ErrDiskFull }

// FormCheckEnv is what form fields check time input against, as the flag side reads it.
func FormCheckEnv() clikit.CheckEnv {
	GinkgoHelper()
	z, err := clikit.TimeZone(nil, "local", TestNow().Location())
	Expect(err).NotTo(HaveOccurred())

	return clikit.CheckEnv{Now: TestNow(), Zone: z}
}

// CtrlE presses ctrl+e in formField.
func CtrlE(formField huh.Field) { formField.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}) }

// Press sends formField each key code.
func Press(formField huh.Field, codes ...rune) {
	for _, code := range codes {
		formField.Update(tea.KeyPressMsg{Code: code})
	}
}

// TypeText presses a key in formField for each character of text.
func TypeText(formField huh.Field, text string) {
	for _, r := range text {
		formField.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TypeKeys presses a key in form for each character of text.
func TypeKeys(form *huh.Form, text string) {
	for _, r := range text {
		form.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// PressResizing presses code in form and, when the key asks for the window size, gives form size
// again, as the terminal would.
func PressResizing(form *huh.Form, code rune, size tea.WindowSizeMsg) {
	GinkgoHelper()
	_, cmd := form.Update(tea.KeyPressMsg{Code: code})
	if cmd != nil && reflect.DeepEqual(cmd(), tea.RequestWindowSize()) {
		form.Update(size)
	}
}

// HelpKeys returns the enabled key bindings formField shows in its help, as "key description".
func HelpKeys(formField huh.Field) []string {
	var keys []string
	for _, binding := range formField.KeyBinds() {
		if binding.Enabled() {
			keys = append(keys, binding.Help().Key+" "+binding.Help().Desc)
		}
	}

	return keys
}

// Enter types value into formField and presses Enter.
func Enter(formField huh.Field, value string) {
	GinkgoHelper()
	TypeInto(formField, value)
	Press(formField, tea.KeyEnter)
}

// NamedField returns the form field keyed name.
func NamedField(fields []huh.Field, name string) huh.Field {
	GinkgoHelper()
	for _, formField := range fields {
		if formField.GetKey() == name {
			return formField
		}
	}

	Fail("no form field " + name)
	return nil
}

// FieldKeys returns each form field's key, in order.
func FieldKeys(fields []huh.Field) []string {
	keys := make([]string, len(fields))
	for i, formField := range fields {
		keys[i] = formField.GetKey()
	}

	return keys
}

// TypeInto sets the text formField holds, as a fake form fills it.
func TypeInto(formField huh.Field, value string) {
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

// RenderedDescription returns formField's view once a form has run its description function.
func RenderedDescription(formField huh.Field) string {
	GinkgoHelper()
	group := huh.NewGroup(formField)
	_, update := group.Update(nil)
	Expect(update).NotTo(BeNil(), "form field %s has no description function", formField.GetKey())
	group.Update(update())

	return FieldView(formField)
}

// FieldView returns formField as it renders, without its styling.
func FieldView(formField huh.Field) string {
	GinkgoHelper()
	view, ok := formField.WithWidth(400).(interface{ View() string })
	if !ok {
		Fail(fmt.Sprintf("form field %s has no view", formField.GetKey()))
	}

	return ansi.Strip(view.View())
}

// Stripped returns pieces without their styles, or nil for none.
func Stripped(pieces []string) []string {
	var plain []string
	for _, piece := range pieces {
		plain = append(plain, ansi.Strip(piece))
	}

	return plain
}

// WithTTY returns app with stdin, stdout and stderr on a terminal as given.
func WithTTY(app clikit.App, in, out, errOut bool) clikit.App {
	app.InIsTTY, app.OutIsTTY, app.ErrIsTTY = in, out, errOut
	return app
}
