package main

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	. "github.com/onsi/gomega"
)

// fakeSystem answers each command from a map keyed on its joined command line, and records
// every call and sleep. A command it has no answer for fails, unless respond handles it. run
// returns the error failing holds for its command line, or nil. It reports darwin/arm64 unless
// goos and goarch are set.
type fakeSystem struct {
	answers map[string]string
	failing map[string]error
	respond func(name string, args []string) (string, bool)
	calls   []string
	sleeps  []time.Duration
	goos    string
	goarch  string
}

var errFailed = errors.New("fake: failed")

// failingWriter fails every write, as a full disk or closed pipe would.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errFailed }

func newFake() *fakeSystem {
	return &fakeSystem{answers: map[string]string{}, failing: map[string]error{}, goos: "darwin", goarch: "arm64"}
}

func (f *fakeSystem) system() system {
	return system{
		output: func(_ context.Context, name string, args ...string) (string, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			f.calls = append(f.calls, line)
			if f.respond != nil {
				if out, ok := f.respond(name, args); ok {
					return out, nil
				}
			}
			out, ok := f.answers[line]
			if !ok {
				return "", fmt.Errorf("fake: no answer for %q", line)
			}

			return out, nil
		},
		run: func(_ context.Context, name string, args ...string) error {
			line := strings.Join(append([]string{name}, args...), " ")
			f.calls = append(f.calls, line)

			return f.failing[line]
		},
		sleep:  func(d time.Duration) { f.sleeps = append(f.sleeps, d) },
		goos:   f.goos,
		goarch: f.goarch,
	}
}

// runJSON runs the tool with args against f, and decodes exactly one JSON object from its
// output.
func runJSON(f *fakeSystem, args ...string) (map[string]any, int) {
	return runJSONContext(context.Background(), f, args...)
}

// runJSONContext is runJSON under ctx.
func runJSONContext(ctx context.Context, f *fakeSystem, args ...string) (map[string]any, int) {
	var out bytes.Buffer
	code := runCommand(ctx, f.system(), args, &out)

	dec := jsontext.NewDecoder(bytes.NewReader(out.Bytes()))
	var got map[string]any
	Expect(json.UnmarshalDecode(dec, &got)).To(Succeed(), out.String())
	_, err := dec.ReadToken()
	Expect(err).To(MatchError(io.EOF), "more than one JSON value: %s", out.String())

	return got, code
}
