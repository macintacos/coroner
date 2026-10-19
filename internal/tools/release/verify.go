package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxBinarySize caps extraction against a decompression bomb (gosec G110).
const maxBinarySize = 256 << 20

type target struct{ goos, goarch string }

// targets are the platforms a release builds; keep in sync with .goreleaser.yaml's builds,
// and archiveFor with its default archive name_template.
func targets() []target {
	return []target{{"darwin", "amd64"}, {"darwin", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}}
}

type verifyResult struct {
	OK            bool   `json:"ok"`
	Tag           string `json:"tag"`
	ReleaseURL    string `json:"release_url"`
	Prerelease    bool   `json:"prerelease"`
	Archive       string `json:"archive"`
	VersionOutput string `json:"version_output"`
}

type asset struct {
	Name string `json:"name"`
}

type release struct {
	URL          string  `json:"url"`
	IsPrerelease bool    `json:"isPrerelease"`
	Body         string  `json:"body"`
	Assets       []asset `json:"assets"`
}

// verify checks the published release of a tag: its assets, flags and notes, this platform's
// archive and binary, and, for a final tag, the Homebrew cask.
func verify(ctx context.Context, sys system, args []string) (any, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("tag", "", "the released tag")
	err := parseFlags(flags, args, "tag")
	if err != nil {
		return nil, err
	}
	v, ok := parseTag(*name)
	if !ok {
		return nil, &toolError{Code: "bad_tag", Message: *name + " is not vX.Y.Z or vX.Y.Z-rc.N"}
	}

	rel, err := viewRelease(ctx, sys, *name)
	if err != nil {
		return nil, err
	}
	err = checkRelease(ctx, sys, *name, rel)
	if err != nil {
		return nil, err
	}

	platform := target{sys.goos, sys.goarch}
	if !slices.Contains(targets(), platform) {
		return nil, &toolError{Code: "unsupported_platform", Message: "no release archive for " + sys.goos + "/" + sys.goarch}
	}
	archive := archiveFor(*name, platform)
	versionOutput, sums, err := checkBinary(ctx, sys, *name, archive)
	if err != nil {
		return nil, err
	}

	if v.rc < 0 {
		err = checkCask(ctx, sys, *name, sums)
		if err != nil {
			return nil, err
		}
	}

	return verifyResult{OK: true, Tag: *name, ReleaseURL: rel.URL, Prerelease: rel.IsPrerelease, Archive: archive, VersionOutput: versionOutput}, nil
}

func archiveFor(tag string, t target) string {
	return fmt.Sprintf("coroner_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), t.goos, t.goarch)
}

func viewRelease(ctx context.Context, sys system, name string) (release, error) {
	return ghJSON[release](ctx, sys, "the release", "release", "view", name, "-R", repo, "--json", "url,isPrerelease,body,assets")
}

// checkRelease checks the release's assets, its prerelease flag and that its body is the
// tag's message.
func checkRelease(ctx context.Context, sys system, name string, rel release) error {
	want := []string{"checksums.txt"}
	for _, t := range targets() {
		archive := archiveFor(name, t)
		want = append(want, archive, archive+".sbom.json")
	}
	for _, w := range want {
		if !slices.Contains(rel.Assets, asset{Name: w}) {
			return &toolError{Code: "asset_missing", Message: name + " has no asset " + w}
		}
	}

	if rel.IsPrerelease != strings.Contains(name, "-rc.") {
		return &toolError{Code: "prerelease_mismatch", Message: fmt.Sprintf("%s is marked prerelease: %t", name, rel.IsPrerelease)}
	}

	message, err := tagMessage(ctx, sys, name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(rel.Body) != strings.TrimSpace(message) {
		return &toolError{Code: "release_body_mismatch", Message: name + "'s release body is not its tag's message"}
	}

	return nil
}

// checkBinary downloads this platform's archive into a temporary directory, checks it against
// checksums.txt, and returns the coroner_version its coroner reports and the SHA-256
// sums checksums.txt lists, keyed on file name.
func checkBinary(ctx context.Context, sys system, name, archive string) (string, map[string]string, error) {
	dir, err := os.MkdirTemp("", "coroner-verify-")
	if err != nil {
		return "", nil, &toolError{Code: "internal", Message: err.Error()}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	_, err = gh(ctx, sys, "release", "download", name, "-R", repo, "-D", dir, "-p", "checksums.txt", "-p", archive)
	if err != nil {
		return "", nil, err
	}
	sums, err := checkChecksums(dir, name, archive)
	if err != nil {
		return "", nil, err
	}

	binary := filepath.Join(dir, "coroner")
	err = extractBinary(filepath.Join(dir, archive), binary)
	if err != nil {
		return "", nil, &toolError{Code: "internal", Message: "extracting coroner from " + archive + ": " + err.Error()}
	}
	out, err := sys.output(ctx, binary, "version", "--json")
	if err != nil {
		return "", nil, &toolError{Code: "version_mismatch", Message: err.Error()}
	}
	var reported struct {
		CoronerVersion string `json:"coroner_version"`
	}
	err = json.Unmarshal([]byte(out), &reported)
	if err != nil || reported.CoronerVersion != name {
		return "", nil, &toolError{Code: "version_mismatch", Message: "coroner version --json printed " + out}
	}

	return reported.CoronerVersion, sums, nil
}

// checkChecksums requires a checksums.txt line for every target's archive, and the
// downloaded archive's SHA-256 to match its line, and returns the sums it lists.
func checkChecksums(dir, name, archive string) (map[string]string, error) {
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums.txt")) //nolint:gosec // G304: a file the tool just downloaded
	if err != nil {
		return nil, &toolError{Code: "internal", Message: err.Error()}
	}
	sums := map[string]string{}
	for line := range strings.Lines(string(checksums)) {
		if sum, file, ok := strings.Cut(strings.TrimSpace(line), "  "); ok {
			sums[file] = sum
		}
	}
	for _, t := range targets() {
		if _, ok := sums[archiveFor(name, t)]; !ok {
			return nil, &toolError{Code: "checksum_mismatch", Message: "checksums.txt has no line for " + archiveFor(name, t)}
		}
	}

	downloaded, err := os.ReadFile(filepath.Join(dir, archive)) //nolint:gosec // G304: a file the tool just downloaded
	if err != nil {
		return nil, &toolError{Code: "internal", Message: err.Error()}
	}
	archiveSum := sha256.Sum256(downloaded)
	if hex.EncodeToString(archiveSum[:]) != sums[archive] {
		return nil, &toolError{Code: "checksum_mismatch", Message: archive + "'s SHA-256 is not checksums.txt's"}
	}

	return sums, nil
}

// extractBinary writes the archive's coroner entry to out as an executable.
func extractBinary(archive, out string) error {
	f, err := os.Open(archive) //nolint:gosec // G304: a file the tool just downloaded
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("no coroner entry")
		}
		if err != nil {
			return err
		}
		if header.Name == "coroner" {
			return writeExecutable(out, tr)
		}
	}
}

func writeExecutable(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700) //nolint:gosec // G302: the binary must be executable
	if err != nil {
		return err
	}
	_, err = io.CopyN(f, r, maxBinarySize)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = f.Close()
		return err
	}

	return f.Close()
}

// checkCask requires the tap's cask to name the version and each darwin archive's SHA-256.
func checkCask(ctx context.Context, sys system, name string, sums map[string]string) error {
	cask, err := gh(ctx, sys, "api", "repos/macintacos/homebrew-tap/contents/Casks/coroner.rb", "-H", "Accept: application/vnd.github.raw")
	if err != nil {
		return err
	}
	want := []string{`version "` + strings.TrimPrefix(name, "v") + `"`}
	for _, t := range targets() {
		if t.goos == "darwin" {
			want = append(want, sums[archiveFor(name, t)])
		}
	}
	for _, w := range want {
		if !strings.Contains(cask, w) {
			return &toolError{Code: "cask_mismatch", Message: "Casks/coroner.rb lacks " + w}
		}
	}

	return nil
}
