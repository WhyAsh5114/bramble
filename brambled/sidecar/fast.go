package sidecar

import "os"

// FastResolveEnabled keeps resolver optimizations opt-in on each node.
func FastResolveEnabled() bool { return os.Getenv("BRAMBLE_FAST_RESOLVE") == "1" }
