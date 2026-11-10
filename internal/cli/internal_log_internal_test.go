package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli/clikit"
)

var _ = Describe("internal log", func() {
	write := func(keep logFilter, loc *time.Location, input string) string {
		var out bytes.Buffer
		p := logPrinter{
			open: func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(input)), nil },
			out:  &out, keep: keep, loc: loc,
		}
		printed, err := p.printFiles(context.Background(), "coroner.log")
		Expect(err).NotTo(HaveOccurred())
		Expect(printed).To(BeTrue())
		return out.String()
	}
	writeUTC := func(keep logFilter, input string) string { return write(keep, time.UTC, input) }

	It("reports a log it cannot open as a usage error naming the file once", func() {
		p := logPrinter{
			open: func(name string) (io.ReadCloser, error) {
				return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
			},
			out: io.Discard, loc: time.UTC,
		}
		_, err := p.printFiles(context.Background(), "/logs/coroner.log")
		var cliErr *clikit.Error
		Expect(errors.As(err, &cliErr)).To(BeTrue())
		Expect(cliErr.Kind).To(Equal(clikit.KindUsage))
		Expect(strings.Count(cliErr.Error(), "/logs/coroner.log")).To(Equal(1))
	})

	It("prints a line of two objects, or an object followed by more text, as it is", func() {
		for _, line := range []string{
			`{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"m"}{"a":1}`,
			`{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"m"} junk`,
		} {
			Expect(writeUTC(logFilter{}, line+"\n")).To(Equal(line + "\n"))
		}
	})

	It("renders a log line as key=value text in the line's key order", func() {
		Expect(writeUTC(logFilter{}, `{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"command finished","command":"coroner event add","kind":"ok","exit_code":0,"duration":1200000}`+"\n")).To(Equal(
			`time=2026-09-11T03:16:00.000Z level=INFO msg="command finished" command="coroner event add" kind=ok exit_code=0 duration=1.2ms` + "\n"))
		Expect(writeUTC(logFilter{}, `{"time":"2026-09-11T03:16:00Z","level":"WARN","msg":"m","flags":["at","db"],"error":"boom"}`+"\n")).To(Equal(
			`time=2026-09-11T03:16:00.000Z level=WARN msg=m flags="[at db]" error=boom` + "\n"))
	})

	It("prints the time in the zone it is given", func() {
		newYork, err := time.LoadLocation("America/New_York")
		Expect(err).NotTo(HaveOccurred())
		Expect(write(logFilter{}, newYork, `{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"m"}`)).To(HavePrefix("time=2026-09-10T23:16:00.000-04:00 "))
	})

	It("escapes control characters in keys and values", func() {
		out := writeUTC(logFilter{}, `{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"a\u009bb","k\u001bey":1,"name":"evil\u001b]0;title\u0007"}`+"\n")
		Expect(out).To(ContainSubstring(`name="evil\x1b]0;title\a"`))
		Expect(out).NotTo(ContainSubstring("\x1b"))
		Expect(out).NotTo(ContainSubstring("\x07"))
		Expect(out).NotTo(ContainSubstring("\x7f"))
		Expect(out).NotTo(ContainSubstring("\u009b"))
	})

	It("prints a line that is not a log line as it is, escaped, whatever the filter", func() {
		errorLevel := slog.LevelError
		keep := logFilter{level: &errorLevel}
		Expect(writeUTC(keep, "not json\n")).To(Equal("not json\n"))
		Expect(writeUTC(keep, "not json\x1b[2J\x7f\n")).To(Equal(`not json\x1b[2J\x7f` + "\n"))
		Expect(writeUTC(keep, "a\u009bb\n")).To(Equal(`a\u009bb` + "\n"))
		for _, line := range []string{
			`[1,2]`,
			`{"level":"INFO","msg":"m"}`,
			`{"time":"2026-09-11T03:16:00Z","level":"LOUD","msg":"m"}`,
		} {
			Expect(writeUTC(keep, line+"\n")).To(Equal(line + "\n"))
		}
	})

	It("keeps lines at the filter's level and above", func() {
		warn := slog.LevelWarn
		input := `{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"i"}` + "\n" +
			`{"time":"2026-09-11T03:16:00Z","level":"WARN","msg":"w"}` + "\n" +
			`{"time":"2026-09-11T03:16:00Z","level":"ERROR","msg":"e"}` + "\n"
		Expect(writeUTC(logFilter{level: &warn}, input)).To(Equal(
			"time=2026-09-11T03:16:00.000Z level=WARN msg=w\n" +
				"time=2026-09-11T03:16:00.000Z level=ERROR msg=e\n"))
	})

	It("keeps lines from the filter's command and the commands under it", func() {
		line := func(command string) string {
			return `{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"m","command":"` + command + `"}` + "\n"
		}
		out := writeUTC(logFilter{command: "coroner event"},
			line("coroner event add")+line("coroner event")+line("coroner events")+line("coroner timeline list"))
		Expect(out).To(Equal(
			`time=2026-09-11T03:16:00.000Z level=INFO msg=m command="coroner event add"` + "\n" +
				`time=2026-09-11T03:16:00.000Z level=INFO msg=m command="coroner event"` + "\n"))
	})

	It("keeps lines at or after since and before until", func() {
		since := time.Date(2026, 9, 11, 3, 15, 0, 0, time.UTC)
		until := time.Date(2026, 9, 11, 3, 20, 0, 0, time.UTC)
		line := func(at string) string {
			return `{"time":"2026-09-11T03:` + at + `:00Z","level":"INFO","msg":"m"}` + "\n"
		}
		Expect(writeUTC(logFilter{since: &since, until: &until}, line("14")+line("15")+line("19")+line("20"))).To(Equal(
			"time=2026-09-11T03:15:00.000Z level=INFO msg=m\n" +
				"time=2026-09-11T03:19:00.000Z level=INFO msg=m\n"))
	})

	It("prints a line longer than 64 KiB in full, and a last line with no newline", func() {
		long := strings.Repeat("a", 1<<20)
		out := writeUTC(logFilter{}, `{"time":"2026-09-11T03:16:00Z","level":"INFO","msg":"`+long+`"}`+"\nlast")
		Expect(out).To(Equal("time=2026-09-11T03:16:00.000Z level=INFO msg=" + long + "\nlast\n"))
	})

	Describe("readLogFilter", func() {
		read := func(args ...string) error {
			cmd := newInternalLogCommand(context.Background(), clikit.App{})
			NewRoot(context.Background(), clikit.App{}).AddCommand(cmd)
			Expect(cmd.ParseFlags(args)).To(Succeed())
			_, err := readLogFilter(slog.New(slog.DiscardHandler), cmd, time.Now(), clikit.Zone{Loc: time.UTC})
			return err
		}

		It("rejects a level that is not debug, info, warn or error", func() {
			Expect(read("--level", "loud")).To(MatchError(ContainSubstring(`--level "loud" is not debug, info, warn or error`)))
		})

		It("rejects a command path that names no command", func() {
			for _, value := range []string{"evnet", "event bogus", " "} {
				Expect(read("--command", value)).To(MatchError(ContainSubstring("names no coroner command")))
			}
		})
	})
})
