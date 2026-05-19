package main
import (
	"fmt"
	"github.com/hanzoai/base/plugins/gojavm"
)
func main(){fmt.Println(gojavm.NewRuntime().Name())}
