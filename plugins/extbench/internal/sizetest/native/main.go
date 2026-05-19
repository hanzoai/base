package main
import (
	"fmt"
	"github.com/hanzoai/base/plugins/extruntime"
)
func main(){fmt.Println(extruntime.NewNative().Name())}
