package internalcmd_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInternalcmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Internalcmd Suite")
}
