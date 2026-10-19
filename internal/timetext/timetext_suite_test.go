package timetext_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTimetext(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Timetext Suite")
}
