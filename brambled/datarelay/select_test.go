package datarelay

import "testing"

func TestPick_LowerPriceWins(t *testing.T) {
	cheap := Quote{Label: "cheap", PricePerByte: 1, LatencyMS: 0}
	expensive := Quote{Label: "expensive", PricePerByte: 100, LatencyMS: 0}

	got := Pick([]Quote{expensive, cheap})
	if got.Label != "cheap" {
		t.Fatalf("Pick() = %q, want %q", got.Label, "cheap")
	}
}

func TestPick_LatencyBreaksATie(t *testing.T) {
	slow := Quote{Label: "slow", PricePerByte: 10, LatencyMS: 200}
	fast := Quote{Label: "fast", PricePerByte: 10, LatencyMS: 5}

	got := Pick([]Quote{slow, fast})
	if got.Label != "fast" {
		t.Fatalf("Pick() = %q, want %q", got.Label, "fast")
	}
}

func TestPick_CompositeScoreCanFavorHigherPriceLowerLatency(t *testing.T) {
	cheapButSlow := Quote{Label: "cheap-slow", PricePerByte: 5, LatencyMS: 1000}
	pricierButFast := Quote{Label: "pricier-fast", PricePerByte: 50, LatencyMS: 1}

	got := Pick([]Quote{cheapButSlow, pricierButFast})
	if got.Label != "pricier-fast" {
		t.Fatalf("Pick() = %q, want %q (score %d vs %d)", got.Label, "pricier-fast",
			cheapButSlow.PricePerByte+cheapButSlow.LatencyMS, pricierButFast.PricePerByte+pricierButFast.LatencyMS)
	}
}

func TestPick_ExactTiePrefersEarlierEntry(t *testing.T) {
	a := Quote{Label: "a", PricePerByte: 10, LatencyMS: 10}
	b := Quote{Label: "b", PricePerByte: 10, LatencyMS: 10}

	got := Pick([]Quote{a, b})
	if got.Label != "a" {
		t.Fatalf("Pick() = %q, want %q on exact tie", got.Label, "a")
	}
}

func TestPick_PanicsOnEmptyCandidates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Pick to panic on empty candidates")
		}
	}()
	Pick(nil)
}
