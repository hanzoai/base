package osutils_test

import (
	"testing"

	"github.com/hanzoai/base/tools/osutils"
)

func TestBool(t *testing.T) {
	const name = "BASE_TEST_FLAG"
	for _, tc := range []struct {
		raw  string
		set  bool
		def  bool
		want bool
	}{
		// The spellings an operator actually writes, all honoured.
		{"1", true, false, true},
		{"true", true, false, true},
		{"TRUE", true, false, true},
		{"True", true, false, true},
		{"t", true, false, true},
		{"0", true, true, false},
		{"false", true, true, false},
		{"FALSE", true, true, false},
		{"f", true, true, false},
		// Surrounding space is a copy-paste artefact, not an opinion.
		{"  true  ", true, false, true},
		{" 0 ", true, true, false},
		// Nothing said: the default stands.
		{"", true, true, true},
		{"", true, false, false},
		{"", false, true, true},
		{"", false, false, false},
		// Nothing meaningful said: still the default, never its opposite.
		{"yes", true, false, false},
		{"on", true, false, false},
		{"maybe", true, true, true},
		{"2", true, false, false},
	} {
		label := tc.raw
		if !tc.set {
			label = "<unset>"
		}
		t.Run(label, func(t *testing.T) {
			if tc.set {
				t.Setenv(name, tc.raw)
			} else {
				t.Setenv(name, "")
			}
			if got := osutils.Bool(name, tc.def); got != tc.want {
				t.Fatalf("Bool(%s=%q, def=%v) = %v, want %v", name, tc.raw, tc.def, got, tc.want)
			}
		})
	}
}
