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
	fieldReading := func(readFile func(string) ([]byte, error), images graphics.Protocol) huh.Field {
		GinkgoHelper()
		choices := formChoices{thumbnails: map[string]func(string) (string, tea.Cmd){"attach": attachThumbnail(readFile, images)}}
		return namedField(attach.formFields(formCheckEnv(), fieldValues{}, choices), "attach")
	}
	field := func(images graphics.Protocol) huh.Field {
		GinkgoHelper()
		return fieldReading(os.ReadFile, images)
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

	It("captions an image it cannot decode as an image, without its size in pixels", func() {
		f := field(graphics.Kitty)
		written := add(f, writeFile("x.webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")))

		Expect(fieldView(f)).To(ContainSubstring("[image: x.webp · 16 B]"))
		Expect(written).To(BeEmpty())
	})

	It("shows no thumbnail for a path it cannot read, until the file is there", func() {
		f := field(graphics.Kitty)
		path := filepath.Join(dir, "later.txt")
		written := add(f, path)

		Expect(fieldView(f)).NotTo(ContainSubstring("later.txt ·"))
		Expect(written).To(BeEmpty())

		writeFile("later.txt", []byte("hello"))
		press(f, tea.KeyUp)

		Expect(fieldView(f)).To(ContainSubstring("[file: later.txt · 5 B]"))
	})

	It("reads a file once, however often it is picked", func() {
		reads := 0
		f := fieldReading(func(name string) ([]byte, error) {
			reads++
			return os.ReadFile(name) //nolint:gosec // the spec reads only the file it wrote
		}, graphics.Kitty)
		add(f, pngFile("p99.png", 40, 30))

		press(f, tea.KeyUp, tea.KeyDown, tea.KeyUp)

		Expect(reads).To(Equal(1))
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
		func(color bool, images graphics.Protocol, attach bool, want colorprofile.Profile) {
			Expect(formProfile(color, images, attach)).To(Equal(want))
		},
		Entry("16 colors", true, graphics.None, true, colorprofile.ANSI),
		Entry("24-bit color for an attach field where images show, since an image's id travels as a color",
			true, graphics.Kitty, true, colorprofile.TrueColor),
		Entry("16 colors where images show but no field attaches", true, graphics.Kitty, false, colorprofile.ANSI),
		Entry("no color when color is off", false, graphics.Kitty, true, colorprofile.ASCII),
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
