package gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/sidecar"
)

type gatedResolver struct {
	started chan string
	release chan struct{}
}

type slowGranterResolver struct {
	fakeResolver
	started chan struct{}
	release chan struct{}
}

func (r slowGranterResolver) ResolveDevice(label string) (*sidecar.DeviceRecord, error) {
	if label == "stalled" {
		close(r.started)
		<-r.release
	}
	return r.fakeResolver.ResolveDevice(label)
}

func TestCheckACLMatchingGrantDoesNotWaitForUnrelatedGranter(t *testing.T) {
	t.Setenv("BRAMBLE_FAST_RESOLVE", "1")
	granterPriv := strings.Repeat("aa", 32)
	gatewayPriv := strings.Repeat("bb", 32)
	requesterPub := mustPub(t, device1Priv)
	digest := mustDigest(t, granterPriv, mustPub(t, gatewayPriv), requesterPub, "task")
	r := slowGranterResolver{
		fakeResolver: fakeResolver{
			"gateway":   {ACLGranters: []string{"valid", "stalled"}},
			"valid":     {Pubkey: strPtr(mustPub(t, granterPriv))},
			"requester": {ACL: []string{digest}, Pubkey: strPtr(requesterPub)},
		},
		started: make(chan struct{}), release: make(chan struct{}),
	}
	done := make(chan struct{})
	allowed := make(chan bool, 1)
	go func() {
		defer close(done)
		ok, err := CheckACL(r, gatewayPriv, "gateway", "requester", "task")
		allowed <- ok && err == nil
	}()
	defer func() { close(r.release); <-done }()
	<-r.started
	select {
	case ok := <-allowed:
		if !ok {
			t.Fatal("valid grant was not accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("a stalled unrelated granter delayed a matching grant")
	}
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
