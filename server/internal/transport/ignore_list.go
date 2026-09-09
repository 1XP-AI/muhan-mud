package transport

import (
	"errors"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	// The C server has PMAX player descriptors. An ignore entry can only be
	// created for an online player, so this cap bounds a connection-local list
	// without making it larger than the source's simultaneous-player universe.
	MaxIgnoredPlayers  = 256
	IgnoreNameMaxBytes = 14
	IgnoreNameMaxRunes = 12
)

var (
	ErrInvalidIgnoreName = errors.New("invalid ignore name")
	ErrIgnoreLimit       = errors.New("ignore list limit reached")
)

// IgnoreList is the descriptor-owned equivalent of command9.c's
// extr->first_ignore linked list. It is intentionally not part of world
// state, receipts, or PostgreSQL. The zero value is ready for use; methods
// take their own lock, so a connector may call them while holding unrelated
// world locks without sharing its mutex contract.
type IgnoreList struct {
	mu    sync.RWMutex
	names []string // newest first, matching C's head insertion
}

type IgnoreToggleResult struct {
	Name    string
	Added   bool
	Removed bool
	Changed bool
}

func NewIgnoreList() *IgnoreList {
	return &IgnoreList{}
}

// Clear releases every connection-local name at disconnect. It is safe to
// call while another descriptor is checking the list because the internal
// mutex, rather than the owning connection mutex, guards the slice.
func (l *IgnoreList) Clear() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.names {
		l.names[i] = ""
	}
	l.names = nil
}

// List returns a stable, owned snapshot in newest-first insertion order.
func (l *IgnoreList) List() []string {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]string(nil), l.names...)
}

// Contains performs the same first-ASCII-byte normalization as C's up()
// before looking up a name. Invalid selectors simply do not match; callers
// that need to report input errors should call ValidateIgnoreName first.
func (l *IgnoreList) Contains(name string) bool {
	canonical, err := CanonicalIgnoreName(name)
	if err != nil || l == nil {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.indexLocked(canonical) >= 0
}

// Add inserts a name at the head. A duplicate is a successful no-op, which
// makes retrying an already completed add safe and leaves insertion order
// unchanged. Target online/visibility authority belongs to the caller.
func (l *IgnoreList) Add(name string) (bool, error) {
	canonical, err := CanonicalIgnoreName(name)
	if err != nil {
		return false, err
	}
	if l == nil {
		return false, errors.New("nil ignore list")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.indexLocked(canonical) >= 0 {
		return false, nil
	}
	if len(l.names) >= MaxIgnoredPlayers {
		return false, ErrIgnoreLimit
	}
	l.names = append(l.names, "")
	copy(l.names[1:], l.names[:len(l.names)-1])
	l.names[0] = canonical
	return true, nil
}

// Remove deletes a name without reordering the other entries. Missing names
// are a successful no-op, matching command9.c's search-then-add toggle path
// when Remove is used independently by a connector.
func (l *IgnoreList) Remove(name string) (bool, error) {
	canonical, err := CanonicalIgnoreName(name)
	if err != nil {
		return false, err
	}
	if l == nil {
		return false, errors.New("nil ignore list")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	index := l.indexLocked(canonical)
	if index < 0 {
		return false, nil
	}
	copy(l.names[index:], l.names[index+1:])
	l.names[len(l.names)-1] = ""
	l.names = l.names[:len(l.names)-1]
	return true, nil
}

// Toggle removes an existing name, otherwise inserts it at the head. The
// operation and its result are protected by one lock so concurrent connector
// input cannot produce duplicate entries or an ambiguous outcome.
func (l *IgnoreList) Toggle(name string) (IgnoreToggleResult, error) {
	canonical, err := CanonicalIgnoreName(name)
	if err != nil {
		return IgnoreToggleResult{}, err
	}
	if l == nil {
		return IgnoreToggleResult{}, errors.New("nil ignore list")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if index := l.indexLocked(canonical); index >= 0 {
		copy(l.names[index:], l.names[index+1:])
		l.names[len(l.names)-1] = ""
		l.names = l.names[:len(l.names)-1]
		return IgnoreToggleResult{Name: canonical, Removed: true, Changed: true}, nil
	}
	if len(l.names) >= MaxIgnoredPlayers {
		return IgnoreToggleResult{}, ErrIgnoreLimit
	}
	l.names = append(l.names, "")
	copy(l.names[1:], l.names[:len(l.names)-1])
	l.names[0] = canonical
	return IgnoreToggleResult{Name: canonical, Added: true, Changed: true}, nil
}

func (l *IgnoreList) indexLocked(name string) int {
	for i, existing := range l.names {
		if existing == name {
			return i
		}
	}
	return -1
}

// ValidateIgnoreName is kept at the transport boundary as well as in the
// session parser. This prevents a future connector helper from bypassing the
// one-token, bounded-name contract by calling IgnoreList directly.
func ValidateIgnoreName(name string) error {
	if name == "" || !utf8.ValidString(name) {
		return ErrInvalidIgnoreName
	}
	if len(name) > IgnoreNameMaxBytes || utf8.RuneCountInString(name) > IgnoreNameMaxRunes {
		return ErrInvalidIgnoreName
	}
	if strings.TrimSpace(name) != name || name == "." || name == ".." {
		return ErrInvalidIgnoreName
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) || r == '/' || r == '\\' || r == ':' {
			return ErrInvalidIgnoreName
		}
	}
	return nil
}

func CanonicalIgnoreName(name string) (string, error) {
	if err := ValidateIgnoreName(name); err != nil {
		return "", err
	}
	canonical := []byte(name)
	if canonical[0] >= 'a' && canonical[0] <= 'z' {
		canonical[0] -= 'a' - 'A'
	}
	return string(canonical), nil
}
