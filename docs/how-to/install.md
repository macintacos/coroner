# Install coroner

*Audience: anyone installing, upgrading or removing coroner on macOS or Linux.*

coroner is a single binary for macOS and Linux, on amd64 and arm64. Windows is not
supported. coroner never contacts the network on its own: it has no telemetry and no
update checks. To upgrade, repeat the install method you used with a newer version.

On macOS, use `go install` for now. The release binaries that Homebrew and the release
archives ship are not signed yet, so macOS may refuse to run them (see
[macOS signing and notarization](#macos-signing-and-notarization)).

Whichever method you use, check the install with:

```sh
coroner version
```

## go install

Needs [Go](https://go.dev/doc/install). Go builds coroner on your machine and installs it
into `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is unset; that directory
must be on your `PATH`.

```sh
go install github.com/macintacos/coroner@latest
```

Run the same command again to upgrade. Name a version instead of `latest` to install that
one, such as `@v0.1.0`.

This method installs the binary only. To add shell completions, see
`coroner completion --help`.

To uninstall, delete the binary from the directory Go installed it into:

```sh
gobin="$(go env GOBIN)"
rm "${gobin:-$(go env GOPATH)/bin}/coroner"
```

## Homebrew on macOS

Needs [Homebrew](https://brew.sh). The cask installs an unsigned binary, so read
[macOS signing and notarization](#macos-signing-and-notarization) before choosing it over
`go install`.

```sh
brew install --cask macintacos/tap/coroner
```

The cask also installs the bash, zsh and fish completions and the `coroner` man page.

Upgrade with `brew update && brew upgrade --cask coroner`. Each new version is unsigned
too, so macOS may refuse it again after an upgrade. To uninstall:

```sh
brew uninstall --cask coroner
brew untap macintacos/tap   # only if you installed nothing else from the tap
```

## Download a release archive

Each release on the [releases page](https://github.com/macintacos/coroner/releases) has
one archive per platform, named `coroner_<version>_<os>_<arch>.tar.gz`. Beside the
archives are `checksums.txt` and one `.sbom.json` per archive, a software bill of
materials listing what that binary was built from; you do not need the SBOMs to install.
Each archive holds:

- `coroner`, the binary;
- `LICENSE`;
- `completions/coroner.bash`, `completions/coroner.zsh` and `completions/coroner.fish`;
- `manpages/`, one man page per command.

Download the archive for your platform, such as `coroner_0.1.0_linux_arm64.tar.gz`, and
`checksums.txt` into one directory, then check the archive against its checksum.
`checksums.txt` lists every file in the release, so `--ignore-missing` skips the ones you
did not download:

```sh
sha256sum --check --ignore-missing checksums.txt          # Linux
shasum -a 256 --check --ignore-missing checksums.txt      # macOS
```

Unpack it and put the binary in a directory on your `PATH`, such as `~/.local/bin`:

```sh
tar -xzf coroner_<version>_<os>_<arch>.tar.gz
mkdir -p ~/.local/bin && install -m 0755 coroner ~/.local/bin/coroner
```

Optionally, install the man pages and the completions for your shell. Run only the
completions line for the shell you use:

```sh
mkdir -p ~/.local/share/man/man1 && cp manpages/*.1 ~/.local/share/man/man1/
```

```sh
# bash
mkdir -p ~/.local/share/bash-completion/completions && cp completions/coroner.bash ~/.local/share/bash-completion/completions/coroner
```

```zsh
# zsh: any directory in $fpath that you can write to works; ${fpath[1]} is the first one
cp completions/coroner.zsh "${fpath[1]}/_coroner"
```

```sh
# fish
mkdir -p ~/.config/fish/completions && cp completions/coroner.fish ~/.config/fish/completions/coroner.fish
```

To upgrade, repeat these steps with the new release. To uninstall, delete the files you
copied, running the completions line from the shell it names:

```sh
rm ~/.local/bin/coroner
rm -f ~/.local/share/man/man1/coroner.1 ~/.local/share/man/man1/coroner-*.1
rm -f ~/.local/share/bash-completion/completions/coroner   # bash
rm -f "${fpath[1]}/_coroner"                               # zsh
rm -f ~/.config/fish/completions/coroner.fish              # fish
```

## macOS signing and notarization

macOS Gatekeeper checks that software downloaded from the internet is signed with an Apple
Developer ID and notarized by Apple. coroner's release binaries are neither yet. There are
two ways to settle that, and the maintainer has not chosen between them; this section
changes when they do:

- **Sign and notarize in the release pipeline.** The cask and the release archives would
  then run without a warning. It needs a paid Apple Developer account and its credentials
  stored as release secrets.
- **Keep the release binaries unsigned for good.** `go install` stays the macOS method,
  since a binary Go builds on your own Mac is not quarantined and Gatekeeper never checks
  it, and the cask keeps the behaviour described below.

Until the maintainer decides:

- `go install` is the recommended way to install coroner on macOS.
- The Homebrew cask installs the unsigned binary as it is. goreleaser can add an install
  hook that strips the quarantine flag, but that would switch off Gatekeeper's check for
  everyone who installs the cask, so the cask leaves it out.
- macOS may refuse to open a coroner installed or upgraded by the cask, or downloaded with
  a browser. An archive downloaded with `curl` is not quarantined.

If you have checked the archive against `checksums.txt` and still want the cask or a
browser download, you can let that one file run yourself: after macOS refuses it, choose
**Open Anyway** under System Settings > Privacy & Security, or remove its quarantine flag.
Either skips Apple's check for that file:

```sh
xattr -d com.apple.quarantine "$(brew --prefix)/bin/coroner"   # the cask
xattr -d com.apple.quarantine ~/.local/bin/coroner             # a browser download
```
