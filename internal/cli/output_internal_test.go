package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("output.rows", func() {
	It("aligns columns on a terminal", func() {
		var out bytes.Buffer

		Expect(output{out: &out, outTTY: true}.rows([][]string{{"*", "a", "x"}, {"", "long-name", "y"}})).To(Succeed())

		Expect(out.String()).To(Equal("*  a          x\n   long-name  y\n"))
	})
})

var _ = Describe("command record objects", func() {
	// tokens returns b's JSON tokens, so encodings that escape <, > and & differently compare equal.
	tokens := func(b []byte) []json.Token {
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.UseNumber()
		var out []json.Token
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				return out
			}
			Expect(err).NotTo(HaveOccurred())
			out = append(out, token)
		}
	}
	attachments := []json.Token{"attachments", json.Delim('['), json.Delim(']')}

	created := time.Date(2026, time.September, 12, 21, 30, 0, 0, time.UTC)
	updated := created.Add(time.Hour)
	ended := created.Add(2 * time.Minute)
	offset := -4 * 3600
	raw, source, url, author, eventID := "9:30 <pm> & on", "pager<duty>", "https://example.com/?a=1&b=<2>", "Priya & <Shah>", "evt_1"
	fullEvent := timeline.Event{
		ID: "evt_1", TimelineID: "tl_1", Seq: 3, OccurredAt: created, OccurredOffset: &offset,
		Precision: timeline.PrecisionMinute, Approximate: true, OccurredRaw: &raw, EndedAt: &ended,
		Source: &source, Tags: []string{"db", "<a&b>"}, Text: "disk <full> & failing", URL: &url,
		Author: &author, CreatedAt: created, UpdatedAt: updated,
	}
	bareEvent := timeline.Event{
		ID: "evt_2", TimelineID: "tl_1", OccurredAt: created, Precision: timeline.PrecisionSecond,
		Text: "page", CreatedAt: created, UpdatedAt: created,
	}
	fullAnnotation := timeline.Annotation{
		ID: "ann_1", TimelineID: "tl_1", EventID: &eventID, Kind: timeline.AnnotationEvidence,
		Body: "log <line> & more", Author: &author, Source: &source, URL: &url, CreatedAt: created, UpdatedAt: updated,
	}
	bareAnnotation := timeline.Annotation{
		ID: "ann_2", TimelineID: "tl_1", Kind: timeline.AnnotationComment, Body: "why", CreatedAt: created, UpdatedAt: created,
	}
	refsTimeline := timeline.Timeline{
		ID: "tl_1", Name: "inc-1", Title: "DB <outage> & recovery", Refs: []string{"INC-1", "https://x/?a&b"},
		CreatedAt: created, UpdatedAt: updated,
	}
	bareTimeline := timeline.Timeline{ID: "tl_2", Name: "inc-2", CreatedAt: created, UpdatedAt: created}
	authoredLink := timeline.Link{
		ID: "lnk_1", FromEventID: "evt_1", ToEventID: "evt_2", Kind: timeline.LinkLedTo, CreatedAt: created, Author: &author,
	}
	bareLink := timeline.Link{ID: "lnk_2", FromEventID: "evt_1", ToEventID: "evt_2", Kind: timeline.LinkSameAs, CreatedAt: created}

	DescribeTable("encode under --json as their export record's JSONL keys and values, then their output-only keys",
		func(object, record any, outputOnly []json.Token) {
			var cli, jsonl bytes.Buffer
			Expect(writeJSON(&cli, false, object)).To(Succeed())
			Expect(export.Write(&jsonl, record)).To(Succeed())
			want := tokens(jsonl.Bytes())
			Expect(tokens(cli.Bytes())).To(Equal(slices.Insert(want, len(want)-1, outputOnly...)))
		},
		Entry("an event with every field set", newEventJSON(fullEvent, nil), export.NewEvent(fullEvent), attachments),
		Entry("an event with no seq, no tags and every nullable field null", newEventJSON(bareEvent, nil), export.NewEvent(bareEvent), attachments),
		Entry("an annotation on an event with every field set", newAnnotationJSON(fullAnnotation, nil), export.NewAnnotation(fullAnnotation), attachments),
		Entry("an annotation on its whole timeline with every nullable field null", newAnnotationJSON(bareAnnotation, nil), export.NewAnnotation(bareAnnotation), attachments),
		Entry("a timeline with refs", newTimelineJSON(refsTimeline), export.NewTimeline(refsTimeline), nil),
		Entry("a timeline with nil refs", newTimelineJSON(bareTimeline), export.NewTimeline(bareTimeline), nil),
		Entry("a link with an author", newLinkJSON(authoredLink), export.NewLink(authoredLink), nil),
		Entry("a link with no author", newLinkJSON(bareLink), export.NewLink(bareLink), nil),
	)

	It("encodes a record with no tags or refs as [] under --json", func() {
		var event, tl bytes.Buffer

		Expect(writeJSON(&event, false, newEventJSON(bareEvent, nil))).To(Succeed())
		Expect(writeJSON(&tl, false, newTimelineJSON(bareTimeline))).To(Succeed())

		Expect(event.String()).To(ContainSubstring(`"tags":[]`))
		Expect(tl.String()).To(ContainSubstring(`"refs":[]`))
	})
})
