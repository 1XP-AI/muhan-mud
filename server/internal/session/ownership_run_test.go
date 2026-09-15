package session

import (
	"errors"
	"testing"
)

func TestOwnershipRunRejectsStaleCommandBeforeApply(t *testing.T) {
	var owners Ownership
	old, _ := owners.Acquire("a")
	owners.Release(old)
	current, _ := owners.Acquire("a")
	if err := owners.Run(old, func() error { t.Fatal("stale command applied"); return nil }); err == nil {
		t.Fatal("stale command accepted")
	}
	calls := 0
	if err := owners.Run(current, func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("%v %d", err, calls)
	}
}

func TestOwnershipRunSerializesReleaseWithCommand(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	entered, finish := make(chan struct{}), make(chan struct{})
	command := make(chan error, 1)
	go func() { command <- owners.Run(lease, func() error { close(entered); <-finish; return nil }) }()
	<-entered
	started, released := make(chan struct{}), make(chan bool, 1)
	go func() { close(started); released <- owners.Release(lease) }()
	<-started
	select {
	case <-released:
		t.Fatal("released during apply")
	default:
	}
	close(finish)
	if err := <-command; err != nil {
		t.Fatal(err)
	}
	if !<-released {
		t.Fatal("release failed after apply")
	}
}

func TestOwnershipRunFailureDoesNotReleaseReservation(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	failure := errors.New("commit uncertain")
	if err := owners.Run(lease, func() error { return failure }); !errors.Is(err, failure) || !owners.Owns(lease) {
		t.Fatal("lost reservation after error")
	}
	if err := owners.Run(lease, nil); err == nil {
		t.Fatal("nil apply accepted")
	}
}
