package main

import (
	"fmt"
	"github.com/hanzoai/plugin/extruntime"
)

func main() { fmt.Println(extruntime.NewNative().Name()) }
