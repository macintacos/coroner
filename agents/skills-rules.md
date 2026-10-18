# Skills rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **Front matter holds only the Agent Skills specification's keys:** `name`,
   `description` and `compatibility`, never a Claude Code extension key such as
   `allowed-tools` or `when_to_use`, and `name` matches the skill's directory. A spec in
   [`internal/cli/agent_test.go`](../internal/cli/agent_test.go) fails otherwise, and
   another when a relative link in `SKILL.md` or a reference breaks.
2. **Command lines go in `sh` blocks with real values:** the examples test skips a line
   with `$(…)` or backticks, and stops a line at `<`, so never write a `<placeholder>`
   there.
3. **Teach only stable surfaces:** name an experimental one only as experimental, as
   [`STABILITY.md`](../STABILITY.md#experimental-in-the-mvp) lists them.
4. **Nothing but the skill goes inside `skills/coroner/`:** the copy install and the
   plugin take the whole directory. [`skills/skills.go`](../skills/skills.go) embeds
   `SKILL.md` alone, which `coroner agent skill` prints, so `SKILL.md` stands without its
   references.
5. **`SKILL.md`'s body stays under 500 lines,** and each doc in the skill keeps its
   audience line after the front matter.
