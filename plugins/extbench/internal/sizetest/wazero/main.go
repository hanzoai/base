package main
import (
	"fmt"
	"github.com/hanzoai/base/plugins/wasmvm"
)
func main(){fmt.Println(wasmvm.NewRuntime().Name())}
