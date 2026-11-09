package brambled

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/hederapay"
	"github.com/WhyAsh5114/bramble/brambled/rendezvous"
)

// DefaultPayAsset is testnet USDC, the one token a node pays in (ADR 0021).
const DefaultPayAsset = "0.0.429274"

const (
	bytesPerMiB = 1 << 20
	// Defaults in USDC's smallest unit (six decimals): half a dollar a
	// session and two dollars a day. Testnet tokens have no value; the caps
	// are there so a hostile relay cannot drain an account by asking again.
	defaultMaxSession = 500_000
	defaultMaxDay     = 2_000_000
	payTimeout        = 90 * time.Second
)

// payer is a session source that can pay a relay that charges. buyDataRelaySession
// asks it before it counts a paid relay as a candidate.
type payer interface {
	// CanPay says whether this node will pay a relay that advertises st now.
	CanPay(st *rendezvous.PaidStatus) error
}

// paidSessions gets sessions from free relays as before and, when a relay
// advertises a price, pays it over x402 within the node's spend caps.
type paidSessions struct {
	freeSessions
	buyer      *hederapay.Buyer
	asset      string
	maxSession int64
	maxDay     int64

	mu      sync.Mutex
	day     string
	spent   int64
	history []Payment // newest last, in memory only
	// receipt is called after each paid session. It is optional.
	receipt func(Payment)
}

// Payment is one paid session, kept in memory for the UI.
type Payment struct {
	Relay       string    `json:"relay"`
	Amount      int64     `json:"amount"`
	Asset       string    `json:"asset"`
	Bytes       int64     `json:"bytes"`
	Transaction string    `json:"transaction"`
	At          time.Time `json:"at"`
}

const maxPaymentHistory = 20

// PayStats is what the node has paid since it started.
type PayStats struct {
	Account    string    `json:"account"`
	Asset      string    `json:"asset"`
	SpentToday int64     `json:"spentToday"`
	MaxSession int64     `json:"maxSession"`
	MaxDay     int64     `json:"maxDay"`
	Recent     []Payment `json:"recent"`
}

func (p *paidSessions) stats(account string) PayStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remainingLocked() // rolls the day over
	return PayStats{Account: account, Asset: p.asset, SpentToday: p.spent, MaxSession: p.maxSession, MaxDay: p.maxDay,
		Recent: append([]Payment{}, p.history...)}
}

func (p *paidSessions) record(pay Payment) {
	p.mu.Lock()
	p.history = append(p.history, pay)
	if len(p.history) > maxPaymentHistory {
		p.history = p.history[len(p.history)-maxPaymentHistory:]
	}
	receipt := p.receipt
	p.mu.Unlock()
	if receipt != nil {
		receipt(pay)
	}
}

func newPaidSessions(s *hederapay.Signer, maxSession, maxDay int64) *paidSessions {
	if maxSession <= 0 {
		maxSession = defaultMaxSession
	}
	if maxDay <= 0 {
		maxDay = defaultMaxDay
	}
	return &paidSessions{
		buyer:      &hederapay.Buyer{Signer: s, MaxAmount: map[string]int64{DefaultPayAsset: maxSession}},
		asset:      DefaultPayAsset,
		maxSession: maxSession,
		maxDay:     maxDay,
	}
}

func (p *paidSessions) today() string { return time.Now().UTC().Format("2006-01-02") }

// remainingLocked is what the daily cap still allows. mu must be held.
func (p *paidSessions) remainingLocked() int64 {
	if p.day != p.today() {
		p.day, p.spent = p.today(), 0
	}
	return p.maxDay - p.spent
}

func (p *paidSessions) CanPay(st *rendezvous.PaidStatus) error {
	if st.Network != hederapay.NetworkTestnet {
		return fmt.Errorf("it charges on %s, and this node pays on %s only", st.Network, hederapay.NetworkTestnet)
	}
	if st.Asset != p.asset {
		return fmt.Errorf("it charges in %s, and this node pays in %s only", st.Asset, p.asset)
	}
	if st.PricePerMiB <= 0 {
		return errors.New("it advertises no usable price")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.remainingLocked() < st.PricePerMiB {
		return errors.New("the daily spend cap is reached")
	}
	return nil
}

// costOf is what a relay charges for bytes: rounded up, never zero.
func costOf(bytes, pricePerMiB int64) int64 {
	return max(1, (bytes*pricePerMiB+bytesPerMiB-1)/bytesPerMiB)
}

// sizeFor trims a session so its price fits the per-session cap and what is
// left of the daily cap: whole MiB, at least one. It returns the byte count
// and what the relay will charge for it (the relay rounds up, never to zero).
func sizeFor(bytes, pricePerMiB, maxSession, remaining int64) (int64, int64) {
	limit := min(maxSession, remaining)
	if costOf(bytes, pricePerMiB) > limit {
		bytes = (limit / pricePerMiB) * bytesPerMiB
	}
	if bytes < bytesPerMiB {
		return 0, 0
	}
	return bytes, costOf(bytes, pricePerMiB)
}

func (p *paidSessions) Session(r rendezvous.Relay, bytes int64) (*rendezvous.SessionGrant, error) {
	st, err := rendezvous.Status(r, dataRelayProbeTimeout)
	if err != nil {
		return nil, err
	}
	if st.Paid == nil {
		return p.freeSessions.Session(r, bytes)
	}
	if err := p.CanPay(st.Paid); err != nil {
		return nil, fmt.Errorf("not paying %s: %w", r.Label, err)
	}
	p.mu.Lock()
	n, cost := sizeFor(bytes, st.Paid.PricePerMiB, p.maxSession, p.remainingLocked())
	if n == 0 {
		p.mu.Unlock()
		return nil, fmt.Errorf("not paying %s: one MiB costs more than this node's spend caps allow", r.Label)
	}
	p.spent += cost // reserved; given back if the payment fails
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), payTimeout)
	defer cancel()
	purchase, err := p.buyer.BuySession(ctx, rendezvous.HTTPClient(r, payTimeout), "https://"+r.Address, n)
	if err != nil {
		p.refund(cost)
		return nil, err
	}
	// The relay may clamp the session below what was asked; count what it charged.
	actual := costOf(purchase.Bytes, st.Paid.PricePerMiB)
	p.refund(cost - actual)
	cost = actual
	tracef("paid %s %d of %s for %d bytes (tx %s)", r.Label, cost, p.asset, n, purchase.Transaction)
	p.record(Payment{Relay: r.Label, Amount: cost, Asset: p.asset, Bytes: purchase.Bytes, Transaction: purchase.Transaction, At: time.Now().UTC()})
	grant, err := rendezvous.RequestSessionWithToken(r, purchase.Bytes, purchase.Token, dataRelayRequestTimeout)
	if err != nil {
		// The payment went through, so keep it counted: it cannot be given back.
		return nil, fmt.Errorf("paid %s but the session failed: %w", r.Label, err)
	}
	return grant, nil
}

// refund gives back part of a reservation, or takes more when n is negative.
func (p *paidSessions) refund(n int64) {
	p.mu.Lock()
	p.spent = max(0, p.spent-n)
	p.mu.Unlock()
}
