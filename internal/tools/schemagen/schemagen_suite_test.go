package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSchemagen(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Schemagen Suite")
}
