# Timeline rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **The package is pure:** `internal/timeline` imports the standard library only and
   never reads a clock, generates a UUID or does I/O. IDs and instants arrive as field
   values the caller fills. The `timeline-pure` depguard rule and forbidigo in
   [`.golangci.yml`](../../.golangci.yml) catch part of this, not all of it.
2. **`FormatInstant` is the only producer of the canonical instant text** (fixed width,
   UTC, nine fractional digits). Never store or export an instant formatted with
   `time.RFC3339Nano`, and never hand a `time.Time` to the SQLite driver: both break
   lexical ordering.
3. **An occurred at is the start of its precision window** in the offset `OccurredOffset`
   states (UTC when nil). Build it with `Precision.Truncate` on the time in that offset,
   never by hand; `Truncate` measures in the fixed offset its argument carries.
4. **Validation rejects and never normalizes.** The timeline name, source and tag grammars
   may only loosen, because tightening one breaks stored data.
5. **Events order by occurred at, then seq, through `CompareEvents`**, never by created
   at.
