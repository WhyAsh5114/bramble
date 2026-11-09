package brambled

import (
	"testing"
	"time"
)

func TestFailoverBackoff(t *testing.T) {
	want := map[int]time.Duration{0: 0, 1: 0, 2: 5 * time.Second, 3: 10 * time.Second, 4: 20 * time.Second, 7: 160 * time.Second, 8: 3 * time.Minute, 50: 3 * time.Minute}
	for failures, w := range want {
		if got := failoverBackoff(failures); got != w {
			t.Errorf("failoverBackoff(%d) = %s, want %s", failures, got, w)
		}
	}
}
