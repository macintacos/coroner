package main

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"strings"

	. "github.com/onsi/gomega"
)

// fakeSystem answers each command from a map keyed on its joined command line, and records
// every call. A command it has no answer for fails.
type fakeSystem struct {
	answers map[string]string
	calls   []string
}

func newFake() *fakeSystem {
	return &fakeSystem{answers: map[string]string{}}
}

func (f *fakeSystem) system() system {
	return system{
		output: func(_ context.Context, name string, args ...string) (string, error) {
			line := strings.Join(append([]string{name}, args...), " ")
			f.calls = append(f.calls, line)
			out, ok := f.answers[line]
			if !ok {
				return "", fmt.Errorf("fake: no answer for %q", line)
			}

			return out, nil
		},
	}
}

// runJSON runs the tool with args against f, and decodes exactly one JSON object from its
// output.
func runJSON(f *fakeSystem, args ...string) (map[string]any, int) {
	var out bytes.Buffer
	code := runCommand(context.Background(), f.system(), args, &out)

	dec := jsontext.NewDecoder(bytes.NewReader(out.Bytes()))
	var got map[string]any
	Expect(json.UnmarshalDecode(dec, &got)).To(Succeed(), out.String())
	_, err := dec.ReadToken()
	Expect(err).To(MatchError(io.EOF), "more than one JSON value: %s", out.String())

	return got, code
}
