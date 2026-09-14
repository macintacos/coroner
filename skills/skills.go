// Package skills embeds the agent skills that ship inside coroner. go:embed cannot reach a
// parent directory, so the embedding lives beside the skills rather than in internal/cli.
package skills

import _ "embed"

// Coroner is coroner/SKILL.md, which `coroner agent skill` prints.
//
//go:embed coroner/SKILL.md
var Coroner []byte
