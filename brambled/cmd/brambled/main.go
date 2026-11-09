// Command brambled runs the Bramble node daemon on its own. The bramble
// binary embeds the same code as `bramble node`.
package main

import (
	"os"

	"github.com/WhyAsh5114/bramble/brambled"
)

func main() { os.Exit(brambled.Main(os.Args[1:])) }
