package transport

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestIgnoreListPreservesLegacyNewestFirstInsertionOrder(t *testing.T) {
	list := NewIgnoreList()
	for _, name := range []string{"alice", "Bob", "한글"} {
		if changed, err := list.Add(name); err != nil || !changed {
			t.Fatalf("Add(%q) = changed=%v err=%v", name, changed, err)
		}
	}
	want := []string{"한글", "Bob", "Alice"}
	if got := list.List(); !equalIgnoreStrings(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	// List returns an owned snapshot, so a caller cannot mutate connection
	// state by changing the returned backing array.
	snapshot := list.List()
	snapshot[0] = "tampered"
	if got := list.List(); !equalIgnoreStrings(got, want) {
		t.Fatalf("List() exposed mutable state: %v", got)
	}
}

func TestIgnoreListToggleRemovesInPlaceAndAddsAtHead(t *testing.T) {
	list := NewIgnoreList()
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		if _, err := list.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := list.Toggle("bob")
	if err != nil {
		t.Fatal(err)
	}
	if removed.Name != "Bob" || !removed.Removed || removed.Added || !removed.Changed {
		t.Fatalf("removed result = %+v", removed)
	}
	if got, want := list.List(), []string{"Carol", "Alice"}; !equalIgnoreStrings(got, want) {
		t.Fatalf("after remove = %v, want %v", got, want)
	}

	added, err := list.Toggle("dave")
	if err != nil {
		t.Fatal(err)
	}
	if added.Name != "Dave" || !added.Added || added.Removed || !added.Changed {
		t.Fatalf("added result = %+v", added)
	}
	if got, want := list.List(), []string{"Dave", "Carol", "Alice"}; !equalIgnoreStrings(got, want) {
		t.Fatalf("after add = %v, want %v", got, want)
	}
}

func TestIgnoreListAddIsIdempotentAndRemoveReportsMissing(t *testing.T) {
	list := NewIgnoreList()
	changed, err := list.Add("Alice")
	if err != nil || !changed {
		t.Fatalf("first Add = changed=%v err=%v", changed, err)
	}
	changed, err = list.Add("alice")
	if err != nil || changed {
		t.Fatalf("duplicate Add = changed=%v err=%v", changed, err)
	}
	removed, err := list.Remove("nobody")
	if err != nil || removed {
		t.Fatalf("missing Remove = removed=%v err=%v", removed, err)
	}
	if !list.Contains("alice") || list.Contains("nobody") {
		t.Fatalf("Contains returned unexpected state: %v", list.List())
	}
}

func TestIgnoreListClearDropsConnectionLocalNames(t *testing.T) {
	list := NewIgnoreList()
	if _, err := list.Add("Alice"); err != nil {
		t.Fatal(err)
	}
	list.Clear()
	if got := list.List(); len(got) != 0 || list.Contains("Alice") {
		t.Fatalf("Clear left names behind: %v", got)
	}
}

func TestIgnoreListRejectsUnsafeNamesAndBoundsCapacity(t *testing.T) {
	list := NewIgnoreList()
	for _, name := range []string{
		"",
		" alice",
		"alice ",
		"alice bob",
		"alice\n",
		"alice\x00",
		"alice/alice",
		strings.Repeat("a", IgnoreNameMaxBytes+1),
	} {
		if changed, err := list.Add(name); changed || !errors.Is(err, ErrInvalidIgnoreName) {
			t.Fatalf("Add(%q) = changed=%v err=%v, want ErrInvalidIgnoreName", name, changed, err)
		}
	}
	for i := 0; i < MaxIgnoredPlayers; i++ {
		name := "p" + strings.Repeat("x", 8) + string(rune('a'+i%26)) + string(rune('0'+i/26))
		if _, err := list.Add(name); err != nil {
			t.Fatalf("Add #%d failed: %v", i, err)
		}
	}
	if changed, err := list.Add("overflow"); changed || !errors.Is(err, ErrIgnoreLimit) {
		t.Fatalf("overflow Add = changed=%v err=%v, want ErrIgnoreLimit", changed, err)
	}
	if got := len(list.List()); got != MaxIgnoredPlayers {
		t.Fatalf("len(List()) = %d, want %d", got, MaxIgnoredPlayers)
	}
}

func TestIgnoreListIsSafeForConcurrentConnectionUse(t *testing.T) {
	list := NewIgnoreList()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "Player" + string(rune('A'+i%8))
			for j := 0; j < 100; j++ {
				_, _ = list.Toggle(name)
				_ = list.Contains(name)
				_ = list.List()
			}
		}(i)
	}
	wg.Wait()
	for _, name := range list.List() {
		if err := ValidateIgnoreName(name); err != nil {
			t.Fatalf("concurrent mutation left invalid name %q: %v", name, err)
		}
	}
}

func equalIgnoreStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
