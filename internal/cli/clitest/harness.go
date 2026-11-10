// Package clitest holds what the command specs share: the App a spec runs commands against,
// a database at the latest schema, and readers of what a command printed.
package clitest

import (
	"cmp"
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

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/skills"
)

// Harness builds the clikit.App an external spec runs: every seam set to the real file system and
// store, with a fixed clock, counted IDs and a config file held in memory. A spec sets the fields
// it varies, and may adjust the App that App returns.
type Harness struct {
	// Home is HOME, whose default config.toml reads as Config.
	Home string
	// DB, when set, is passed as --db ahead of the command line.
	DB string
	// Env is the whole environment; nil means HOME only.
	Env               map[string]string
	In                io.Reader
	InIsTTY, OutIsTTY bool
	Out, ErrOut       io.Writer
	// Config is the default config file's text; "" means there is none.
	Config  string
	Version string
	// Now defaults to 2026-09-13T01:30:00Z.
	Now func() time.Time
	// NewID defaults to a counter from 1 that starts again with each App, stamped with Now.
	NewID func() (uuid.UUID, error)
}

// App returns the App that runs args.
func (h Harness) App(args ...string) clikit.App {
	if h.DB != "" {
		args = append([]string{"--db", h.DB}, args...)
	}
	now := h.Now
	if now == nil {
		now = func() time.Time { return time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC) }
	}
	newID := h.NewID
	if newID == nil {
		var n uint64
		newID = func() (uuid.UUID, error) {
			n++
			return TestID(now().UnixMilli(), n), nil
		}
	}

	return clikit.App{
		Args:              args,
		In:                cmp.Or[io.Reader](h.In, strings.NewReader("")),
		Out:               cmp.Or[io.Writer](h.Out, io.Discard),
		Err:               cmp.Or[io.Writer](h.ErrOut, io.Discard),
		InIsTTY:           h.InIsTTY,
		OutIsTTY:          h.OutIsTTY,
		Version:           cmp.Or(h.Version, "(devel)"),
		Skill:             skills.Coroner,
		LookupEnv:         h.lookupEnv,
		ReadFile:          h.readFile,
		WriteFile:         os.WriteFile,
		OpenFile:          OpenFile,
		Open:              OpenRead,
		RemoveFile:        os.Remove,
		ReadDir:           os.ReadDir,
		Stat:              os.Stat,
		MkdirAll:          os.MkdirAll,
		Now:               now,
		NewID:             newID,
		OpenStore:         store.OpenWithoutBlobSync,
		OpenStoreReadOnly: store.OpenReadOnly,
		Rename:            os.Rename,
	}
}

func (h Harness) lookupEnv(key string) (string, bool) {
	if h.Env == nil {
		if key != "HOME" {
			return "", false
		}
		return h.Home, true
	}
	value, ok := h.Env[key]
	return value, ok
}

func (h Harness) readFile(name string) ([]byte, error) {
	if name != filepath.Join(h.Home, ".config", "coroner", "config.toml") {
		return os.ReadFile(filepath.Clean(name))
	}
	if h.Config == "" {
		return nil, fs.ErrNotExist
	}
	return []byte(h.Config), nil
}

// CountedIDs numbers IDs 0199a1b2-0000-7000-8000-000000000001 and up from *n + 1, so a spec
// restarts them by zeroing *n.
func CountedIDs(n *int) func() (uuid.UUID, error) {
	return func() (uuid.UUID, error) {
		*n++
		return uuid.Parse(CountedID(*n))
	}
}

// CountedID is the nth ID countedIDs returns.
func CountedID(n int) string {
	return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n)
}

// NilSeams returns the name of each func field of app that is nil, which a command would panic
// calling.
func NilSeams(app clikit.App) []string {
	var missing []string
	value := reflect.ValueOf(app)
	for field := range reflect.TypeFor[clikit.App]().Fields() {
		if field.Type.Kind() == reflect.Func && value.FieldByIndex(field.Index).IsNil() {
			missing = append(missing, field.Name)
		}
	}
	return missing
}

// TestID returns the UUIDv7 whose timestamp is millis and whose random bits hold n.
func TestID(millis int64, n uint64) uuid.UUID {
	var id uuid.UUID
	binary.BigEndian.PutUint64(id[:8], uint64(max(millis, 0))<<16)
	binary.BigEndian.PutUint64(id[8:], n)
	id[6] = 0x70
	id[8] = id[8]&0x3f | 0x80
	return id
}

// OpenFile is the App's OpenFile seam over the real file system.
func OpenFile(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(filepath.Clean(name), flag, perm)
}

// OpenRead is the App's Open seam over the real file system.
func OpenRead(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Clean(name))
}

// ReadFile returns the text of the file at path, failing the spec when it cannot be read.
func ReadFile(path string) string {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred())
	return string(data)
}

// ReadCounter counts the reads made of R.
type ReadCounter struct {
	R     io.Reader
	Reads int
}

func (c *ReadCounter) Read(p []byte) (int, error) {
	c.Reads++
	return c.R.Read(p)
}

// FailingWriter fails every write, as a closed pipe does.
type FailingWriter struct{}

func (FailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}

// LastEnvelope returns the "error" object of the JSON error envelope on stderr's last line.
func LastEnvelope(stderr string) (map[string]any, error) {
	line := LastLine(stderr)
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

// EnvelopeOf is lastEnvelope, failing the spec when stderr ends in no error envelope.
func EnvelopeOf(stderr string) map[string]any {
	GinkgoHelper()
	envelope, err := LastEnvelope(stderr)
	Expect(err).NotTo(HaveOccurred())
	return envelope
}

// LastLine returns the final line of output that ends in a newline.
func LastLine(output string) string {
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	return lines[len(lines)-1]
}
