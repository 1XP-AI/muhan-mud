package session

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

type registrationProbe struct {
	calls, loads int
	ids          []string
	hashes       [][]byte
	fail         bool
}

func (p *registrationProbe) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	p.loads++
	return storage.WorldSnapshot{Revision: int64(p.loads)}, nil
}
func (p *registrationProbe) CreateInWorld(ctx context.Context, w, id string, rev int64, name string, hash []byte, draft game.Creation) (string, error) {
	p.calls++
	p.ids = append(p.ids, id)
	p.hashes = append(p.hashes, bytes.Clone(hash))
	if rev != int64(p.loads) || w != "world" || name != "Alice" {
		return "", errors.New("wrong registration context")
	}
	if p.fail || p.calls == 1 {
		return "", errors.New("commit response unavailable")
	}
	if p.calls == 2 {
		return "", storage.ErrWorldConflict
	}
	return "character-id", nil
}
func TestWorldAccountsKeepsRegistrationIdentityAcrossRetries(t *testing.T) {
	probe := &registrationProbe{}
	accounts := NewWorldAccounts(nil, probe, "world")
	draft, err := game.BuildCreation(game.CreationChoices{Class: 4, Weapon: 1, RaceChoice: 7, Stats: game.Stats{12, 10, 12, 10, 10}})
	if err != nil {
		t.Fatal(err)
	}
	id, err := accounts.Register(context.Background(), "alice", []byte("pw1234"), draft)
	if err != nil || id != "character-id" || probe.calls != 3 {
		t.Fatalf("%q %v calls%d", id, err, probe.calls)
	}
	for i := range probe.ids {
		if probe.ids[i] == "" || probe.ids[i] != probe.ids[0] || !bytes.Equal(probe.hashes[i], probe.hashes[0]) || !identity.CheckPassword(probe.hashes[i], []byte("pw1234")) {
			t.Fatal("retry changed ID or hash")
		}
	}
}
func TestWorldAccountsRejectsInvalidDraftWithoutIO(t *testing.T) {
	probe := &registrationProbe{}
	accounts := NewWorldAccounts(nil, probe, "world")
	if _, err := accounts.Register(context.Background(), "Alice", []byte("pw1234"), game.Creation{}); !errors.Is(err, game.ErrCreation) || probe.calls != 0 || probe.loads != 0 {
		t.Fatalf("invalid draft reached storage: %v", err)
	}
}

func TestTerminalRegistrationUsesWorldAccounts(t *testing.T) {
	for _, fail := range []bool{false, true} {
		base := &fakeStore{}
		probe := &registrationProbe{fail: fail}
		login := NewLogin(NewWorldAccounts(base, probe, "world"))
		for _, line := range []string{"Alice", "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7"} {
			v := login.Submit(context.Background(), line)
			if v.Closed || v.Verified != nil {
				t.Fatal("premature completion")
			}
		}
		if !login.View().Secret {
			t.Fatal("password echo enabled")
		}
		v := login.Submit(context.Background(), "pw1234")
		if probe.calls != 3 || base.registrations != 0 || strings.Contains(v.Text, "pw1234") {
			t.Fatalf("wrong registration path: %+v", v)
		}
		if fail {
			if !v.Closed || v.Verified != nil {
				t.Fatal("unknown outcome admitted")
			}
		} else {
			if v.Closed || v.Verified == nil || v.Verified.ID != "character-id" {
				t.Fatal("world creation not verified")
			}
		}
		login.Close()
	}
}
