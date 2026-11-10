package importcmd_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestImportcmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Importcmd Suite")
}
