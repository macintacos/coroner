# Releasing coroner

*Audience: maintainers cutting a coroner release.*

A release is a pushed annotated `v*` tag whose message is the release notes. The
[Release workflow](https://github.com/macintacos/coroner/blob/main/.github/workflows/release.yml)
reads the notes from the tag, then runs goreleaser with
[`.goreleaser.yaml`](https://github.com/macintacos/coroner/blob/main/.goreleaser.yaml).
goreleaser publishes a GitHub release with an archive and a `.sbom.json` per platform and
`checksums.txt`, and updates the cask in `macintacos/homebrew-tap`. The `/release-coroner`
Claude Code skill runs the whole release, from the checks before tagging through checking
the published release.

## One-time setup

1. Create the public repository `macintacos/homebrew-tap`. goreleaser writes
   `Casks/coroner.rb` into it.
2. Create a fine-grained GitHub token with read and write access to the contents of
   `macintacos/homebrew-tap` only, and add it to `macintacos/coroner` as the Actions
   secret `HOMEBREW_TAP_GITHUB_TOKEN`. The workflow's own token cannot push to another
   repository. Never commit the token or paste it anywhere else.
3. Decide how macOS binaries are signed, and update the install page's
   [macOS signing and notarization](../how-to/install.md#macos-signing-and-notarization)
   section to match. This does not block a release: until the decision, releases ship
   unsigned and that section says what users should expect.

## Releasing

In Claude Code, on an up-to-date `main`, type:

```text
/release-coroner <patch|minor|major> [rc] [dry run]
```

The bump picks the next version from the latest final tag, or from `v0.0.0` when there is
none. `rc` makes a release candidate such as `v0.1.0-rc.0`, which publishes a GitHub
pre-release and leaves the Homebrew cask alone. `dry run` runs every check, prints the tag
and the notes, and creates no tag.

The skill drives `go run ./internal/tools/release`, which does each step and prints one
JSON object:

1. **`gates`** checks that you are on `main` with a clean tree level with `origin/main`,
   that `gh` is logged in, that `HOMEBREW_TAP_GITHUB_TOKEN` is set, and that CI passed on
   `HEAD`. Then it runs `mise run preflight`, `mise run release:check` and
   `mise run release:snapshot`.
2. **`compute`** works out the tag and gathers the pull requests and commits since the
   latest final tag, each PR's `stability:` line, and any change to the contract scripts,
   the export schema or a deprecation call.
3. The skill writes the notes from that data and shows you the tag, the notes, and every
   PR whose `stability:` line is missing or unfilled. You confirm once, or answer with an
   edit to the notes.
4. **`tag`** creates the annotated, unsigned tag with the notes as its message, byte for
   byte, and pushes it. It refuses a tag that already exists locally or on `origin`.
5. **`watch`** waits for the Release workflow the tag started, and fails unless it
   succeeds.
6. **`verify`** checks the published release: every archive, SBOM and `checksums.txt`, the
   pre-release flag, the body against the tag's message, your platform's archive against
   `checksums.txt`, that the binary reports the tag as its version, and, for a final tag,
   the version and checksums in the Homebrew cask.

## Recovery

A failed Release workflow is re-run, never re-tagged: the tag already carries the notes.
Re-run it with `gh run rerun <id> -R macintacos/coroner`, then wait for it again with
`go run ./internal/tools/release watch --tag <tag> --sha <sha>` and check it with
`go run ./internal/tools/release verify --tag <tag>`. If the failed run already uploaded
assets, first delete the release as [Rebuilding a tag](#rebuilding-a-tag) describes.
[The skill](https://github.com/macintacos/coroner/blob/main/.claude/skills/release-coroner/SKILL.md)
names the recovery for every other failure.

### Rebuilding a tag

A tag is built again when its Release workflow is re-run, or when goreleaser builds it
locally. A re-run cannot repair a release that already has assets: goreleaser uploads the
archives before it pushes the cask, so a run that failed at the cask, for example on a
missing or under-scoped `HOMEBREW_TAP_GITHUB_TOKEN`, leaves them on the release, and a
re-run fails on their names. To rebuild or retry a published tag, delete its GitHub
release but not the tag, with `gh release delete vX.Y.Z` (without `--cleanup-tag`), then
re-run the Release workflow.

`checksums.txt` has a line for each archive and each SBOM. Building the same tag again
with the tools `mise.lock` pins produces byte-identical archives, so compare only the
archive lines. The SBOM lines change: syft writes a creation time and a random document ID
into every SBOM.

## After the release

1. Run `brew update && brew install --cask macintacos/tap/coroner` (or
   `brew upgrade --cask coroner`) and check that the installed binary runs. While releases
   are unsigned macOS may refuse it, as the install page describes; if the install page no
   longer matches what macOS does, update it.
2. Add the release's fixture database, following
   [Adding a release's fixture](migrations.md#adding-a-releases-fixture).
