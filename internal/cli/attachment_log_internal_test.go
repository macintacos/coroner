package cli

import (
	"bytes"
	"encoding/json"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("attachResult.logAdded", func() {
	It("logs a dry run's attachment, which has no id yet, by its owner, size and sha256 alone", func(ctx SpecContext) {
		var logged bytes.Buffer
		eventID := "evt-1"
		r := attachResult{added: []timeline.Attachment{{
			Owner: timeline.AttachmentOwner{EventID: &eventID}, Size: 5, SHA256: "abc",
		}}}

		r.logAdded(ctx, slog.New(slog.NewJSONHandler(&logged, nil)))

		var line map[string]any
		Expect(json.Unmarshal(logged.Bytes(), &line)).To(Succeed())
		Expect(line).NotTo(HaveKey("attachment"))
		Expect(line).To(HaveKeyWithValue("event", "evt-1"))
		Expect(line).To(HaveKeyWithValue("sha256", "abc"))
	})
})
