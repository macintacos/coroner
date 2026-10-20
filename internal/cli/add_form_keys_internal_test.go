package cli

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/graphics"
)

// scriptKey is one step of a keyScript: text typed, which is not counted, or a named key pressed,
// which is.
type scriptKey struct{ text, name string }

// keyScript is what a person types into the forms, in order, across every ask. asks records the
// counted keys each ask took.
type keyScript struct {
	keys []scriptKey
	asks [][]string
}

func (s *keyScript) typed(text string) *keyScript {
	s.keys = append(s.keys, scriptKey{text: text})
	return s
}

func (s *keyScript) press(names ...string) *keyScript {
	for _, name := range names {
		s.keys = append(s.keys, scriptKey{name: name})
	}
	return s
}

// namedKey returns the key press a keyScript names.
func namedKey(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: name}
}

// keyedForms returns forms that press script's keys through each form terminalForms would run,
// at size, calling check, when non-nil, after each counted key. It must run inside a synctest
// bubble.
func keyedForms(t *testing.T, size tea.WindowSizeMsg, script *keyScript, check func(*huh.Form)) func(bool, graphics.Protocol) askFunc {
	return func(bool, graphics.Protocol) askFunc {
		return func(_ context.Context, groups ...formGroup) (bool, error) {
			d := keyDriver{form: newForm(groups...), size: size, pending: make(chan tea.Msg), done: make(chan struct{})}
			cancelled := false
			keys := d.press(t, script, escCancels(&cancelled, fieldsOf(groups)), check)
			script.asks = append(script.asks, keys)
			switch {
			case cancelled:
				return false, nil
			case d.form.State == huh.StateAborted:
				return false, huh.ErrUserAborted
			}
			return true, nil
		}
	}
}

// keyDriver runs a form as bubbletea would: each command in a goroutine, its message fed back.
type keyDriver struct {
	form    *huh.Form
	size    tea.WindowSizeMsg
	pending chan tea.Msg
	// done ends every command still waiting to deliver its message.
	done chan struct{}
}

// press presses script's keys into the form, through filter, until the form ends, and returns the
// counted keys it took.
func (d keyDriver) press(t *testing.T, script *keyScript, filter func(tea.Model, tea.Msg) tea.Msg, check func(*huh.Form)) []string {
	t.Helper()
	var counted []string
	d.run(d.form.Init())
	d.update(d.size)
	d.settle()
	if check != nil && d.form.State == huh.StateNormal {
		check(d.form)
	}
	for d.form.State == huh.StateNormal {
		if len(script.keys) == 0 {
			t.Fatalf("the form is still asking after %v when the script ends", counted)
		}
		key := script.keys[0]
		script.keys = script.keys[1:]
		if key.name == "" {
			for _, r := range key.text {
				d.update(filter(nil, tea.KeyPressMsg{Code: r, Text: string(r)}))
				d.settle()
			}
			continue
		}
		counted = append(counted, key.name)
		d.update(filter(nil, namedKey(key.name)))
		d.settle()
		if check != nil && d.form.State == huh.StateNormal {
			check(d.form)
		}
	}
	close(d.done)
	// Moves fake time past every pending blink, so the bubble ends with no goroutine blocked.
	time.Sleep(time.Second)

	return counted
}

func (d keyDriver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		select {
		case d.pending <- cmd():
		case <-d.done:
		}
	}()
}

func (d keyDriver) update(msg tea.Msg) {
	_, cmd := d.form.Update(msg)
	d.run(cmd)
}

// settle feeds the form the messages its commands produce, until only commands waiting on time
// (cursor blinks) remain.
func (d keyDriver) settle() {
	for {
		synctest.Wait()
		select {
		case msg := <-d.pending:
			d.handle(msg)
		default:
			return
		}
	}
}

func (d keyDriver) handle(msg tea.Msg) {
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, cmd := range batch {
			d.run(cmd)
		}
		return
	}
	if cmds, ok := sequenced(msg); ok {
		go d.runInOrder(cmds)
		return
	}
	if reflect.DeepEqual(msg, tea.RequestWindowSize()) {
		msg = d.size
	}
	d.update(msg)
}

// runInOrder runs cmds one at a time, delivering each message before running the next.
func (d keyDriver) runInOrder(cmds []tea.Cmd) {
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		select {
		case d.pending <- cmd():
		case <-d.done:
			return
		}
	}
}

// sequenced returns the commands of bubbletea's unexported sequence message.
func sequenced(msg tea.Msg) ([]tea.Cmd, bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		return nil, false
	}
	cmds := make([]tea.Cmd, v.Len())
	for i := range cmds {
		cmds[i], _ = reflect.TypeAssert[tea.Cmd](v.Index(i))
	}
	return cmds, true
}

// keyedRun runs event add with no arguments against a scratch database through forms that press
// script's keys at 60×20.
func keyedRun(t *testing.T, script *keyScript, check func(*huh.Form)) App {
	t.Helper()
	home := t.TempDir()
	db := filepath.Join(home, "coroner.db")
	data, err := migrated()
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(db, data, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	ids := 0
	app := scratchApp(home, io.Discard, io.Discard, sequentialIDs(&ids))
	app.ReadFile = func(name string) ([]byte, error) {
		if !strings.HasPrefix(name, "files/") {
			return nil, fs.ErrNotExist
		}
		return []byte("\x89PNG\r\n"), nil
	}
	for _, args := range [][]string{
		{"timeline", "create", "api-latency"},
		{"timeline", "create", "mongo-outage"},
		{"event", "add", "Primary stepped down", "-t", "mongo-outage", "--source", "mongod"},
		{"event", "add", "Replica lag", "-t", "mongo-outage", "--source", "mysql"},
	} {
		seed := app
		seed.Args = append([]string{"--db", db}, args...)
		if code := Run(t.Context(), seed); code != 0 {
			t.Fatalf("coroner %v exited %d", args, code)
		}
	}
	app.InIsTTY, app.OutIsTTY = true, true

	synctest.Test(t, func(t *testing.T) {
		event := &cobra.Command{Use: "event"}
		addTimelineFlag(event.PersistentFlags())
		add := &cobra.Command{Use: "add", Annotations: map[string]string{formatsAnnotation: "text,json"}}
		event.AddCommand(eventAddForm().attach(t.Context(), add, app, keyedForms(t, tea.WindowSizeMsg{Width: 60, Height: 20}, script, check)))
		root := &cobra.Command{Use: "coroner", SilenceErrors: true, SilenceUsage: true}
		root.PersistentFlags().AddFlagSet(globalFlags())
		root.AddCommand(event)
		root.SetOut(io.Discard)
		root.SetArgs([]string{"--db", db, "event", "add"})
		err := root.ExecuteContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	})
	for i, keys := range script.asks {
		t.Logf("ask %d: %d keys %v", i, len(keys), keys)
	}

	app.Args = []string{"--db", db}
	return app
}

// storedEvents returns the events event list prints for mongo-outage.
func storedEvents(t *testing.T, app App) []map[string]any {
	t.Helper()
	var out strings.Builder
	app.Out = &out
	app.Args = append(app.Args, "event", "list", "-t", "mongo-outage", "--json")
	if code := Run(t.Context(), app); code != 0 {
		t.Fatalf("event list exited %d", code)
	}
	var list struct {
		Events []map[string]any `json:"events"`
	}
	err := json.Unmarshal([]byte(out.String()), &list)
	if err != nil {
		t.Fatal(err)
	}
	return list.Events
}

// baselineEntry is the issue's typical event, up to the More options answer.
func baselineEntry() *keyScript {
	return new(keyScript).
		press("down", "enter").
		typed("Failover finished").press("enter").
		typed("03:25").press("enter").
		typed("m").press("ctrl+e", "enter").
		typed("election").press("ctrl+j").typed("db").press("enter").
		typed("files/p99.png").press("enter", "enter").
		press("enter")
}

func TestEventAddFormKeys(t *testing.T) {
	t.Run("baseline", func(t *testing.T) {
		script := baselineEntry().press("n")

		app := keyedRun(t, script, nil)

		if keys := len(slices.Concat(script.asks...)); keys > 12 {
			t.Errorf("the baseline took %d keys; want at most 12", keys)
		}
		events := storedEvents(t, app)
		i := slices.IndexFunc(events, func(e map[string]any) bool { return e["text"] == "Failover finished" })
		if i == -1 {
			t.Fatalf("stored %v; want Failover finished", events)
		}
		added := events[i]
		if added["source"] != "mongod" {
			t.Errorf("stored %v; want Failover finished from mongod", added)
		}
		if tags, _ := json.Marshal(added["tags"]); string(tags) != `["db","election"]` {
			t.Errorf("stored tags %s; want election and db", tags)
		}
		if attachments, _ := added["attachments"].([]any); len(attachments) != 1 {
			t.Errorf("stored attachments %v; want one", added["attachments"])
		}
	})

	t.Run("another", func(t *testing.T) {
		script := baselineEntry().press("y").
			typed("Failover confirmed").press("enter", "enter", "enter", "enter", "enter", "enter").
			press("n")

		keyedRun(t, script, nil)

		if another := len(script.asks[2]); another != 1 {
			t.Errorf("Add another took %d keys; want 1", another)
		}
		if second := len(script.asks[3]); second > 6 {
			t.Errorf("the second entry took %d keys; want at most 6", second)
		}
	})

	t.Run("more options", func(t *testing.T) {
		titles := map[string]string{"more-options": "More options?"}
		for _, f := range eventAddDefinition().fields {
			titles[f.name] = f.formTitle()
		}
		script := new(keyScript).
			press("down", "enter").
			typed("Failover finished").press("enter", "enter", "enter", "enter", "enter").
			press("y").
			typed("Asia/Tokyo").press("enter", "enter", "enter", "enter", "enter", "enter", "enter").
			press("n")
		// A title must lead a line, since names such as at and url also occur inside descriptions.
		check := func(form *huh.Form) {
			t.Helper()
			title, ok := titles[form.GetFocusedField().GetKey()]
			if !ok {
				return
			}
			lines := strings.Split(ansi.Strip(form.View()), "\n")
			if !slices.ContainsFunc(lines, func(line string) bool {
				return strings.HasPrefix(strings.TrimLeft(line, "┃│ "), title)
			}) {
				t.Errorf("%s is not shown at 60×20:\n%s", title, ansi.Strip(form.View()))
			}
		}

		keyedRun(t, script, check)
	})
}
