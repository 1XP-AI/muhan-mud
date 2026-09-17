package session

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
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

func bankCommandByNameFixture() []byte {
	s, err := world.DecodeState(bankCommandFixture())
	if err != nil {
		panic(err)
	}
	p := s.Players["a"]
	p.Items = &world.ItemCollection{Items: map[string]world.Item{
		"beta":  {Object: world.LegacyObject{Name: "BladeTwo", Keys: [3]string{"blade-alias"}, Weight: 1}},
		"alpha": {Object: world.LegacyObject{Name: "BladeOne", Keys: [3]string{"blade-alias"}, Weight: 1}},
		"other": {Object: world.LegacyObject{Name: "Rock", Keys: [3]string{"stone"}, Weight: 1}},
	}, Inventory: []string{"beta", "alpha", "other"}}
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
		{"250냥 입금", world.BankDeposit, false, 250},
		{"모두 출금", world.BankWithdraw, true, 0},
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
	deposit, ok := parseBankLine("검 보관물")
	if !ok || deposit.kind != "deposit-item" || deposit.name != "검" || deposit.occurrence != 1 {
		t.Fatalf("last-token deposit item=%+v ok=%t", deposit, ok)
	}
	withdraw, ok := parseBankLine("검 받아")
	if !ok || withdraw.kind != "withdraw-item" || withdraw.name != "검" || withdraw.occurrence != 1 {
		t.Fatalf("last-token withdraw item=%+v ok=%t", withdraw, ok)
	}
	for _, tc := range []struct {
		line, kind, name string
	}{
		{"모든검 보관물", "deposit-items-by-name", "검"},
		{"보관물 모든검", "deposit-items-by-name", "검"},
		{"모든검 받아", "withdraw-items-by-name", "검"},
		{"받아 모든검", "withdraw-items-by-name", "검"},
	} {
		got, ok := parseBankLine(tc.line)
		if !ok || got.kind != tc.kind || got.name != tc.name || got.occurrence != 1 || got.all {
			t.Fatalf("by-name line=%q got=%+v ok=%t", tc.line, got, ok)
		}
	}
	both, ok := parseBankLine("보관물 받아")
	if !ok || both.kind != "withdraw-item" || both.name != "보관물" {
		t.Fatalf("last-token wins=%+v ok=%t", both, ok)
	}
	for _, line := range []string{"", "검", "동 입금 extra", "입금 250냥 extra", "모든 보관물", "보관물 모든", "모든 받아", "받아 모든"} {
		if _, ok := parseBankLine(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
}

func executeParsedBankLine(t *testing.T, owners *Ownership, store *departureStore, lease SessionLease, commandID, line string) storage.WorldReceipt {
	t.Helper()
	parsed, err := ParseCommand(line)
	if err != nil {
		t.Fatalf("ParseCommand(%q) err=%v", line, err)
	}
	if parsed.Kind == CommandDirectional {
		t.Fatalf("ParseCommand(%q)=CommandDirectional", line)
	}
	if parsed.Kind != CommandBank {
		t.Fatalf("ParseCommand(%q)=%+v want CommandBank", line, parsed)
	}
	receipt, execErr := owners.ExecuteBankLine(context.Background(), store, "w", commandID, lease, line)
	if execErr != nil {
		t.Fatalf("ExecuteBankLine(%q) err=%v", line, execErr)
	}
	return receipt
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

func TestExecuteBankLineLastTokenMoneyWithoutMoving(t *testing.T) {
	store := &departureStore{state: bankCommandFixture()}
	owners, lease := admitBankOwner(t)
	first := executeParsedBankLine(t, owners, store, lease, "bank-suffix-deposit-1", "250냥 입금")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "250") {
		t.Fatalf("deposit=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["a"].Body.Gold != 750 || saved.BankAccounts["a"].Balance != 250 {
		t.Fatalf("deposit moved or mutated=%+v err=%v", saved.Players["a"], err)
	}
	replay := executeParsedBankLine(t, owners, store, lease, "bank-suffix-deposit-1", "250냥 입금")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v commits=%d", replay, store.commits)
	}
	withdraw := executeParsedBankLine(t, owners, store, lease, "bank-suffix-withdraw-1", "모두 출금")
	if withdraw.Replayed || store.commits != 2 || !strings.Contains(string(withdraw.Response), "250") {
		t.Fatalf("withdraw=%q commits=%d replayed=%t", withdraw.Response, store.commits, withdraw.Replayed)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["a"].Body.Gold != 1000 || saved.BankAccounts["a"].Balance != 0 {
		t.Fatalf("withdraw moved or mutated=%+v err=%v", saved.Players["a"], err)
	}
}

func TestExecuteBankLineLastTokenItemsWithoutMoving(t *testing.T) {
	store := &departureStore{state: bankCommandItemFixture()}
	owners, lease := admitBankOwner(t)
	first := executeParsedBankLine(t, owners, store, lease, "bank-suffix-item-1", "검 보관물")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "검") {
		t.Fatalf("deposit item=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 || bankTestHasID(saved.Players["a"].Items.Inventory, "sword") || !bankTestHasID(saved.BankAccounts["a"].Items.Inventory, "sword") {
		t.Fatalf("deposit item moved or mutated player=%+v bank=%+v err=%v", saved.Players["a"], saved.BankAccounts["a"], err)
	}
	replay := executeParsedBankLine(t, owners, store, lease, "bank-suffix-item-1", "검 보관물")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("deposit replay=%+v commits=%d", replay, store.commits)
	}
	withdraw := executeParsedBankLine(t, owners, store, lease, "bank-suffix-item-2", "검 받아")
	if withdraw.Replayed || store.commits != 2 || !strings.Contains(string(withdraw.Response), "검") {
		t.Fatalf("withdraw item=%q commits=%d replayed=%t", withdraw.Response, store.commits, withdraw.Replayed)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 || !bankTestHasID(saved.Players["a"].Items.Inventory, "sword") || bankTestHasID(saved.BankAccounts["a"].Items.Inventory, "sword") {
		t.Fatalf("withdraw item moved or mutated player=%+v bank=%+v err=%v", saved.Players["a"], saved.BankAccounts["a"], err)
	}
}

func TestExecuteBankLineByNameMovesAtomicallyAndReplays(t *testing.T) {
	store := &departureStore{state: bankCommandByNameFixture()}
	owners, lease := admitBankOwner(t)
	first := executeParsedBankLine(t, owners, store, lease, "bank-by-name-1", "모든blade 보관물")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "BladeTwo, BladeOne") {
		t.Fatalf("deposit=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !reflect.DeepEqual(saved.BankAccounts["a"].Items.Inventory, []string{"beta", "alpha"}) || !reflect.DeepEqual(saved.Players["a"].Items.Inventory, []string{"other"}) {
		t.Fatalf("deposit state=%+v err=%v", saved, err)
	}
	replay := executeParsedBankLine(t, owners, store, lease, "bank-by-name-1", "모든blade 보관물")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("deposit replay=%+v commits=%d", replay, store.commits)
	}
	withdraw := executeParsedBankLine(t, owners, store, lease, "bank-by-name-2", "받아 모든blade")
	if withdraw.Replayed || store.commits != 2 || !strings.Contains(string(withdraw.Response), "BladeTwo, BladeOne") {
		t.Fatalf("withdraw=%q commits=%d replayed=%t", withdraw.Response, store.commits, withdraw.Replayed)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || len(saved.BankAccounts["a"].Items.Inventory) != 0 || !reflect.DeepEqual(saved.Players["a"].Items.Inventory, []string{"other", "beta", "alpha"}) {
		t.Fatalf("withdraw state=%+v err=%v", saved, err)
	}
}
