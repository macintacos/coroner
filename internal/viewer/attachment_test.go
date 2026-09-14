package viewer_test

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/viewer"
)

const (
	pngSHA     = "1111111111111111111111111111111111111111111111111111111111111111"
	logSHA     = "2222222222222222222222222222222222222222222222222222222222222222"
	grafanaSHA = "3333333333333333333333333333333333333333333333333333333333333333"
)

// withAttachments is fixture() with a PNG and a log file on the first event, and an image on
// that event's evidence.
func withAttachments() viewer.Snapshot {
	s := fixture()
	stepdown := s.Events[0].ID
	evidence := "0199a1b2-0000-7000-8000-000000000021"
	s.Annotations[0].ID = evidence
	s.Attachments = []timeline.Attachment{
		{
			ID: "0199a1b2-0000-7000-8000-000000000031", Owner: timeline.AttachmentOwner{EventID: &stepdown},
			Filename: "p99.png", MIME: "image/png", SHA256: pngSHA, Size: 184_000,
		},
		{
			ID: "0199a1b2-0000-7000-8000-000000000032", Owner: timeline.AttachmentOwner{EventID: &stepdown},
			Filename: "mongod.log", MIME: "text/plain; charset=utf-8", SHA256: logSHA, Size: 2_140_000,
		},
		{
			ID: "0199a1b2-0000-7000-8000-000000000033", Owner: timeline.AttachmentOwner{AnnotationID: &evidence},
			Filename: "grafana.png", MIME: "image/png", SHA256: grafanaSHA, Size: 9_000,
		},
	}

	return s
}

func pngOf(width, height int) []byte {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, height)))).To(Succeed())
	return b.Bytes()
}

// blobs is an Image that records the hash of every attachment it reads and decodes content.
type blobs struct {
	mu      sync.Mutex
	read    []string
	content map[string][]byte
}

func (b *blobs) image(a timeline.Attachment) (graphics.Image, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.read = append(b.read, a.SHA256)
	content, ok := b.content[a.SHA256]
	if !ok {
		return graphics.Image{}, errors.New("blob " + a.SHA256 + ": no such file")
	}

	return graphics.Decode(content)
}

func (b *blobs) reads() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string{}, b.read...)
}

// optionsFor shows snapshot, reads it again unchanged and reads attachments from b.
func optionsFor(snapshot viewer.Snapshot, b *blobs) viewer.Options {
	return viewer.Options{
		Snapshot:  snapshot,
		Load:      (&loader{next: snapshot}).load,
		Image:     b.image,
		Location:  newYork(),
		ZoneLabel: "America/New_York",
		Now:       func() time.Time { return utc(12, 21, 30, 0) },
	}
}

func newOn(term viewer.Terminal, snapshot viewer.Snapshot, b *blobs) viewer.Model {
	return viewer.OnTerminal(term, viewer.New(optionsFor(snapshot, b)))
}

// answered is m sized 80 by 20, once the terminal has answered the queries Init sends.
func answered(m tea.Model) tea.Model {
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = m.Update(uv.PrimaryDeviceAttributesEvent{62, 22})
	return m
}

// openWith is the viewer on a terminal showing images through protocol, once answered.
func openWith(snapshot viewer.Snapshot, b *blobs, protocol graphics.Protocol) tea.Model {
	return answered(newOn(viewer.Terminal{Graphics: protocol}, snapshot, b))
}

// drainMsgs is drain, also returning every message m was handed, in order.
func drainMsgs(m tea.Model, cmd tea.Cmd) (tea.Model, []tea.Msg) {
	var all []tea.Msg
	for _, msg := range messages(cmd) {
		var next tea.Cmd
		m, next = m.Update(msg)
		var more []tea.Msg
		m, more = drainMsgs(m, next)
		all = append(append(all, msg), more...)
	}

	return m, all
}

func raws(msgs []tea.Msg) string {
	var b strings.Builder
	for _, msg := range msgs {
		if raw, ok := msg.(tea.RawMsg); ok {
			fmt.Fprint(&b, raw.Msg)
		}
	}

	return b.String()
}

var _ = Describe("the viewer's attachments", func() {
	var b *blobs

	BeforeEach(func() {
		b = &blobs{content: map[string][]byte{pngSHA: pngOf(40, 30), grafanaSHA: pngOf(16, 16), logSHA: []byte("log")}}
	})

	It("counts an event's attachments beside its annotations", func() {
		m, _ := openWith(withAttachments(), b, graphics.None).Update(tea.WindowSizeMsg{Width: 120, Height: 20})
		Expect(cursorLine(m)).To(ContainSubstring("(2 annotations, 2 attachments)"))
	})

	It("shows a placeholder for each attachment, with an image's size in pixels once read", func() {
		m, cmd := press(openWith(withAttachments(), b, graphics.None), "enter")
		m, _ = drainMsgs(m, cmd)

		matchGolden("attachments", screen(m))
	})

	It("names an image's size in bytes alone while it is being read", func() {
		m, _ := press(openWith(withAttachments(), b, graphics.None), "enter")

		Expect(screen(m)).To(ContainSubstring("[image: p99.png · 184 KB]"))
	})

	It("reads each image once, and never a file that is not an image", func() {
		m, cmd := press(openWith(withAttachments(), b, graphics.None), "enter")
		m, _ = drainMsgs(m, cmd)
		m, cmd = press(m, "enter", "enter")
		drainMsgs(m, cmd)

		Expect(b.reads()).To(ConsistOf(pngSHA, grafanaSHA))
	})

	It("never reads an image in a format it cannot decode", func() {
		webp := withAttachments()
		webp.Attachments[0].MIME = "image/webp"
		m, cmd := press(openWith(webp, b, graphics.None), "enter")
		drainMsgs(m, cmd)

		Expect(b.reads()).To(ConsistOf(grafanaSHA))
	})

	It("reads an expanded event's images one at a time", func() {
		_, cmd := press(openWith(withAttachments(), b, graphics.None), "enter")
		Expect(cmd()).NotTo(BeAssignableToTypeOf(tea.BatchMsg{}))
	})

	It("reads the images an expanded event gains when the timeline is read again", func() {
		logOnly := withAttachments()
		logOnly.Attachments = logOnly.Attachments[1:2]
		opts := optionsFor(logOnly, b)
		opts.Load = (&loader{next: withAttachments()}).load
		m, cmd := press(answered(viewer.New(opts)), "enter")
		m, _ = drainMsgs(m, cmd)
		m, cmd = press(m, "r")
		drainMsgs(m, cmd)

		Expect(b.reads()).To(ConsistOf(pngSHA, grafanaSHA))
	})

	It("shows the placeholder for an image it cannot read, and keeps running", func() {
		delete(b.content, pngSHA)
		m, cmd := press(openWith(withAttachments(), b, graphics.None), "enter")
		m, msgs := drainMsgs(m, cmd)

		Expect(screen(m)).To(ContainSubstring("[image: p99.png · 184 KB]"))
		Expect(msgs).NotTo(ContainElement(BeAssignableToTypeOf(tea.QuitMsg{})))
	})

	It("sends nothing to the terminal but its screen when it cannot show images", func() {
		m, cmd := press(openWith(withAttachments(), b, graphics.None), "enter")
		_, msgs := drainMsgs(m, cmd)

		Expect(raws(msgs)).To(BeEmpty())
	})

	Describe("on a terminal with Kitty graphics", func() {
		It("asks the terminal for its cell size in pixels, then for its attributes", func() {
			m := newOn(viewer.Terminal{Graphics: graphics.Kitty}, withAttachments(), b)
			Expect(m.Init()()).To(Equal(tea.RawMsg{Msg: "\x1b[16t\x1b[c"}))
		})

		It("holds every image until the terminal has answered its attributes", func() {
			m, _ := newOn(viewer.Terminal{Graphics: graphics.Kitty}, withAttachments(), b).
				Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			m, cmd := press(m, "enter")
			m, msgs := drainMsgs(m, cmd)
			Expect(raws(msgs)).To(BeEmpty())

			m, cmd = m.Update(uv.PrimaryDeviceAttributesEvent{62, 22})
			_, msgs = drainMsgs(m, cmd)
			Expect(raws(msgs)).To(ContainSubstring("\x1b_Ga=t,f=100,t=d,i=1,q=2;"))
		})

		It("sends each image it reads, then draws it through placeholder cells with its caption", func() {
			m, cmd := press(openWith(withAttachments(), b, graphics.Kitty), "enter")
			m, msgs := drainMsgs(m, cmd)

			Expect(raws(msgs)).To(ContainSubstring("\x1b_Ga=t,f=100,t=d,i=1,q=2;"))
			Expect(raws(msgs)).To(ContainSubstring("\x1b_Ga=t,f=100,t=d,i=2,q=2;"))
			matchGolden("attachments-kitty", screen(m))
		})

		It("sizes an event's image to the window: as wide as it fits, or half as tall as the list", func() {
			m, cmd := press(openWith(withAttachments(), b, graphics.Kitty), "enter")
			_, msgs := drainMsgs(m, cmd)

			Expect(raws(msgs)).To(ContainSubstring("a=p,U=1,i=1,p=1,c=22,r=8,q=2"))
		})

		It("sends nothing for an image it cannot read, and shows its placeholder", func() {
			delete(b.content, pngSHA)
			delete(b.content, grafanaSHA)
			m, cmd := press(openWith(withAttachments(), b, graphics.Kitty), "enter")
			m, msgs := drainMsgs(m, cmd)

			Expect(raws(msgs)).To(BeEmpty())
			Expect(screen(m)).To(ContainSubstring("[image: p99.png · 184 KB]"))
		})

		It("keeps every line of an expanded event within reach of the movement keys", func() {
			m, cmd := press(openWith(withAttachments(), b, graphics.Kitty), "enter")
			m, _ = drainMsgs(m, cmd)
			seen := []string{screen(m)}
			for _, k := range []string{"j", "k", "G", "g"} {
				m, _ = press(m, k)
				seen = append(seen, screen(m))
			}

			Expect(strings.Join(seen, "\n")).To(SatisfyAll(
				ContainSubstring("p99.png ·"), ContainSubstring("mongod.log"), ContainSubstring("replSetStepDown"),
				ContainSubstring("grafana.png ·"), ContainSubstring("heartbeat"),
			))
		})

		It("places the images again at the new size when the window resizes", func() {
			m, cmd := press(openWith(withAttachments(), b, graphics.Kitty), "enter")
			m, _ = drainMsgs(m, cmd)
			m, cmd = m.Update(tea.WindowSizeMsg{Width: 20, Height: 20})
			_, msgs := drainMsgs(m, cmd)

			Expect(raws(msgs)).To(ContainSubstring("a=p,U=1,i=1,p=1,c=14,r=6,q=2"))
		})

		It("places the images again when the terminal reports its cell size", func() {
			m, cmd := press(openWith(withAttachments(), b, graphics.Kitty), "enter")
			m, _ = drainMsgs(m, cmd)
			m, cmd = m.Update(uv.CellSizeEvent{Width: 19, Height: 39})
			_, msgs := drainMsgs(m, cmd)

			Expect(raws(msgs)).To(ContainSubstring("a=p,U=1,i=1,p=1,c=3,r=1,q=2"))
		})
	})

	It("shows placeholders and sends nothing on a terminal with Kitty graphics but no color", func() {
		m := newOn(viewer.Terminal{Graphics: graphics.Kitty, NoColor: true}, withAttachments(), b)
		Expect(m.Init()).To(BeNil())

		next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		next, _ = next.Update(uv.PrimaryDeviceAttributesEvent{62, 22})
		next, cmd := press(next, "enter")
		next, msgs := drainMsgs(next, cmd)

		Expect(raws(msgs)).To(BeEmpty())
		Expect(screen(next)).To(ContainSubstring("[image: p99.png · 40×30 · 184 KB]"))
	})

	DescribeTable("the color profile it pins on a terminal",
		func(term viewer.Terminal, want colorprofile.Profile) {
			Expect(viewer.ColorProfile(term)).To(Equal(want))
		},
		Entry("16 colors, which the terminal's theme picks shades for", viewer.Terminal{}, colorprofile.ANSI),
		Entry("24-bit color for Kitty graphics, whose image ids travel as colors", viewer.Terminal{Graphics: graphics.Kitty}, colorprofile.TrueColor),
		Entry("no color when asked, even with Kitty graphics", viewer.Terminal{Graphics: graphics.Kitty, NoColor: true}, colorprofile.ASCII),
	)
})
