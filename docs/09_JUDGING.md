# Judging

## Objections, with answers

**1. "Headscale is free and takes ten minutes. Why are you here?"**
Headscale is the server this removes. It is also, per current comparisons, without Tailnet Lock — so self-hosters have a weaker admission story than Tailscale customers. This gives you Tailscale's admission guarantee with no server to keep alive.

**2. "You put device endpoints on-chain? You published my network topology."**
No. Only public keys and authorization. Endpoints, candidates, and traffic never touch the chain. (Gate 1.4 enforces this in CI.)

**3. "Endpoints change constantly. You are writing to a chain on every network change?"**
No. Authorization is low-churn and on-chain. Discovery is high-churn and off-chain through relays that cannot admit anyone.

**4. "Your revocation is slower than Tailscale's."**
Yes. Tailscale pushes over a live connection; this requires peers to observe chain state. Measured propagation is [N] seconds and it is in the README. The trade is that Tailscale's revocation can be withheld by whoever runs the coordinator, and this one cannot.

**5. "Passkeys plus a pinned admin key does all of this without a chain."**
Largely true, and that is Tailnet Lock. What remains: bootstrap (a new device must learn the admin key from somewhere the coordinator does not control), non-equivocation (a coordinator can show different key lists to different nodes), and unwithholdable revocation. A Certificate-Transparency-style log would also work; the advantage here is not operating it, and ENS doubling as the bootstrap pointer.

**6. "Who actually buys this?"**
Small and mid-size distributed teams and self-hosters past the free tier who do not want to run Headscale, plus agent hosts needing scoped private access. Not enterprises — they buy SSO, audit, and support, which this does not provide.

**7. "Is this cheaper at scale?"**
For small and mid teams, yes, versus seats and versus operating a server. Not for enterprises, and that claim is not made.

**8. "More relays means a network effect, right?"**
No. It improves geographic latency, blocking resistance, and redundancy, with diminishing returns. It is a two-sided market with a cold-start problem, which is a disadvantage at launch, not an advantage.

**9. "How many relays are actually running?"**
The mechanism is real — different prices, client selection, failover on kill — and the market is the design, not a claim about scale today.

**10. "This isn't decentralized, there are still relays."**
Correct, and the claim is narrower: no *trusted* coordinator. Relays see only encrypted WireGuard packets. They can drop or delay; they cannot read or impersonate. That is exactly why they can be permissionless when a coordinator could not be.

**11. "Why ENS instead of DNS TXT records?"**
You could put keys in DNS. ENS adds programmable delegated permissions (EAC), revocation each peer verifies independently, and wallet-native signing. It is DNS with permissions, which is ENS's own framing.

**12. "Why two chains?"**
Identity and settlement are separate concerns. Authorization lives where naming lives; metered payment lives where cheap fast settlement lives. They do not need to be the same chain and are not pretending to be one system.

**13. "ENSIP-26 defines an agent endpoint record — why don't your agents use it?"**
Mesh-member device and agent subnames don't, because their whole security model depends on not being publicly reachable — that's exactly what admission control gates. Relay subnames do: a relay is a permissionless public service by design, and its ENSIP-26 endpoint record is literally the agent-discoverable directory the Hedera track asks for. Same standard, applied only to the entity that's actually meant to be found.

**14. "Isn't the rendezvous relay just a renamed coordinator?"**
No — the difference is what it's trusted for. A compromised coordinator (Tailscale/Headscale-style) can *inject* a device, because the coordinator is the source of truth for who's authorized; every other peer just believes it. A rogue rendezvous relay cannot do that: admission is still checked by each peer against ENS, independently of anything the relay says. What a rogue rendezvous relay *can* do is lie about or withhold an address — for example, hand Alice an attacker's IP instead of Bob's. That doesn't work either: Alice already has Bob's real public key from ENS, and WireGuard's handshake only completes if whoever answers can prove they hold the matching *private* key. The attacker doesn't, so the handshake just fails — a doomed connection attempt, not a compromised one. The one real risk left is availability, not integrity: a malicious relay operator could selectively drop messages for one targeted pair, indistinguishable from ordinary packet loss. That's why the design (`adr/0003-rendezvous-relay-split.md`) requires a small *set* of rendezvous relays per tailnet with failover across them, not a single one — so one rogue or down relay can't unilaterally deny connectivity.
