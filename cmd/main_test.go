package cmd

import (
	"os"
	"testing"
)

// TestMain lets git run this test binary as the enclaude CLI: with
// ENCLAUDE_TEST_AS_CLI=1 it executes the command line it was given instead of
// the tests, so tests can register it as a real merge driver.
func TestMain(m *testing.M) {
	if os.Getenv("ENCLAUDE_TEST_AS_CLI") == "1" {
		Execute()
		os.Exit(0)
	}
	os.Exit(m.Run())
}
