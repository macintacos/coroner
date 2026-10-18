package viewer_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const updateFlag = "update"

func TestMain(m *testing.M) {
	// Registered here and read back with flag.Lookup: gochecknoglobals rejects a package-level flag var.
	flag.Bool(updateFlag, false, "rewrite the golden files in testdata")
	os.Exit(m.Run())
}

func TestViewer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Viewer Suite")
}

// matchGolden compares got with testdata/<name>.golden, rewriting the file instead under -update.
func matchGolden(name, got string) {
	GinkgoHelper()
	path := filepath.Join("testdata", name+".golden")
	if flag.Lookup(updateFlag).Value.String() == "true" {
		Expect(os.MkdirAll("testdata", 0o750)).To(Succeed())
		Expect(os.WriteFile(path, []byte(got), 0o600)).To(Succeed())
	}

	want, err := os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred(), "run go test ./internal/viewer -update to write it")
	Expect(got).To(Equal(string(want)))
}
