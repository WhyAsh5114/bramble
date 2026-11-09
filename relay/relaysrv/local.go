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
	go func() { _ = srv.Serve(ln) }()
	return &Local{Addr: ln.Addr().String(), Pin: pin, Data: data, ln: ln, dir: dir}, nil
}

// Close stops the listener and every data session, like a relay that died.
func (l *Local) Close() {
	l.ln.Close()
	l.Data.CloseAll()
	os.RemoveAll(l.dir)
}
