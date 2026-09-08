package session

import (
	"sync"
	"testing"
)

func TestOwnershipStaleReleaseCannotRemoveNewLogin(t *testing.T) {
	var owners Ownership
	first, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owners.Acquire("a"); err == nil {
		t.Fatal("duplicate owner")
	}
	if !owners.Release(first) {
		t.Fatal("release failed")
	}
	second, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || owners.Release(first) || owners.Owns(first) || !owners.Owns(second) {
		t.Fatal("stale lease affected new login")
	}
}

func TestOwnershipConcurrentAcquireHasOneWinner(t *testing.T) {
	var owners Ownership
	leases := make(chan SessionLease, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lease, err := owners.Acquire("a"); err == nil {
				leases <- lease
			}
		}()
	}
	wg.Wait()
	close(leases)
	count := 0
	for lease := range leases {
		count++
		if !owners.Owns(lease) {
			t.Fatal("winner not owner")
		}
	}
	if count != 1 {
		t.Fatalf("winners%d", count)
	}
}

func TestOwnershipOrderIsAcquisitionOrder(t *testing.T) {
	var owners Ownership
	b, _ := owners.Acquire("b")
	a, _ := owners.Acquire("a")
	order := owners.Ordered()
	if len(order) != 2 || order[0] != b || order[1] != a {
		t.Fatal("order lost")
	}
	order[0] = SessionLease{}
	if !owners.Owns(b) || owners.Release(SessionLease{}) {
		t.Fatal("invalid lease accepted")
	}
	if _, err := owners.Acquire(""); err == nil {
		t.Fatal("empty actor")
	}
}
