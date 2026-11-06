# Resolver provider visibility

Before the optimization, the RPC provider receives seven separate `eth_call` requests per device over the `ens` Tor isolation key. It sees a Tor exit IP, time, the Universal Resolver and registry targets, encoded device names, requested fields and public return values. A three-record decision issues up to 21 calls.

With Multicall3, the provider receives one `eth_call` per device, or three per three-record decision. It sees the same public information inside one aggregate payload and can directly associate the subcalls in that payload. A warm mesh IP cache omits that field. The fallback issues the individual calls concurrently. Tor still uses the `ens` key for these calls and the separate `pay` key for payments. CCIP-Read stays disabled. No new direct connection, payment change or persistent node state is introduced.

