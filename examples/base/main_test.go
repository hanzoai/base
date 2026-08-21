package main

import (
	"testing"

	"github.com/hanzoai/base/plugins/extruntime"
)

// The binary this file builds must be able to run the functions it serves.
//
// `apis.bindFunctionsApi` mounts /v1/functions unconditionally, and the invoke
// reaches for a runtime by language at call time — so a deployment that links
// none still lists, stores and serves functions, and refuses only when somebody
// calls one. The functions tests link the runtime themselves, which is what let
// this binary ship without it: every test passed while every call answered "no
// runtime is linked".
//
// Asking the question here, where the binary is assembled, is what makes an
// import removed upstairs a failure rather than a surprise in production.
func TestBinaryRunsFunctions(t *testing.T) {
	// The language apis.functionLang names, spelled out because the constant is
	// unexported and this is the contract between the two packages.
	if extruntime.Lookup("js") == nil {
		t.Fatal("no runtime is linked for js: /v1/functions accepts and stores " +
			"functions this binary cannot run")
	}
}
