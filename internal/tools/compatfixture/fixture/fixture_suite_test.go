package fixture_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFixture(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fixture Suite")
}
