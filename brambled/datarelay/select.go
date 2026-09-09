// Package datarelay picks which data-plane relay to buy a session from
// when a peer is configured to use one (docs/adr/0008, docs/05_BUILD_PLAN.md
// Gate 4.3's "two relays, client chooses"). Deliberately just this one pure
// function — gathering quotes (an HTTP GET to each candidate's relay-sidecar
// /price route, timing the round trip) is brambled/main.go's job, kept out
// of this package so the selection formula itself is unit-testable without
// real network calls.
package datarelay

// Quote is one data relay's advertised price (atomic USDC per byte, from
// GET /price) and measured round-trip latency in milliseconds.
type Quote struct {
	Label        string // the -data-relay flag's label, for logging
	SidecarURL   string
	PricePerByte int64
	LatencyMS    int64
}

// LatencyWeightAtomicPerMS converts one millisecond of round-trip latency
// into the same atomic-USDC-per-byte units as PricePerByte, so the two
// numbers can be summed into a single composite score. A flat weight of 1
// is a deliberately simple, documented choice, not a tuned one — see
// docs/adr/0008's "Relay selection" section.
const LatencyWeightAtomicPerMS = 1

// Pick returns the candidate with the lowest price+latency composite score,
// preferring the earliest entry in candidates on an exact tie. Panics on an
// empty slice — callers should not invoke relay selection with no
// -data-relay candidates configured for the peer in question.
func Pick(candidates []Quote) Quote {
	if len(candidates) == 0 {
		panic("datarelay.Pick: no candidates")
	}
	best := candidates[0]
	bestScore := score(best)
	for _, c := range candidates[1:] {
		if s := score(c); s < bestScore {
			best, bestScore = c, s
		}
	}
	return best
}

func score(q Quote) int64 {
	return q.PricePerByte + LatencyWeightAtomicPerMS*q.LatencyMS
}
