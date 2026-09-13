# Time parsing rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **The package is pure:** `internal/timeparse` takes `now` and `loc` as parameters and
   never calls `time.Now`, reads `time.Local` or touches `os`. Resolving `loc` from `--tz`
   and the config file belongs to the caller.
2. **The golden corpus is the behaviour contract:** a row in
   [`corpus_test.go`](../../internal/timeparse/corpus_test.go) that changes after a
   go-dateparser upgrade is a change in what users' input means. Decide on it; never edit
   the row just to make the suite pass.
3. **DST gaps and folds are rejected, never guessed:** a naive wall time that a DST change
   skips or repeats returns `ErrDSTGap` or `ErrDSTFold` with a hint naming the offsets.
   Never resolve one through `time.Date`, which picks silently.
4. **RFC 3339 is the stable input grammar;** natural language is experimental and may
   change with the library. Scripts, agents and contract tests send RFC 3339.
5. **A stated zone wins:** an input that names an offset or zone is read in it, relative
   words included, whatever `loc` is.
