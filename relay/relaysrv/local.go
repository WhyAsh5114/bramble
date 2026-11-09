package relaysrv

import (
	"crypto/tls"
	"os"
)

// Local is a relay on 127.0.0.1 with a throwaway key, for other packages' tests.
type Local struct {
	Addr string
	Pin  string
	Data *DataRelay

	ln  interface{ Close() error }
	dir string
}

// StartLocal starts a relay on loopback. Data sessions bind to ports in limits.
func StartLocal(limits Limits, budget *Budget) (*Local, error) {
	return startLocal(limits, budget, nil)
}

// StartLocalPaid starts a relay on loopback in paid mode (ADR 0021). The
// session cap in pay is set from limits when it is zero.
func StartLocalPaid(limits Limits, budget *Budget, pay PaywallConfig) (*Local, error) {
	return startLocal(limits, budget, &pay)
}

func startLocal(limits Limits, budget *Budget, pay *PaywallConfig) (*Local, error) {
	dir, err := os.MkdirTemp("", "bramble-relay-*")
	if err != nil {
		return nil, err
	}
	key, err := LoadOrCreateKey(dir)
	if err != nil {
		return nil, err
	}
	pin, err := PinOf(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	cfg, err := TLSConfig(key)
	if err != nil {
		return nil, err
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		return nil, err
	}
	if budget == nil {
		budget = NewBudget("", 0, 0)
	}
	data := NewDataRelay("127.0.0.1", limits, budget)
	srv := NewServer(data, "127.0.0.1", "test")
	if pay != nil {
		if pay.MaxSessionBytes == 0 {
			pay.MaxSessionBytes = limits.SessionBytes
		}
		pw, err := NewPaywall(*pay, dir)
		if err != nil {
			return nil, err
		}
		srv.SetPaywall(pw)
	}
	go func() { _ = srv.Serve(ln) }()
	return &Local{Addr: ln.Addr().String(), Pin: pin, Data: data, ln: ln, dir: dir}, nil
}

// Close stops the listener and every data session, like a relay that died.
func (l *Local) Close() {
	l.ln.Close()
	l.Data.CloseAll()
	os.RemoveAll(l.dir)
}
