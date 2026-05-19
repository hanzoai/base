package main
import (
	"fmt"
	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/plugins/gojavm"
	"github.com/hanzoai/base/plugins/wasmvm"
	"github.com/hanzoai/base/plugins/v8vm"
)
func main(){
	fmt.Println(extruntime.NewNative().Name(), gojavm.NewRuntime().Name(), wasmvm.NewRuntime().Name(), v8vm.NewRuntime().Name())
}
