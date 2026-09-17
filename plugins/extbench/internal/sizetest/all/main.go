package main

import (
	"fmt"

	"github.com/hanzoai/base/plugins/v8vm"
	"github.com/hanzoai/plugin/extruntime"
	"github.com/hanzoai/plugin/gojavm"
	"github.com/hanzoai/plugin/wasmvm"
)

func main() {
	fmt.Println(extruntime.NewNative().Name(), gojavm.NewRuntime().Name(), wasmvm.NewRuntime().Name(), v8vm.NewRuntime().Name())
}
