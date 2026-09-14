# Releasing coroner

*Audience: maintainers cutting a coroner release.*

A release is a pushed `v*` tag. The
[Release workflow](https://github.com/macintacos/coroner/blob/main/.github/workflows/release.yml)
writes the release notes with git-cliff, then runs goreleaser with
[`.goreleaser.yaml`](https://github.com/macintacos/coroner/blob/main/.goreleaser.yaml).
goreleaser publishes a GitHub release with four archives (darwin and linux, amd64 and
arm64), `checksums.txt` and a separate `.sbom.json` per archive, and updates the cask in
`macintacos/homebrew-tap`. The steps below check locally what the tag will publish before
it is pushed, then check the published release.

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
2. Run `mise run release:check`, then `mise run release:snapshot`. Neither inspects what
   the snapshot built, so check by hand that `dist/` holds four `.tar.gz` archives,
   `checksums.txt`, four `.sbom.json` files and `dist/homebrew/Casks/coroner.rb`, and that
   `dist/coroner_<os>_<arch>*/coroner version` does not end in `+dirty`.
3. Pick the version, `vX.Y.Z`.
4. Run `git cliff --tag vX.Y.Z -o CHANGELOG.md` and commit the result to `main` as
   `chore(release): vX.Y.Z`. Never edit `CHANGELOG.md` by hand.
5. Tag that commit and push the tag:

   ```sh
   git tag -a vX.Y.Z -m vX.Y.Z
   git push origin vX.Y.Z
   ```

## After the workflow finishes

1. Open the release on GitHub and check the four archives, `checksums.txt`, the SBOMs and
   the notes.
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
locally. `checksums.txt` has a line for each archive and each SBOM. Building the same tag
again produces byte-identical archives, so their lines do not change. The SBOM lines do:
syft writes a creation time and a random document ID into every SBOM.
