package world

import "testing"

func bankFixture() State {
	var flags [8]byte
	flags[RoomBankFlag/8] |= 1 << (RoomBankFlag % 8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Flags: flags}},
			PlayerIDs: []string{"a"},
			Items:     &ItemCollection{Items: map[string]Item{}},
		}},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: 1000}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}},
		},
	}
}

func TestApplyBankMoneyRequiresBankRoom(t *testing.T) {
	s := bankFixture()
	r := s.Rooms[1]
	r.Resource.Flags = [8]byte{}
	s.Rooms[1] = r
	if _, _, err := s.ApplyBankMoney("a", BankDeposit, 10, false); err == nil {
		t.Fatal("deposit outside bank room unexpectedly succeeded")
	}
}

func TestApplyBankMoneyDepositsAndWithdrawsAtomically(t *testing.T) {
	s := bankFixture()
	next, deposit, err := s.ApplyBankMoney("a", BankDeposit, 250, false)
	if err != nil || deposit.Amount != 250 || deposit.Balance != 250 || deposit.Gold != 750 {
		t.Fatalf("deposit=%+v err=%v", deposit, err)
	}
	if next.Players["a"].Body.Gold != 750 || next.BankAccounts["a"].Balance != 250 {
		t.Fatalf("deposit state=%+v bank=%+v", next.Players["a"].Body.Gold, next.BankAccounts["a"])
	}

	next, withdraw, err := next.ApplyBankMoney("a", BankWithdraw, 0, true)
	if err != nil || withdraw.Amount != 250 || withdraw.Balance != 0 || withdraw.Gold != 1000 {
		t.Fatalf("withdraw=%+v err=%v", withdraw, err)
	}
	if next.Players["a"].Body.Gold != 1000 || next.BankAccounts["a"].Balance != 0 {
		t.Fatalf("withdraw state=%+v bank=%+v", next.Players["a"].Body.Gold, next.BankAccounts["a"])
	}
}

func TestApplyBankMoneyRejectsOverflowAndInsufficientFunds(t *testing.T) {
	s := bankFixture()
	if _, _, err := s.ApplyBankMoney("a", BankDeposit, 1001, false); err == nil {
		t.Fatal("overdraft deposit unexpectedly succeeded")
	}
	s.BankAccounts = map[string]BankAccount{"a": {Balance: MaxBankBalance}}
	if _, _, err := s.ApplyBankMoney("a", BankDeposit, 1, false); err == nil {
		t.Fatal("bank overflow unexpectedly succeeded")
	}
	if _, _, err := s.ApplyBankMoney("a", BankWithdraw, 1, false); err != nil {
		t.Fatalf("withdraw should succeed with balance: %v", err)
	}
}

func TestParseBankAmount(t *testing.T) {
	if got, all, err := ParseBankAmount("12냥"); err != nil || all || got != 12 {
		t.Fatalf("amount=%d all=%t err=%v", got, all, err)
	}
	if got, all, err := ParseBankAmount("모두"); err != nil || !all || got != 0 {
		t.Fatalf("all=%d all=%t err=%v", got, all, err)
	}
}

func bankItemFixture() State {
	s := bankFixture()
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Weight: 1}},
		"stone": {Object: LegacyObject{Name: "돌", Weight: 1}},
		"bag":   {Object: LegacyObject{Name: "가방", Flags: [8]byte{1 << itemContainerFlag}}},
	}, Inventory: []string{"sword", "stone", "bag"}}
	s.Players["a"] = p
	return s
}

func TestBankItemGraphMovesCanonicalRoots(t *testing.T) {
	s := bankItemFixture()
	next, deposit, err := s.DepositBankItem("a", "검", 1)
	if err != nil || deposit.Action != "bank-deposit-item" || next.Players["a"].Items.Items["sword"].Object.Name != "" {
		t.Fatalf("deposit=%+v err=%v state=%+v", deposit, err, next)
	}
	if len(next.Players["a"].Items.Inventory) != 2 || len(next.BankAccounts["a"].Items.Inventory) != 1 || next.BankAccounts["a"].Items.Inventory[0] != "sword" {
		t.Fatalf("deposit owners player=%+v bank=%+v", next.Players["a"].Items.Inventory, next.BankAccounts["a"].Items.Inventory)
	}

	next, withdraw, err := next.WithdrawBankItem("a", "검", 1)
	if err != nil || withdraw.Action != "bank-withdraw-item" || len(next.BankAccounts["a"].Items.Inventory) != 0 {
		t.Fatalf("withdraw=%+v err=%v state=%+v", withdraw, err, next)
	}
	if len(next.Players["a"].Items.Inventory) != 3 || !containsString(next.Players["a"].Items.Inventory, "bag") || !containsString(next.Players["a"].Items.Inventory, "sword") {
		t.Fatalf("withdraw inventory=%+v", next.Players["a"].Items.Inventory)
	}
}

func TestBankItemGraphRejectsContainersAndSupportsAll(t *testing.T) {
	s := bankItemFixture()
	if _, _, err := s.DepositBankItem("a", "가방", 1); err == nil {
		t.Fatal("container deposit unexpectedly succeeded")
	}
	next, all, err := s.DepositAllBankItems("a")
	if err != nil || all.Count != 2 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	if len(next.Players["a"].Items.Inventory) != 1 || len(next.BankAccounts["a"].Items.Inventory) != 2 {
		t.Fatalf("all owners player=%+v bank=%+v", next.Players["a"].Items.Inventory, next.BankAccounts["a"].Items.Inventory)
	}
	next, all, err = next.WithdrawAllBankItems("a")
	if err != nil || all.Count != 2 || len(next.BankAccounts["a"].Items.Inventory) != 0 {
		t.Fatalf("withdraw all=%+v err=%v", all, err)
	}
}
