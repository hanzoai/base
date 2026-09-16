package main

import (
	"fmt"
	"github.com/hanzoai/plugin/wasmvm"
)

func main() { fmt.Println(wasmvm.NewRuntime().Name()) }
