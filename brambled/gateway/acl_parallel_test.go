package gateway

import (
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

type gatedResolver struct {
	started chan string
	release chan struct{}
}

func (r *gatedResolver) ResolveDevice(label string) (*sidecar.DeviceRecord, error) {
	r.started <- label
	<-r.release
	return &sidecar.DeviceRecord{}, nil
}

func TestCheckACLResolvesGatewayAndRequesterConcurrently(t *testing.T) {
	t.Setenv("BRAMBLE_FAST_RESOLVE", "1")
	r := &gatedResolver{started: make(chan string, 2), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		CheckACL(r, "", "gateway", "requester", "task")
	}()
	defer func() { close(r.release); <-done }()
	seen := map[string]bool{}
	for range 2 {
		select {
		case label := <-r.started:
			seen[label] = true
		case <-time.After(time.Second):
			t.Fatal("independent records did not start before either read completed")
		}
	}
	if !seen["gateway"] || !seen["requester"] {
		t.Fatalf("resolved labels: %v", seen)
	}
}
