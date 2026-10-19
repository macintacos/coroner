# Time text rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **The package is pure:** `internal/timetext` imports only the standard library and
   `internal/timeline`, takes the zone and every instant as parameters, and never reads a
   clock, reads `time.Local` or touches `os`. The `timetext-pure` depguard rule in
   [`.golangci.yml`](../.golangci.yml) enforces the imports.
2. **It is the one place an event's time becomes text for people:** the clock to its
   precision, the day an event is filed under, each part's date, the approximate marker,
   the zone label of a set of times and the export stamp. Surfaces keep only layout: bold,
   code spans, `–` or `to`, `~` or `approximately`, `Jan 02` or `2006-01-02`, padding and
   widths. Never add a time rule to a surface.
3. **Its table spec is the behaviour contract for people-facing time text:** a row in
   [`timetext_test.go`](../internal/timetext/timetext_test.go) that changes is a change in
   what people read on every surface. Decide on it; never edit the row just to make the
   suite pass.
4. **A coarse window that does not line up in the zone reads in its own offset:** an hour
   at +05:30 shown in UTC is `10:-- +05:30`, never `04:30`, and an interval's end is read
   where its start is. Every event is still filed under the day its instant falls on in
   the zone, so a part on another day shows its date.
