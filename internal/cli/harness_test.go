package cli_test

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/skills"
)

// harness builds the cli.App an external spec runs: every seam set to the real file system and
// store, with a fixed clock, counted IDs and a config file held in memory. A spec sets the fields
// it varies, and may adjust the App app returns.
type harness struct {
	// home is HOME, whose default config.toml reads as config.
	home string
	// db, when set, is passed as --db ahead of the command line.
	db string
	// env is the whole environment; nil means HOME only.
	env               map[string]string
	in                io.Reader
	inIsTTY, outIsTTY bool
	out, errOut       io.Writer
	// config is the default config file's text; "" means there is none.
	config  string
	version string
	// now defaults to 2026-09-13T01:30:00Z.
	now func() time.Time
	// newID defaults to a counter from 1 that starts again with each App, stamped with now.
	newID func() (uuid.UUID, error)
}

func (h harness) app(args ...string) cli.App {
	if h.db != "" {
		args = append([]string{"--db", h.db}, args...)
	}
	now := h.now
	if now == nil {
		now = func() time.Time { return time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC) }
	}
	newID := h.newID
	if newID == nil {
		var n uint64
		newID = func() (uuid.UUID, error) {
			n++
			return testID(now().UnixMilli(), n), nil
		}
	}

	return cli.App{
		Args:              args,
		In:                cmp.Or[io.Reader](h.in, strings.NewReader("")),
		Out:               cmp.Or[io.Writer](h.out, io.Discard),
		Err:               cmp.Or[io.Writer](h.errOut, io.Discard),
		InIsTTY:           h.inIsTTY,
		OutIsTTY:          h.outIsTTY,
		Version:           cmp.Or(h.version, "(devel)"),
		Skill:             skills.Coroner,
		LookupEnv:         h.lookupEnv,
		ReadFile:          h.readFile,
		WriteFile:         os.WriteFile,
		OpenFile:          openFile,
		RemoveFile:        os.Remove,
		ReadDir:           os.ReadDir,
		Stat:              os.Stat,
		MkdirAll:          os.MkdirAll,
		Now:               now,
		NewID:             newID,
		OpenStore:         store.Open,
		OpenStoreReadOnly: store.OpenReadOnly,
	}
}

func (h harness) lookupEnv(key string) (string, bool) {
	if h.env == nil {
		if key != "HOME" {
			return "", false
		}
		return h.home, true
	}
	value, ok := h.env[key]
	return value, ok
}

func (h harness) readFile(name string) ([]byte, error) {
	if name != filepath.Join(h.home, ".config", "coroner", "config.toml") {
		return os.ReadFile(filepath.Clean(name))
	}
	if h.config == "" {
		return nil, fs.ErrNotExist
	}
	return []byte(h.config), nil
}

// countedIDs numbers IDs 0199a1b2-0000-7000-8000-000000000001 and up from *n + 1, so a spec
// restarts them by zeroing *n.
func countedIDs(n *int) func() (uuid.UUID, error) {
	return func() (uuid.UUID, error) {
		*n++
		return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", *n))
	}
}

// nilSeams returns the name of each func field of app that is nil, which a command would panic
// calling.
func nilSeams(app cli.App) []string {
	var missing []string
	value := reflect.ValueOf(app)
	for field := range reflect.TypeFor[cli.App]().Fields() {
		if field.Type.Kind() == reflect.Func && value.FieldByIndex(field.Index).IsNil() {
			missing = append(missing, field.Name)
		}
	}
	return missing
}

var _ = Describe("harness", func() {
	It("sets every seam of the App it builds", func() {
		Expect(nilSeams(harness{}.app())).To(BeEmpty())
	})
})

// expectNothingInvalidStored fails unless every timeline in the database at db exports, which
// export refuses when any stored string is not UTF-8, and returns that export as JSONL.
func expectNothingInvalidStored(ctx context.Context, db string) string {
	GinkgoHelper()
	s, err := store.Open(ctx, db)
	Expect(err).NotTo(HaveOccurred())
	timelines, err := s.Timelines(ctx)
	Expect(errors.Join(err, s.Close())).NotTo(HaveOccurred())
	args := []string{"timeline", "export", "--format", "jsonl"}
	for _, t := range timelines {
		args = append(args, "-t", t.Name)
	}
	var out, errOut strings.Builder
	Expect(cli.Run(ctx, harness{home: filepath.Dir(db), db: db, out: &out, errOut: &errOut}.app(args...))).To(Equal(0), errOut.String())
	return out.String()
}

// testID returns the UUIDv7 whose timestamp is millis and whose random bits hold n.
func testID(millis int64, n uint64) uuid.UUID {
	var id uuid.UUID
	binary.BigEndian.PutUint64(id[:8], uint64(max(millis, 0))<<16)
	binary.BigEndian.PutUint64(id[8:], n)
	id[6] = 0x70
	id[8] = id[8]&0x3f | 0x80
	return id
}

func openFile(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(filepath.Clean(name), flag, perm)
}

func readFile(path string) string {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred())
	return string(data)
}

// readCounter counts the reads made of r.
type readCounter struct {
	r     io.Reader
	reads int
}

func (c *readCounter) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

// lastEnvelope returns the "error" object of the JSON error envelope on stderr's last line.
func lastEnvelope(stderr string) (map[string]any, error) {
	line := lastLine(stderr)
	var envelope map[string]map[string]any
	err := json.Unmarshal([]byte(line), &envelope)
	if err != nil {
		return nil, fmt.Errorf("stderr's last line %q: %w", line, err)
	}
	if len(envelope) != 1 || envelope["error"] == nil {
		return nil, fmt.Errorf("stderr's last line %q is not an error envelope", line)
	}
	return envelope["error"], nil
}

// envelopeOf is lastEnvelope, failing the spec when stderr ends in no error envelope.
func envelopeOf(stderr string) map[string]any {
	GinkgoHelper()
	envelope, err := lastEnvelope(stderr)
	Expect(err).NotTo(HaveOccurred())
	return envelope
}

// lastLine returns the final line of output that ends in a newline.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	return lines[len(lines)-1]
}
