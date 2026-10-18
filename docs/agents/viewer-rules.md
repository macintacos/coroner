# Viewer rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **The viewer never holds the database open:** it reads only through `Options.Load`,
   which opens the store, reads in deferred transactions and closes it on every call.
   Never keep a `*store.Store`, a connection or a transaction in the model, and never read
   while rendering: an agent keeps running `coroner event add` while a person watches. The
   `viewer-no-store` depguard rule in [`.golangci.yml`](../../.golangci.yml) fails any
   non-test viewer file that imports `internal/store`. The timeline picker reads nothing
   itself: its rows arrive in `PickOptions`, read from a store the command closes before
   the picker opens.
2. **Filtering re-queries the store:** a filter key changes the viewer's `Match` and calls
   `Load`, so the viewer keeps exactly the events `coroner event list` keeps with the same
   flags. Never filter or sort events in memory. The picker's `/` narrows its timeline
   rows in memory, which filters no events.
3. **Order and gaps come from the domain:** events arrive in `timeline.CompareEvents`
   order, and gap rows come from `timeline.Gaps`. Never compute either in the view.
4. **Color never carries meaning alone:** evidence and comments, gaps, day breaks, the
   current timeline and the cursor each have a glyph or word as well, so `NO_COLOR` and
   `--no-color` lose nothing. Use the 16 ANSI colors, which the terminal's theme picks
   shades for.
5. **The screen is pinned by goldens:** `testdata/*.golden` hold `View()` at 80×20 in
   America/New_York, with a fixed clock and escape sequences stripped. Rewrite them only
   with `go test ./internal/viewer -update`, and read the diff before committing it.
