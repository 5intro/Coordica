package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"coordica/internal/domain"
)

func TestWatchdogStopsBeforeLeaseAndRevocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		respond(w, domain.HeartbeatResponse{Cancel: true, Reason: "paused"})
	}))
	defer server.Close()
	r := testRunner(t, server)
	for _, leaseDuration := range []time.Duration{150 * time.Millisecond, 5 * time.Second} {
		ctx, stop := context.WithCancel(context.Background())
		now := time.Now()
		a := domain.Assignment{Run: domain.Run{ID: "run_test", Generation: 7}, HeartbeatSeconds: 1}
		end, _ := r.watchPermission(ctx, stop, a, now, now.Add(leaseDuration), now.Add(time.Minute), now)
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("local permission watchdog did not stop")
		}
		end()
		stop()
	}
}

func TestDeliveryUsesLatestConfirmedLeaseButFreezesItsWindow(t *testing.T) {
	now := time.Now()
	window := &permissionWindow{lease: now.Add(time.Second), deadline: now.Add(time.Minute), lastSuccess: now}
	initial := window.until()
	window.renew(now.Add(10*time.Second), now.Add(time.Second))
	frozen := window.until()
	if !frozen.After(initial) {
		t.Fatal("delivery reused the original pre-execution lease")
	}
	window.renew(now.Add(20*time.Second), now.Add(2*time.Second))
	if frozen != now.Add(10*time.Second) || !window.until().After(frozen) {
		t.Fatal("renewal changed the frozen value or lost the live lease")
	}
	window.deadline = now.Add(5 * time.Second)
	if window.until() != window.deadline {
		t.Fatal("lease escaped the absolute Run deadline")
	}
}

func TestWatchdogExposesActualRenewedPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		respond(w, domain.HeartbeatResponse{LeaseUntil: time.Now().Add(10 * time.Second)})
	}))
	defer server.Close()
	r := testRunner(t, server)
	r.OnEvent = func(Event) {}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	now := time.Now()
	initial := now.Add(2 * time.Second)
	a := domain.Assignment{Run: domain.Run{ID: "run_test", Generation: 7}, HeartbeatSeconds: 1}
	end, until := r.watchPermission(ctx, stop, a, now, initial, now.Add(time.Minute), now)
	defer end()
	wait := time.NewTimer(1800 * time.Millisecond)
	defer wait.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if until().After(initial) {
				return
			}
		case <-wait.C:
			t.Fatal("successful heartbeat did not update the delivery permission window")
		case <-ctx.Done():
			t.Fatal("permission cancelled before successful renewal")
		}
	}
}
