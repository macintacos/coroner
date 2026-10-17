package timeparse_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTimeparse(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Timeparse Suite")
}
