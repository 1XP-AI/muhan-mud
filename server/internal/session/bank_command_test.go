package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func bankCommandFixture() []byte {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	r := s.Rooms[1]
	r.Resource.Flags[world.RoomBankFlag/8] |= 1 << (world.RoomBankFlag % 8)
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Body.Gold = 1000
	s.Players["a"] = p
	raw, _ := json.Marshal(s)
	return raw
}

func bankCommandItemFixture() []byte {
	s, err := world.DecodeState(bankCommandFixture())
	if err != nil {
		panic(err)
	}
	p := s.Players["a"]
	p.Items = &world.ItemCollection{Items: map[string]world.Item{
		"sword": {Object: world.LegacyObject{Name: "검", Weight: 1}},
		"stone": {Object: world.LegacyObject{Name: "돌", Weight: 1}},
	}, Inventory: []string{"sword", "stone"}}
	s.Players["a"] = p
	raw, _ := json.Marshal(s)
	return raw
}

func bankTestHasID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func admitBankOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseBankLine(t *testing.T) {
	cases := []struct {
		line string
		op   world.BankMoneyOperation
		all  bool
		amt  int64
	}{
		{"잔액", world.BankBalance, false, 0},
		{"입금 12냥", world.BankDeposit, false, 12},
		{"출금 모두", world.BankWithdraw, true, 0},
	}
	for _, tc := range cases {
		got, ok := parseBankLine(tc.line)
		if !ok || got.operation != tc.op || got.all != tc.all || got.amount != tc.amt {
			t.Fatalf("line=%q got=%+v ok=%t", tc.line, got, ok)
		}
	}
	if got, ok := parseBankLine("보관물"); !ok || got.kind != "inventory" {
		t.Fatalf("bank inventory command not parsed: %+v ok=%t", got, ok)
	}
}

func TestExecuteBankLinePersistsAndReplays(t *testing.T) {
	store := &departureStore{state: bankCommandFixture()}
	owners, lease := admitBankOwner(t)
	first, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-deposit-1", lease, "입금 250냥")
	if err != nil || !strings.Contains(string(first.Response), "250") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 750 || saved.BankAccounts["a"].Balance != 250 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-deposit-1", lease, "입금 250냥")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	withdraw, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-withdraw-1", lease, "출금 모두")
	if err != nil || !strings.Contains(string(withdraw.Response), "250") || store.commits != 2 {
		t.Fatalf("withdraw=%q err=%v commits=%d", withdraw.Response, err, store.commits)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 1000 || saved.BankAccounts["a"].Balance != 0 {
		t.Fatalf("withdraw saved=%+v err=%v", saved, err)
	}
}

func TestExecuteBankLineRejectsNonBankRoom(t *testing.T) {
	state, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	storeState, _ := json.Marshal(state)
	store := &departureStore{state: storeState}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-bad-room", lease, "잔액"); err == nil {
		t.Fatal("balance outside bank room unexpectedly succeeded")
	}
	if store.commits != 0 {
		t.Fatalf("unexpected commit count=%d", store.commits)
	}
}

func TestExecuteBankLineMovesItemsAndListsBank(t *testing.T) {
	store := &departureStore{state: bankCommandItemFixture()}
	owners, lease := admitBankOwner(t)
	first, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-item-1", lease, "보관물 검")
	if err != nil || !strings.Contains(string(first.Response), "검") || store.commits != 1 {
		t.Fatalf("deposit item=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-item-1", lease, "보관물 검")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("deposit replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	listing, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-item-list", lease, "보관물")
	if err != nil || !strings.Contains(string(listing.Response), "검") || store.commits != 2 {
		t.Fatalf("listing=%q err=%v commits=%d", listing.Response, err, store.commits)
	}
	withdraw, err := owners.ExecuteBankLine(context.Background(), store, "w", "bank-item-2", lease, "받아 검")
	if err != nil || !strings.Contains(string(withdraw.Response), "검") || store.commits != 3 {
		t.Fatalf("withdraw item=%q err=%v commits=%d", withdraw.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.BankAccounts["a"].Items.Inventory) != 0 || !bankTestHasID(saved.Players["a"].Items.Inventory, "sword") {
		t.Fatalf("item state=%+v err=%v", saved, err)
	}
}
