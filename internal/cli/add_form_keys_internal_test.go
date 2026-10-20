package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
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
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+e", "ctrl+n", "ctrl+p":
		return tea.KeyPressMsg{Code: rune(name[len(name)-1]), Mod: tea.ModCtrl}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: name}
}

// keyedForms returns forms that press script's keys through each form terminalForms would run,
// at size, calling check, when non-nil, after each counted key and each typed step. It must run
// inside a synctest bubble.
func keyedForms(t *testing.T, size tea.WindowSizeMsg, script *keyScript, check func(*fittedForm)) func(bool, graphics.Protocol) askFunc {
	return func(bool, graphics.Protocol) askFunc {
		return func(_ context.Context, groups ...formGroup) (bool, error) {
			d := keyDriver{form: newForm("📝 New event", groups...), size: size, pending: make(chan tea.Msg), done: make(chan struct{})}
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
	form    *fittedForm
	size    tea.WindowSizeMsg
	pending chan tea.Msg
	// done ends every command still waiting to deliver its message.
	done chan struct{}
}

// press presses script's keys into the form, through filter, until the form ends, and returns the
// counted keys it took.
func (d keyDriver) press(t *testing.T, script *keyScript, filter func(tea.Model, tea.Msg) tea.Msg, check func(*fittedForm)) []string {
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
			if check != nil && d.form.State == huh.StateNormal {
				check(d.form)
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
	if view := d.form.View().Content; view != "" {
		t.Errorf("the form still draws once it ends, so the terminal keeps it:\n%s", view)
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

// formSize is the terminal the forms are pressed in.
func formSize() tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: 60, Height: 20} }

// keyedRun runs event add with no arguments against a scratch database, in a working directory
// holding files/p99.png, files/q01.png and docs/r1.txt, through forms that press script's keys at
// 60×20.
func keyedRun(t *testing.T, script *keyScript, check func(*fittedForm)) App {
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
	work := t.TempDir()
	err = os.CopyFS(work, fstest.MapFS{"files/p99.png": {Data: []byte("\x89PNG\r\n")}, "files/q01.png": {}, "docs/r1.txt": {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	app.ReadFile, app.ReadDir = os.ReadFile, os.ReadDir
	for _, args := range [][]string{
		{"timeline", "create", "api-latency"},
		{"timeline", "create", "mongo-outage"},
		{"event", "add", "Primary stepped down", "-t", "mongo-outage", "--source", "mongod"},
		{"event", "add", "Replica lag", "-t", "mongo-outage", "--source", "mysql"},
		{"event", "add", "On-call paged", "-t", "mongo-outage", "--tag", "escalation", "--at", "2026-09-11T03:00:00Z"},
		{"event", "add", "Election started", "-t", "mongo-outage", "--tag", "election", "--at", "2026-09-11T03:10:00Z"},
		{"event", "add", "Edge slow", "-t", "api-latency", "--tag", "edge"},
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
		event.AddCommand(eventAddForm().attach(t.Context(), add, app, keyedForms(t, formSize(), script, check)))
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
	return filesEntry().typed("files/p99.png").press("enter", "enter").press("enter")
}

func TestEventAddFormKeys(t *testing.T) {
	t.Run("baseline", func(t *testing.T) {
		script := baselineEntry().press("n")

		app := keyedRun(t, script, nil)

		if keys := len(slices.Concat(script.asks...)); keys > 12 {
			t.Errorf("the baseline took %d keys; want at most 12", keys)
		}
		added := addedEvent(t, app)
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
		titles := map[string]string{"more-options": "🔧 More options?"}
		for _, f := range eventAddDefinition().fields {
			titles[f.name] = fieldIcon(f.name) + " " + f.formTitle()
		}
		script := new(keyScript).
			press("down", "enter").
			typed("Failover finished").press("enter", "enter", "enter", "enter", "enter").
			press("y").
			typed("Asia/Tokyo").press("enter", "enter", "enter", "enter", "enter", "enter", "enter").
			press("n")
		// The terminal keeps only a frame's last rows, and a title is a whole line, since
		// descriptions start with their titles.
		check := func(form *fittedForm) {
			t.Helper()
			title, ok := titles[form.GetFocusedField().GetKey()]
			if !ok {
				return
			}
			frame := ansi.Strip(form.View().Content)
			lines := strings.Split(frame, "\n")
			height := formSize().Height
			if len(lines) > height {
				t.Errorf("the frame is %d lines at 60×20:\n%s", len(lines), frame)
			}
			if wide := slices.IndexFunc(lines, func(line string) bool { return ansi.StringWidth(line) > formSize().Width }); wide != -1 {
				t.Errorf("line %d of the frame is wider than 60 cells:\n%s", wide, frame)
			}
			shown := lines[max(0, len(lines)-height):]
			if !slices.ContainsFunc(shown, func(line string) bool {
				line = inFrame(line)
				return line == title || line == title+" (required)"
			}) {
				t.Errorf("%s is not shown at 60×20:\n%s", title, frame)
			}
		}

		keyedRun(t, script, check)
	})

	t.Run("tag suggestion", testTagSuggestion)
	t.Run("cycle and pick", testCycleAndPick)
	t.Run("remove carried tag", testRemoveCarriedTag)
	t.Run("esc in tags", testEscInTags)
	t.Run("attach completion", testAttachCompletion)
	t.Run("attach directories", testAttachDirectories)
	t.Run("attach cycle and pick", testAttachCycleAndPick)
}

func testAttachDirectories(t *testing.T) {
	script := filesEntry().
		typed("fi").press("ctrl+e").typed("p").press("ctrl+e", "enter").
		typed("do").press("ctrl+e", "ctrl+e", "enter").
		press("enter", "enter", "n")
	rows := map[string]string{"fi": "💡 Suggestions: files/", "files/": "💡 Suggestions: files/p99.png · files/q01.png", "docs/": "💡 Suggestions: docs/r1.txt"}
	seen := map[string]bool{}
	check := func(form *fittedForm) {
		t.Helper()
		typed := focusedTyped(form)
		row, ok := rows[typed]
		if !ok || seen[typed] {
			return
		}
		seen[typed] = true
		lines := strings.Split(ansi.Strip(form.View().Content), "\n")
		if !showsRow(lines, row) {
			t.Errorf("no row of matches leading with %q after %s:\n%s", row, typed, strings.Join(lines, "\n"))
		}
		if typed == "fi" && !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "> files/") }) {
			t.Errorf("the input does not ghost files/ after fi:\n%s", strings.Join(lines, "\n"))
		}
	}

	app := keyedRun(t, script, check)

	if len(seen) != len(rows) {
		t.Fatalf("the form held %v in Files; want each of fi, files/ and docs/", seen)
	}
	if names := addedAttachments(t, app); !slices.Equal(names, []string{"p99.png", "r1.txt"}) {
		t.Errorf("stored attachments %v; want p99.png and r1.txt", names)
	}
}

func testAttachCycleAndPick(t *testing.T) {
	script := filesEntry().
		typed("files/p99.png").press("enter").
		typed("files/").press("down", "ctrl+e", "enter").
		press("enter", "enter", "n")
	var typingHelp, pickHelp []string
	check := func(form *fittedForm) {
		l, ok := form.GetFocusedField().(*listField)
		switch {
		case !ok:
		case typingHelp == nil && l.typed() == "files/":
			typingHelp = helpTexts(l)
		case pickHelp == nil && l.typed() == "" && len(l.values) == 2:
			pickHelp = helpTexts(l)
		}
	}

	app := keyedRun(t, script, check)

	if !slices.Contains(typingHelp, "↑/↓ cycle matches") || !slices.Contains(typingHelp, "ctrl+n/ctrl+p cycle") {
		t.Errorf("help while typing files/ is %v; want the cycle keys", typingHelp)
	}
	if !slices.Contains(pickHelp, "↑/↓ pick") {
		t.Errorf("help after adding two files is %v; want ↑/↓ pick", pickHelp)
	}
	if names := addedAttachments(t, app); !slices.Equal(names, []string{"p99.png", "q01.png"}) {
		t.Errorf("stored attachments %v; want p99.png and q01.png", names)
	}
}

// addedAttachments returns the file names attached to the event the form added, Failover finished.
func addedAttachments(t *testing.T, app App) []string {
	t.Helper()
	data, err := json.Marshal(addedEvent(t, app)["attachments"])
	if err != nil {
		t.Fatal(err)
	}
	var attachments []struct {
		Filename string `json:"filename"`
	}
	err = json.Unmarshal(data, &attachments)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range attachments {
		names = append(names, a.Filename)
	}
	return names
}

func testAttachCompletion(t *testing.T) {
	script := filesEntry().typed("files/p").press("ctrl+e", "enter").press("enter", "enter", "n")
	checked := false
	check := func(form *fittedForm) {
		t.Helper()
		if focusedTyped(form) != "files/p" {
			return
		}
		checked = true
		lines := strings.Split(ansi.Strip(form.View().Content), "\n")
		if !showsRow(lines, "💡 Suggestions: files/p99.png") {
			t.Errorf("no row of matches leading with files/p99.png:\n%s", strings.Join(lines, "\n"))
		}
		if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "> files/p99.png") }) {
			t.Errorf("the input does not ghost files/p99.png after files/p:\n%s", strings.Join(lines, "\n"))
		}
	}

	app := keyedRun(t, script, check)

	if !checked {
		t.Fatal("the form never held files/p in Files")
	}
	if names := addedAttachments(t, app); !slices.Equal(names, []string{"p99.png"}) {
		t.Errorf("stored attachments %v; want p99.png", names)
	}
}

// filesEntry is the start of an entry, up to typing into Files.
func filesEntry() *keyScript {
	return new(keyScript).press("down", "enter").
		typed("Failover finished").press("enter").
		typed("03:25").press("enter").
		typed("m").press("ctrl+e", "enter").
		typed("election").press("enter").typed("db").press("tab")
}

func testTagSuggestion(t *testing.T) {
	script := tagsEntry().typed("e").press("ctrl+e", "enter").press("enter", "enter", "enter", "n")
	checked := false
	check := func(form *fittedForm) {
		t.Helper()
		if focusedTyped(form) != "e" {
			return
		}
		checked = true
		lines := strings.Split(ansi.Strip(form.View().Content), "\n")
		if !showsRow(lines, "💡 Suggestions: election") {
			t.Errorf("no row of matches leading with election:\n%s", strings.Join(lines, "\n"))
		}
		if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "> election") }) {
			t.Errorf("the input does not ghost election after e:\n%s", strings.Join(lines, "\n"))
		}
	}

	app := keyedRun(t, script, check)

	if !checked {
		t.Fatal("the form never held e in Tags")
	}
	if tags := addedTags(t, app); !slices.Equal(tags, []string{"election"}) {
		t.Errorf("stored tags %v; want election", tags)
	}
}

func testCycleAndPick(t *testing.T) {
	script := tagsEntry().
		typed("e").press("down", "ctrl+e", "enter").
		press("up", "backspace").
		typed("e").press("ctrl+n", "ctrl+n", "ctrl+e", "enter").
		press("enter", "enter", "enter", "n")
	var typingHelp, pickHelp []string
	check := func(form *fittedForm) {
		l, ok := form.GetFocusedField().(*listField)
		switch {
		case !ok:
		case typingHelp == nil && l.typed() == "e":
			typingHelp = helpTexts(l)
		case pickHelp == nil && l.typed() == "" && len(l.values) == 1:
			pickHelp = helpTexts(l)
		}
	}

	app := keyedRun(t, script, check)

	if !slices.Contains(typingHelp, "↑/↓ cycle matches") || !slices.Contains(typingHelp, "ctrl+n/ctrl+p cycle") {
		t.Errorf("help while typing e is %v; want the cycle keys", typingHelp)
	}
	if !slices.Contains(pickHelp, "↑/↓ pick") {
		t.Errorf("help after adding a tag is %v; want ↑/↓ pick", pickHelp)
	}
	if tags := addedTags(t, app); !slices.Equal(tags, []string{"edge"}) {
		t.Errorf("stored tags %v; want edge", tags)
	}
}

func testRemoveCarriedTag(t *testing.T) {
	script := baselineEntry().press("y").
		typed("Failover confirmed").press("enter", "enter", "enter", "up", "backspace", "enter", "enter", "enter").
		press("n")

	app := keyedRun(t, script, nil)

	events := storedEvents(t, app)
	i := slices.IndexFunc(events, func(e map[string]any) bool { return e["text"] == "Failover confirmed" })
	if i == -1 {
		t.Fatalf("stored %v; want Failover confirmed", events)
	}
	if tags, _ := events[i]["tags"].([]any); len(tags) != 1 {
		t.Errorf("stored tags %v; want one of the two carried", events[i]["tags"])
	}
}

func testEscInTags(t *testing.T) {
	script := tagsEntry().typed("db").press("esc", "n")

	app := keyedRun(t, script, nil)

	if slices.ContainsFunc(storedEvents(t, app), func(e map[string]any) bool { return e["text"] == "Failover finished" }) {
		t.Error("stored the entry Esc cancelled")
	}
}

// tagsEntry is the start of an entry, up to typing into Tags.
func tagsEntry() *keyScript {
	return new(keyScript).
		press("down", "enter").
		typed("Failover finished").press("enter", "enter", "enter")
}

// focusedTyped returns what is typed into the focused list field, or "" when no list field has focus.
func focusedTyped(form *fittedForm) string {
	l, ok := form.GetFocusedField().(*listField)
	if !ok {
		return ""
	}
	return l.typed()
}

// helpTexts returns the help the field shows for each of its keys, as "key description".
func helpTexts(f huh.Field) []string {
	var texts []string
	for _, b := range f.KeyBinds() {
		texts = append(texts, b.Help().Key+" "+b.Help().Desc)
	}
	return texts
}

// addedTags returns the tags of the event the form added, Failover finished.
func addedTags(t *testing.T, app App) []string {
	t.Helper()
	data, err := json.Marshal(addedEvent(t, app)["tags"])
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	err = json.Unmarshal(data, &tags)
	if err != nil {
		t.Fatal(err)
	}
	return tags
}

// addedEvent returns the event the form added, Failover finished.
func addedEvent(t *testing.T, app App) map[string]any {
	t.Helper()
	events := storedEvents(t, app)
	i := slices.IndexFunc(events, func(e map[string]any) bool { return e["text"] == "Failover finished" })
	if i == -1 {
		t.Fatalf("stored %v; want Failover finished", events)
	}
	return events[i]
}

func TestTerminalFormClearsWhenItEnds(t *testing.T) {
	for name, key := range map[string]string{"picked": "\r", "esc": "\x1b"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				keys, typing := io.Pipe()
				screen := new(lineTerminal)
				_, _ = screen.Write([]byte("Equivalent command: coroner event add\n"))
				go func() {
					// After bubbletea sends the model its unknown size, a terminal reports its own in band.
					time.Sleep(time.Millisecond)
					_, _ = typing.Write([]byte(ansi.WindowOp(8, 24, 100)))
					time.Sleep(time.Second)
					_, _ = typing.Write([]byte(key))
				}()
				pick := huh.NewSelect[string]().Key("timeline").Options(huh.NewOptions("api-latency", "mongo-outage")...)

				_, err := terminalForms(App{In: keys, Out: screen}, "📝 New event")(false, graphics.None)(t.Context(), formGroup{fields: []huh.Field{pick}})
				_ = typing.Close()
				if err != nil {
					t.Fatal(err)
				}
				_, _ = screen.Write([]byte("Added event #3\n"))

				want := []string{"Equivalent command: coroner event add", "Added event #3"}
				if got := screen.lines(); !slices.Equal(got, want) {
					t.Errorf("the screen holds %q once the form ends; want %q", got, want)
				}
			})
		})
	}
}

// lineTerminal is a terminal of unbounded height that keeps what each cell holds and where the
// cursor is, as an inline program moves it, and treats \n as \r\n as a tty's output does.
type lineTerminal struct {
	rows  [][]string
	x, y  int
	state byte
	p     *ansi.Parser
}

func (s *lineTerminal) Write(b []byte) (int, error) {
	if s.p == nil {
		s.p = ansi.NewParser()
	}
	for rest := b; len(rest) > 0; {
		seq, width, n, state := ansi.DecodeSequence(rest, s.state, s.p)
		s.state, rest = state, rest[n:]
		switch {
		case width > 0:
			s.put(string(seq), width)
		case len(seq) == 1:
			s.control(seq[0])
		case string(seq) == "\x1bM":
			s.y--
		case ansi.HasCsiPrefix(seq):
			s.csi(ansi.Cmd(s.p.Command()))
		}
	}
	return len(b), nil
}

func (s *lineTerminal) control(c byte) {
	switch c {
	case '\r':
		s.x = 0
	case '\n':
		s.x, s.y = 0, s.y+1
	case '\b':
		s.x = max(0, s.x-1)
	case '\t':
		s.x = s.x/8*8 + 8
	}
}

func (s *lineTerminal) csi(cmd ansi.Cmd) {
	if cmd.Prefix() != 0 || cmd.Intermediate() != 0 {
		return
	}
	n, _ := s.p.Param(0, 1)
	row := s.row()
	switch cmd.Final() {
	case 'A':
		s.y = max(0, s.y-n)
	case 'B':
		s.y += n
	case 'C':
		s.x += n
	case 'D':
		s.x = max(0, s.x-n)
	case 'J':
		s.rows = s.rows[:s.y+1]
		s.rows[s.y] = (*row)[:min(s.x, len(*row))]
	case 'K':
		*row = (*row)[:min(s.x, len(*row))]
	case 'X':
		for i := s.x; i < min(s.x+n, len(*row)); i++ {
			(*row)[i] = ""
		}
	case '@':
		*row = slices.Insert(*row, min(s.x, len(*row)), make([]string, n)...)
	case 'P':
		*row = slices.Delete(*row, min(s.x, len(*row)), min(s.x+n, len(*row)))
	}
}

// row returns the cursor's row, adding the rows down to it.
func (s *lineTerminal) row() *[]string {
	for len(s.rows) <= s.y {
		s.rows = append(s.rows, nil)
	}
	return &s.rows[s.y]
}

func (s *lineTerminal) put(grapheme string, width int) {
	row := s.row()
	for len(*row) < s.x+width {
		*row = append(*row, "")
	}
	(*row)[s.x] = grapheme
	s.x += width
}

// lines returns the screen's rows as text, without trailing blanks or blank rows at the bottom.
func (s *lineTerminal) lines() []string {
	var lines []string
	for _, row := range s.rows {
		var line strings.Builder
		for _, cell := range row {
			line.WriteString(cmp.Or(cell, " "))
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// showsRow reports whether a line of a form's view, inside its frame and huh's border, leads with row.
func showsRow(lines []string, row string) bool {
	return slices.ContainsFunc(lines, func(line string) bool { return strings.HasPrefix(inFrame(line), row) })
}

// inFrame returns what a line of a form's view holds inside its frame and huh's border.
func inFrame(line string) string {
	return strings.Trim(line, " │┃")
}
