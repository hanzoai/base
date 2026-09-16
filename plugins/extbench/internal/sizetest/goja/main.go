package main

import (
	"fmt"
	"github.com/hanzoai/plugin/gojavm"
)

func main() { fmt.Println(gojavm.NewRuntime().Name()) }
