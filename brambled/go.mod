module github.com/WhyAsh5114/bramble/brambled

go 1.27.0

toolchain go1.27.3

require (
	github.com/WhyAsh5114/bramble/relay v0.0.0
	golang.org/x/crypto v0.57.0
	golang.org/x/net v0.60.0
	golang.org/x/time v0.7.0
	golang.zx2c4.com/wireguard v0.0.0-20260522210424-ecfc5a8d5446
)

require (
	github.com/google/btree v1.1.2 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	gvisor.dev/gvisor v0.0.0-20250503011706-39ed1f5ac29c // indirect
)

replace github.com/WhyAsh5114/bramble/relay => ../relay
