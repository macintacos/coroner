package clikit_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClikit(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Clikit Suite")
}
