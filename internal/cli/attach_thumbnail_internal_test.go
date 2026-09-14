package cli

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/colorprofile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/graphics"
)

var _ = Describe("the attach field's thumbnail", func() {
	attach := definition{fields: []field{attachField()}}
	var dir string

	writeFile := func(name string, content []byte) string {
		GinkgoHelper()
		path := filepath.Join(dir, name)
		Expect(os.WriteFile(path, content, 0o600)).To(Succeed())
		return path
	}
	pngFile := func(name string, width, height int) string {
		GinkgoHelper()
		var b bytes.Buffer
		Expect(png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, height)))).To(Succeed())
		return writeFile(name, b.Bytes())
	}
	field := func(images graphics.Protocol) huh.Field {
		GinkgoHelper()
		choices := formChoices{thumbnails: map[string]func(string) (string, tea.Cmd){"attach": attachThumbnail(os.ReadFile, images)}}
		return namedField(attach.formFields(formCheckEnv(), fieldValues{}, choices), "attach")
	}
	// add types path into f and enters it, returning every raw sequence the field asks to write.
	add := func(f huh.Field, path string) string {
		GinkgoHelper()
		typeInto(f, path)
		_, cmd := f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return rawWrites(cmd)
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	It("shows the placeholder of the file last added, with an image's size in pixels", func() {
		f := field(graphics.None)
		add(f, pngFile("p99.png", 40, 30))

		Expect(fieldView(f)).To(MatchRegexp(`\[image: p99\.png · 40×30 · \d+ B\]`))
	})

	It("shows a file that is not an image as its placeholder", func() {
		f := field(graphics.Kitty)
		written := add(f, writeFile("notes.txt", []byte("hello")))

		Expect(fieldView(f)).To(ContainSubstring("[file: notes.txt · 5 B]"))
		Expect(written).To(BeEmpty())
	})

	It("shows the picked value's thumbnail rather than the last one added", func() {
		f := field(graphics.None)
		add(f, pngFile("first.png", 16, 16))
		add(f, pngFile("second.png", 32, 8))

		press(f, tea.KeyUp, tea.KeyUp)

		Expect(fieldView(f)).To(SatisfyAll(ContainSubstring("[image: first.png · 16×16"), Not(ContainSubstring("[image: second.png"))))
	})

	It("shows no thumbnail once its last value is removed", func() {
		f := field(graphics.None)
		add(f, pngFile("p99.png", 40, 30))

		press(f, tea.KeyUp, tea.KeyBackspace)

		Expect(fieldView(f)).NotTo(ContainSubstring("[image:"))
	})

	Describe("on a terminal with Kitty graphics", func() {
		It("draws the image through placeholder cells over its caption, and sends it to the terminal", func() {
			f := field(graphics.Kitty)
			written := add(f, pngFile("p99.png", 40, 30))

			Expect(written).To(ContainSubstring("\x1b_Ga=t,f=100"))
			Expect(written).To(ContainSubstring("a=p,U=1"))
			Expect(fieldView(f)).To(SatisfyAll(ContainSubstring("\U0010EEEE"), MatchRegexp(`p99\.png · 40×30 · \d+ B`)))
		})

		It("sends an image once, however often its thumbnail shows again", func() {
			f := field(graphics.Kitty)
			add(f, pngFile("p99.png", 40, 30))

			_, cmd := f.Update(tea.KeyPressMsg{Code: tea.KeyUp})

			Expect(rawWrites(cmd)).To(BeEmpty())
			Expect(fieldView(f)).To(ContainSubstring("\U0010EEEE"))
		})
	})

	DescribeTable("the color profile a form pins",
		func(color bool, env map[string]string, want colorprofile.Profile) {
			lookup := func(key string) (string, bool) { value, ok := env[key]; return value, ok }
			Expect(formProfile(color, lookup)).To(Equal(want))
		},
		Entry("16 colors", true, map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, colorprofile.ANSI),
		Entry("24-bit color where images show, since an image's id travels as a color",
			true, map[string]string{"TERM_PROGRAM": "ghostty"}, colorprofile.TrueColor),
		Entry("no color when color is off, even where images show", false, map[string]string{"TERM_PROGRAM": "ghostty"}, colorprofile.ASCII),
	)
})

// rawWrites runs cmd and every command it batches, returning the raw sequences they write.
func rawWrites(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}

	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var b bytes.Buffer
		for _, each := range msg {
			b.WriteString(rawWrites(each))
		}
		return b.String()
	case tea.RawMsg:
		return fmt.Sprint(msg.Msg)
	}

	return ""
}
