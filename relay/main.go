// Command relay is a free, permissionless Bramble relay: rendezvous for sealed
// candidate exchange, a capped data relay for peers that cannot hole-punch,
// and a STUN responder (ADR 0003, ADR 0019). The bramble binary embeds the
// same code as `bramble relay`.
package main

import (
	"os"

	"github.com/WhyAsh5114/bramble/relay/relaysrv"
)

var version = "dev"

func main() { os.Exit(relaysrv.Main(os.Args[1:], version)) }
