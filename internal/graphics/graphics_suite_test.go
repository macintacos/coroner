package graphics_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGraphics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Graphics Suite")
}
