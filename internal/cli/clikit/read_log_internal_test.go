package clikit

import (
	"bytes"
	"encoding/json"
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

var _ = DescribeTable("filterValue logs what narrowed a read of events, never --contains' text",
	func(f store.EventFilter, want map[string]any) {
		var buf bytes.Buffer
		slog.New(slog.NewJSONHandler(&buf, nil)).Info("read", slog.Any("filter", FilterValue(f)))
		var line map[string]any
		Expect(json.Unmarshal(buf.Bytes(), &line)).To(Succeed())

		if want == nil {
			Expect(line).NotTo(HaveKey("filter"))
			return
		}
		Expect(line).To(HaveKeyWithValue("filter", want))
	},
	Entry("sources", store.EventFilter{Sources: []string{"pagerduty"}}, map[string]any{"sources": []any{"pagerduty"}}),
	Entry("tags", store.EventFilter{Tags: []string{"db"}}, map[string]any{"tags": []any{"db"}}),
	Entry("contains as true", store.EventFilter{Contains: "secret text"}, map[string]any{"contains": true}),
	Entry("a descending order", store.EventFilter{Order: store.OrderDesc}, map[string]any{"order": "desc"}),
	Entry("a limit", store.EventFilter{Limit: 5}, map[string]any{"limit": float64(5)}),
	Entry("nothing, leaving filter out", store.EventFilter{}, nil),
)
