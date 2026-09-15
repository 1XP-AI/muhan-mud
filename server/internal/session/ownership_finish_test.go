package session

import (
	"errors"
	"testing"
)

func TestOwnershipFinishRetainsClosingLeaseUntilCommit(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	failure := errors.New("commit unavailable")
	if err := owners.Finish(lease, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if !owners.Owns(lease) {
		t.Fatal("released uncertain departure")
	}
	if _, err := owners.Acquire("a"); err == nil {
		t.Fatal("admitted over closing session")
	}
	if err := owners.Run(lease, func() error { t.Fatal("command during closing"); return nil }); err == nil {
		t.Fatal("closing command accepted")
	}
	if err := owners.Finish(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	next, err := owners.Acquire("a")
	if err != nil || next.Generation == lease.Generation {
		t.Fatal("new session not admitted")
	}
	if err := owners.Finish(lease, func() error { t.Fatal("stale departure executed"); return nil }); err == nil || !owners.Owns(next) {
		t.Fatal("stale finish affected new owner")
	}
}

func TestOwnershipFinishInvalidCallbackDoesNotClose(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Finish(lease, nil); err == nil {
		t.Fatal("nil callback")
	}
	if err := owners.Run(lease, func() error { return nil }); err != nil {
		t.Fatal("nil callback closed lease")
	}
}
