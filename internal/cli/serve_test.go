package cli

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitIdle(t *testing.T, s *scanner) {
	t.Helper()
	for i := 0; i < 200; i++ {
		s.mu.Lock()
		idle := s.running.IsZero()
		s.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("scan did not finish")
}

func TestScannerKeepsLastGoodResult(t *testing.T) {
	results := []error{nil, errors.New("boom")}
	calls := 0
	s := &scanner{timeout: time.Second, run: func(context.Context) ([]byte, error) {
		err := results[calls]
		calls++
		return []byte("cephilis_dir_bytes 1\n"), err
	}}
	s.tick()
	waitIdle(t, s)
	s.tick()
	waitIdle(t, s)

	var b strings.Builder
	s.write(&b)
	out := b.String()
	for _, want := range []string{"cephilis_dir_bytes 1", "cephilis_serve_scan_failures_total 1", "cephilis_serve_scan_running_seconds 0.000"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "cephilis_serve_last_success_timestamp_seconds 0\n") {
		t.Error("last success not recorded")
	}
}

// A scan stuck in D state never returns; no second scan may start, and the
// stuck time must be visible.
func TestScannerDoesNotPileUpStuckScans(t *testing.T) {
	var mu sync.Mutex
	started := 0
	block := make(chan struct{})
	s := &scanner{timeout: time.Hour, run: func(context.Context) ([]byte, error) {
		mu.Lock()
		started++
		mu.Unlock()
		<-block
		return nil, nil
	}}
	defer close(block)
	for i := 0; i < 5; i++ {
		s.tick()
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	n := started
	mu.Unlock()
	if n != 1 {
		t.Errorf("%d scans started while one was stuck, want 1", n)
	}
	var b strings.Builder
	s.write(&b)
	if strings.Contains(b.String(), "cephilis_serve_scan_running_seconds 0.000\n") {
		t.Error("running time not reported for the stuck scan")
	}
}
