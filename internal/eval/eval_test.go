package eval

import (
	"testing"
)

// TestStandardCases is the quality gate: every fixture must keep its facts
// and hit its savings floor. Run -v to see the was→is table.
func TestStandardCases(t *testing.T) {
	report, ok := Report(Run(StandardCases()))
	t.Log("\n" + report)
	if !ok {
		t.Fatal("quality fixtures violated contracts")
	}
}
