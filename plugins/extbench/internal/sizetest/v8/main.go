package main
import (
	"fmt"
	"github.com/hanzoai/base/plugins/v8vm"
)
func main(){fmt.Println(v8vm.NewRuntime().Name())}
