package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	finalTag   = "v0.1.0"
	releaseURL = "https://github.com/macintacos/coroner/releases/tag/"
	caskCmd    = "gh api repos/macintacos/homebrew-tap/contents/Casks/coroner.rb -H Accept: application/vnd.github.raw"
	notes      = "### Added\n\n- a thing"
)

// verifyRelease is everything a fake release of tag publishes and downloads, with each
// archive's SHA-256 checksum keyed on its name.
type verifyRelease struct {
	tag        string
	prerelease bool
	body       string
	assets     []string
	binary     string // the extracted coroner prints this, so it doubles as `coroner version`
	checksums  map[string]string
	cask       string
	downloadTo string
}

func archiveName(tag, goos, goarch string) string {
	return fmt.Sprintf("coroner_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), goos, goarch)
}

func tarGz(name, content string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	Expect(tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))})).To(Succeed())
	_, err := tw.Write([]byte(content))
	Expect(err).NotTo(HaveOccurred())
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())

	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// newRelease describes a release of tag that verify accepts.
func newRelease(tag string) *verifyRelease {
	r := &verifyRelease{
		tag:        tag,
		prerelease: strings.Contains(tag, "-rc."),
		body:       "\n" + notes + "\n",
		assets:     []string{"checksums.txt"},
		binary:     "coroner version " + tag,
		checksums:  map[string]string{},
	}
	for _, t := range targets() {
		name := archiveName(tag, t.goos, t.goarch)
		r.assets = append(r.assets, name, name+".sbom.json")
		r.checksums[name] = sum([]byte(name))
	}
	r.checksums[archiveName(tag, "darwin", "arm64")] = sum(tarGz("coroner", r.binary))
	r.cask = fmt.Sprintf("version \"%s\"\nsha256 \"%s\"\nsha256 \"%s\"\n", strings.TrimPrefix(tag, "v"),
		r.checksums[archiveName(tag, "darwin", "amd64")], r.checksums[archiveName(tag, "darwin", "arm64")])

	return r
}

func (r *verifyRelease) fake() *fakeSystem {
	f := newFake()
	ref := "refs/tags/" + r.tag
	f.answers["git fetch --force origin "+ref+":"+ref] = ""
	f.answers["git cat-file -t "+ref] = "tag"
	f.answers["git for-each-ref --format=%(contents) "+ref] = notes
	f.answers[caskCmd] = r.cask

	f.respond = func(name string, args []string) (string, bool) {
		switch {
		case name == "gh" && slices.Equal(args[:2], []string{"release", "view"}):
			return r.view(), true
		case name == "gh" && slices.Equal(args[:2], []string{"release", "download"}):
			r.download(args[slices.Index(args, "-D")+1])
			return "", true
		case filepath.Base(name) == "coroner":
			out, err := os.ReadFile(filepath.Clean(name))
			Expect(err).NotTo(HaveOccurred())
			return string(out), true
		}

		return "", false
	}

	return f
}

func (r *verifyRelease) view() string {
	assets := make([]map[string]string, 0, len(r.assets))
	for _, a := range r.assets {
		assets = append(assets, map[string]string{"name": a})
	}
	out, err := json.Marshal(map[string]any{"url": releaseURL + r.tag, "isPrerelease": r.prerelease, "body": r.body, "assets": assets})
	Expect(err).NotTo(HaveOccurred())

	return string(out)
}

func (r *verifyRelease) download(dir string) {
	r.downloadTo = dir
	var lines strings.Builder
	for name, s := range r.checksums {
		fmt.Fprintf(&lines, "%s  %s\n", s, name)
	}
	Expect(os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(lines.String()), 0o600)).To(Succeed())
	archive := archiveName(r.tag, "darwin", "arm64")
	Expect(os.WriteFile(filepath.Join(dir, archive), tarGz("coroner", r.binary), 0o600)).To(Succeed())
}

func usedCask(f *fakeSystem) bool { return slices.Contains(f.calls, caskCmd) }

var _ = Describe("verify", func() {
	It("checks a release candidate without reading the cask", func() {
		r := newRelease(rcTag)
		f := r.fake()

		got, code := runJSON(f, "verify", "--tag", rcTag)

		Expect(code).To(Equal(0))
		Expect(got).To(Equal(map[string]any{
			"ok":             true,
			"tag":            rcTag,
			"release_url":    releaseURL + rcTag,
			"prerelease":     true,
			"archive":        "coroner_0.1.0-rc.0_darwin_arm64.tar.gz",
			"version_output": "coroner version v0.1.0-rc.0",
		}))
		Expect(usedCask(f)).To(BeFalse())
		Expect(r.downloadTo).NotTo(BeEmpty())
		Expect(r.downloadTo).NotTo(BeADirectory())
	})

	It("checks a final release's cask", func() {
		r := newRelease(finalTag)
		f := r.fake()

		got, code := runJSON(f, "verify", "--tag", finalTag)

		Expect(code).To(Equal(0), fmt.Sprint(got))
		Expect(got).To(HaveKeyWithValue("prerelease", false))
		Expect(usedCask(f)).To(BeTrue())
	})

	DescribeTable("fails a release that is not what was tagged",
		func(tag string, spoil func(*verifyRelease, *fakeSystem), want string) {
			r := newRelease(tag)
			f := r.fake()
			spoil(r, f)

			got, code := runJSON(f, "verify", "--tag", tag)

			Expect(code).To(Equal(1))
			Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", want)))
			if r.downloadTo != "" {
				Expect(r.downloadTo).NotTo(BeADirectory())
			}
		},
		Entry("a missing SBOM", rcTag, func(r *verifyRelease, _ *fakeSystem) {
			r.assets = slices.DeleteFunc(r.assets, func(a string) bool { return strings.HasSuffix(a, "linux_amd64.tar.gz.sbom.json") })
		}, "asset_missing"),
		Entry("a prerelease flag that disagrees with the tag", rcTag, func(r *verifyRelease, _ *fakeSystem) { r.prerelease = false }, "prerelease_mismatch"),
		Entry("a body that is not the tag's message", rcTag, func(r *verifyRelease, _ *fakeSystem) { r.body = "other notes" }, "release_body_mismatch"),
		Entry("an archive whose checksum does not match", rcTag, func(r *verifyRelease, _ *fakeSystem) {
			r.checksums[archiveName(rcTag, "darwin", "arm64")] = sum([]byte("other"))
		}, "checksum_mismatch"),
		Entry("a binary that reports another version", rcTag, func(r *verifyRelease, _ *fakeSystem) {
			r.binary = "coroner version v0.0.1"
			r.checksums[archiveName(rcTag, "darwin", "arm64")] = sum(tarGz("coroner", r.binary))
		}, "version_mismatch"),
		Entry("a cask without the version", finalTag, func(r *verifyRelease, f *fakeSystem) {
			f.answers[caskCmd] = strings.ReplaceAll(r.cask, "0.1.0", "0.0.9")
		}, "cask_mismatch"),
		Entry("a platform outside the release targets", rcTag, func(_ *verifyRelease, f *fakeSystem) { f.goos = "windows" }, "unsupported_platform"),
	)
})
