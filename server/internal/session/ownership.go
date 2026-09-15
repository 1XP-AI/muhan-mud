package session

import (
	"fmt"
	"sort"
	"sync"
)

// SessionLease is process-local, never a client credential or database fence.
type SessionLease struct {
	ActorID    string
	Generation uint64
}

// Ownership reserves a single connection per actor, including pending world
// admission. Keep the reservation until departure is committed. Admission
// failure may release its own lease. Runtime commands must be serialized with
// ownership checks; Owns followed by an asynchronous write is not sufficient.
// This is NOT cross-process ownership: startup fencing remains required.
type Ownership struct {
	mu       sync.Mutex
	next     uint64
	owners   map[string]uint64
	closing  map[string]bool
	admitted map[string]bool
}

func (o *Ownership) Acquire(actorID string) (SessionLease, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if actorID == "" || o.owners[actorID] != 0 {
		return SessionLease{}, fmt.Errorf("actor already reserved or invalid")
	}
	if o.next == ^uint64(0) {
		return SessionLease{}, fmt.Errorf("session generation exhausted")
	}
	if o.owners == nil {
		o.owners = map[string]uint64{}
	}
	o.next++
	o.owners[actorID] = o.next
	return SessionLease{ActorID: actorID, Generation: o.next}, nil
}

func (o *Ownership) Owns(lease SessionLease) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return lease.Generation != 0 && o.owners[lease.ActorID] == lease.Generation
}

// Run holds the process-local coordinator lock across validation and durable
// command application, including receipt recovery. apply must be bounded by its
// own context and must not call any Ownership method (the lock is non-reentrant).
// Publish outside this lock only to the captured connection, not a later owner.
// Errors retain the reservation: an uncertain commit must be resolved before
// another connection is admitted. This does not provide database writer fencing.
func (o *Ownership) Run(lease SessionLease, apply func() error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if lease.Generation == 0 || o.owners[lease.ActorID] != lease.Generation {
		return fmt.Errorf("stale session command")
	}
	if o.closing[lease.ActorID] {
		return fmt.Errorf("session departure pending")
	}
	if apply == nil {
		return fmt.Errorf("missing session command")
	}
	return apply()
}

// Admit marks ready only after durable admission/receipt recovery succeeds.
func (o *Ownership) Admit(lease SessionLease, commit func() error) error {
	if commit == nil {
		return fmt.Errorf("missing admission commit")
	}
	return o.Run(lease, func() error {
		if err := commit(); err != nil {
			return err
		}
		if o.admitted == nil {
			o.admitted = map[string]bool{}
		}
		o.admitted[lease.ActorID] = true
		return nil
	})
}

// RunGame is the gameplay entry point; Run alone only validates reservation.
func (o *Ownership) RunGame(lease SessionLease, apply func() error) error {
	if apply == nil {
		return fmt.Errorf("missing game command")
	}
	return o.Run(lease, func() error {
		if !o.admitted[lease.ActorID] {
			return fmt.Errorf("world admission pending")
		}
		return apply()
	})
}

// Finish commits departure (or resolves its receipt) before freeing the actor.
// Failed/uncertain commits stay closing: no new commands or acquisition until a
// successful retry. The callback must not call Ownership methods. Process death
// still requires durable startup recovery; this state is deliberately local.
func (o *Ownership) Finish(lease SessionLease, commit func() error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if lease.Generation == 0 || o.owners[lease.ActorID] != lease.Generation {
		return fmt.Errorf("stale session departure")
	}
	if commit == nil {
		return fmt.Errorf("missing departure commit")
	}
	if o.closing == nil {
		o.closing = map[string]bool{}
	}
	o.closing[lease.ActorID] = true
	if err := commit(); err != nil {
		return err
	}
	delete(o.owners, lease.ActorID)
	delete(o.closing, lease.ActorID)
	delete(o.admitted, lease.ActorID)
	return nil
}

func (o *Ownership) Release(lease SessionLease) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if lease.Generation == 0 || o.owners[lease.ActorID] != lease.Generation || o.closing[lease.ActorID] {
		return false
	}
	delete(o.owners, lease.ActorID)
	delete(o.admitted, lease.ActorID)
	return true
}

// Seal stops admission and gameplay immediately when cleanup is queued, without
// releasing the reservation before its durable outcome is known.
func (o *Ownership) Seal(lease SessionLease) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if lease.Generation == 0 || o.owners[lease.ActorID] != lease.Generation {
		return fmt.Errorf("stale session closure")
	}
	if o.closing == nil {
		o.closing = map[string]bool{}
	}
	o.closing[lease.ActorID] = true
	return nil
}

// Ordered returns independent values in acceptance order. Descriptor reuse in
// C is not reproduced here; runtime ordering policy must explicitly adopt this
// order or maintain a descriptor-compatible order when strict parity is needed.
func (o *Ownership) Ordered() []SessionLease {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]SessionLease, 0, len(o.owners))
	for id, generation := range o.owners {
		out = append(out, SessionLease{ActorID: id, Generation: generation})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Generation < out[j].Generation })
	return out
}
