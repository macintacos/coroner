package config

import (
	"os"
	"path/filepath"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the configuration reference", func() {
	It("names every key the config file accepts", func() {
		page, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "configuration.md"))
		Expect(err).NotTo(HaveOccurred())

		for _, field := range reflect.VisibleFields(reflect.TypeFor[fileConfig]()) {
			key := field.Tag.Get("toml")
			Expect(string(page)).To(ContainSubstring("`"+key+"`"), key)
		}
	})
})
