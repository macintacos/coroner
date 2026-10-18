# Releasing coroner

*Audience: maintainers cutting a coroner release.*

A release is a pushed `v*` tag. The
[Release workflow](https://github.com/macintacos/coroner/blob/main/.github/workflows/release.yml)
writes the release notes with git-cliff, then runs goreleaser with
[`.goreleaser.yaml`](https://github.com/macintacos/coroner/blob/main/.goreleaser.yaml).
goreleaser publishes a GitHub release with an archive and a `.sbom.json` per platform and
`checksums.txt`, and updates the cask in `macintacos/homebrew-tap`. The steps below check
locally what the tag will publish before it is pushed, then check the published release.

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

## Before tagging

1. On an up-to-date `main`, run `mise run preflight`.
2. Run `mise run release:check`, then `mise run release:snapshot`, which fails if the
   build leaves a file git sees that was not there before it. Neither checks what `dist/`
   holds, so check by hand that it holds a `.tar.gz` archive and a `.sbom.json` per
   platform, `checksums.txt` and `dist/homebrew/Casks/coroner.rb`.
3. Pick the version, `vX.Y.Z`. A pre-release tag such as `vX.Y.Z-rc.N` publishes a GitHub
   pre-release and leaves the Homebrew cask alone.
4. Run `git cliff --tag vX.Y.Z -o CHANGELOG.md` and commit the result as
   `chore(release): vX.Y.Z`. Never edit `CHANGELOG.md` by hand. Get that commit onto
   `origin/main`, by pushing it or by merging the pull request that carries it and
   pulling, then wait for CI on it to pass: the Release workflow runs no checks of its
   own.
5. Tag that commit and push the tag:

   ```sh
   git tag -a vX.Y.Z -m vX.Y.Z
   git push origin vX.Y.Z
   ```

## After the workflow finishes

1. Open the release on GitHub and check the archives, the SBOMs, `checksums.txt` and the
   notes.
2. Download the archive for your machine, check it against `checksums.txt`, and check that
   `coroner version` prints exactly `vX.Y.Z`.
3. Run `brew update && brew install --cask macintacos/tap/coroner` (or
   `brew upgrade --cask coroner`) and check that the installed binary runs. While releases
   are unsigned macOS may refuse it, as the install page describes; if the install page no
   longer matches what macOS does, update it.
4. Add the release's fixture database, following
   [Adding a release's fixture](migrations.md#adding-a-releases-fixture).

## Rebuilding a tag

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
