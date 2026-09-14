package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("event add --from-file", func() {
	It("builds a 200-event timeline in one call, and running it again adds nothing", func(ctx SpecContext) {
		home := GinkgoT().TempDir()
		var out, errOut bytes.Buffer
		run := func(stdin string, args ...string) int {
			out.Reset()
			errOut.Reset()
			return cli.Run(ctx, cli.App{
				Args: append([]string{"--db", filepath.Join(home, "coroner.db")}, args...),
				In:   strings.NewReader(stdin), Out: &out, Err: &errOut,
				LookupEnv: func(key string) (string, bool) { return map[string]string{"HOME": home}[key], key == "HOME" },
				ReadFile:  readNoFile,
				Now:       func() time.Time { return time.Date(2026, time.September, 11, 4, 0, 0, 0, time.UTC) },
				NewID:     uuid.NewV7,
				OpenStore: store.Open,
			})
		}

		Expect(run("", "timeline", "create", "mongo-failover", "--json")).To(Equal(0), errOut.String())
		var created struct {
			ID string `json:"id"`
		}
		Expect(json.Unmarshal(out.Bytes(), &created)).To(Succeed())

		var batch strings.Builder
		wantIDs := make([]string, 200)
		for i := range wantIDs {
			wantIDs[i] = fmt.Sprintf("01a0a07c-0000-7000-8000-%012d", i+1)
			// Earlier in time the further down the file, so seqs follow the file, not the times.
			at := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Second).Format("2006-01-02T15:04:05.000000000Z")
			fmt.Fprintf(&batch, `{"type":"event","id":%q,"timeline_id":%q,"seq":null,"occurred_at":%q,"occurred_offset":null,`+
				`"precision":"second","approximate":false,"occurred_raw":null,"ended_at":null,"source":null,"tags":[],`+
				`"text":"Replica lag sample %d","url":null,"author":"julian","created_at":%[3]q,"updated_at":%[3]q}`+"\n",
				wantIDs[i], created.ID, at, i+1)
		}
		type event struct {
			ID  string `json:"id"`
			Seq int64  `json:"seq"`
		}
		var result struct {
			Inserted  []event `json:"inserted"`
			Identical []event `json:"identical"`
		}
		addBatch := func() {
			GinkgoHelper()
			result.Inserted, result.Identical = nil, nil
			Expect(run(batch.String(), "event", "add", "-t", "mongo-failover", "--from-file", "-", "--json")).To(Equal(0), errOut.String())
			Expect(json.Unmarshal(out.Bytes(), &result)).To(Succeed())
		}

		addBatch()
		want := make([]event, len(wantIDs))
		for i, id := range wantIDs {
			want[i] = event{ID: id, Seq: int64(i + 1)}
		}
		Expect(result.Inserted).To(Equal(want))
		Expect(result.Identical).To(BeEmpty())

		addBatch()
		Expect(result.Inserted).To(BeEmpty())
		Expect(result.Identical).To(Equal(want))
	})
})
