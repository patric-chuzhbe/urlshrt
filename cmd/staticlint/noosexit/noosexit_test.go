package noosexit

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Test runs the noosexit Analyzer against test data using analysistest.
// This ensures that the analyzer reports the correct diagnostics.
func Test(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer)
}
