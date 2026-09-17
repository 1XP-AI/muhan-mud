package world

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

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

func TestBankItemSelectorsUseLegacyPrefixes(t *testing.T) {
	s := bankItemFixture()
	player := s.Players["a"]
	sword := player.Items.Items["sword"]
	sword.Object.Name = "Longsword"
	sword.Object.Keys[0] = "BladeAlias"
	player.Items.Items["sword"] = sword
	s.Players["a"] = player

	next, result, err := s.DepositBankItem("a", "blade", 1)
	if err != nil || result.Action != "bank-deposit-item" || result.ItemName != "Longsword" {
		t.Fatalf("deposit by key result=%+v err=%v", result, err)
	}

	account := next.BankAccounts["a"]
	bankSword := account.Items.Items["sword"]
	bankSword.Object.Keys[1] = "VaultAlias"
	account.Items.Items["sword"] = bankSword
	next.BankAccounts["a"] = account
	withdrawn, result, err := next.WithdrawBankItem("a", "vault", 1)
	if err != nil || result.Action != "bank-withdraw-item" || result.ItemName != "Longsword" {
		t.Fatalf("withdraw by key result=%+v err=%v", result, err)
	}
	if len(withdrawn.BankAccounts["a"].Items.Inventory) != 0 || !containsID(withdrawn.Players["a"].Items.Inventory, "sword") {
		t.Fatalf("withdraw by key locations bank=%+v player=%+v", withdrawn.BankAccounts["a"].Items.Inventory, withdrawn.Players["a"].Items.Inventory)
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

func bankInvisibleItemFixture(detect bool) State {
	s := bankItemFixture()
	p := s.Players["a"]
	invisible := p.Items.Items["stone"]
	invisible.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	p.Items.Items["stone"] = invisible
	if detect {
		p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	}
	s.Players["a"] = p
	return s
}

func TestDepositAllBankItemsSkipsInvisibleRootWithoutDetectInvisible(t *testing.T) {
	next, result, err := bankInvisibleItemFixture(false).DepositAllBankItems("a")
	if err != nil || result.Action != "bank-deposit-all" || result.Count != 1 || result.ItemName != "검" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := next.BankAccounts["a"].Items.Inventory; len(got) != 1 || got[0] != "sword" {
		t.Fatalf("bank inventory=%v", got)
	}
	if got := next.Players["a"].Items.Inventory; len(got) != 2 || got[0] != "stone" || got[1] != "bag" {
		t.Fatalf("player inventory=%v", got)
	}
	if _, ok := next.Players["a"].Items.Items["stone"]; !ok {
		t.Fatal("invisible root lost from player ownership")
	}
	if _, ok := next.BankAccounts["a"].Items.Items["stone"]; ok {
		t.Fatal("invisible root moved into bank without detect-invisible")
	}
}

func TestDepositAllBankItemsAdmitsInvisibleRootWithDetectInvisible(t *testing.T) {
	next, result, err := bankInvisibleItemFixture(true).DepositAllBankItems("a")
	if err != nil || result.Action != "bank-deposit-all" || result.Count != 2 || result.ItemName != "검, 돌" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := next.BankAccounts["a"].Items.Inventory; len(got) != 2 || got[0] != "sword" || got[1] != "stone" {
		t.Fatalf("bank inventory=%v", got)
	}
	if got := next.Players["a"].Items.Inventory; len(got) != 1 || got[0] != "bag" {
		t.Fatalf("player inventory=%v", got)
	}
	if _, ok := next.BankAccounts["a"].Items.Items["stone"]; !ok {
		t.Fatal("detected invisible root missing from bank ownership")
	}
	if _, ok := next.Players["a"].Items.Items["stone"]; ok {
		t.Fatal("detected invisible root remained in player ownership")
	}
}

func bankByNameItemFixture() State {
	s := bankFixture()
	p := s.Players["a"]
	flagged := func(bit uint) [8]byte {
		var flags [8]byte
		flags[bit/8] |= 1 << (bit % 8)
		return flags
	}
	p.Items = &ItemCollection{Items: map[string]Item{
		"beta":     {Object: LegacyObject{Name: "BladeTwo", Keys: [3]string{"blade-alias"}, Weight: 1}, Contents: []string{"beta-gem"}},
		"alpha":    {Object: LegacyObject{Name: "BladeOne", Keys: [3]string{"blade-alias"}, Weight: 1}},
		"hidden":   {Object: LegacyObject{Name: "BladeHidden", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flagged(objectInvisibleFlag)}},
		"quest":    {Object: LegacyObject{Name: "BladeQuest", Keys: [3]string{"blade-alias"}, Weight: 1, Quest: 1}},
		"event":    {Object: LegacyObject{Name: "BladeEvent", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flagged(objectEventFlag)}},
		"bag":      {Object: LegacyObject{Name: "BladeBag", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flagged(itemContainerFlag)}},
		"other":    {Object: LegacyObject{Name: "Rock", Keys: [3]string{"stone"}, Weight: 1}},
		"beta-gem": {Object: LegacyObject{Name: "Gem", Weight: 1}},
	}, Inventory: []string{"beta", "alpha", "hidden", "quest", "event", "bag", "other"}}
	s.Players["a"] = p
	s.BankAccounts = map[string]BankAccount{"a": BankAccount{Items: &ItemCollection{
		Items:     map[string]Item{"old": {Object: LegacyObject{Name: "VaultOld", Weight: 1}}},
		Inventory: []string{"old"},
	}}}
	return s
}

func bankByNameWithdrawFixture() State {
	s := bankFixture()
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"owned": {Object: LegacyObject{Name: "Owned", Weight: 1}},
	}, Inventory: []string{"owned"}}
	s.Players["a"] = p
	flags := func(bit uint) [8]byte {
		var out [8]byte
		out[bit/8] |= 1 << (bit % 8)
		return out
	}
	s.BankAccounts = map[string]BankAccount{"a": BankAccount{Items: &ItemCollection{Items: map[string]Item{
		"beta":      {Object: LegacyObject{Name: "BladeTwo", Keys: [3]string{"blade-alias"}, Weight: 1}, Contents: []string{"beta-gem"}},
		"alpha":     {Object: LegacyObject{Name: "BladeOne", Keys: [3]string{"blade-alias"}, Weight: 1}},
		"hidden":    {Object: LegacyObject{Name: "BladeHidden", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flags(objectHiddenFlag)}},
		"invisible": {Object: LegacyObject{Name: "BladeInvisible", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flags(objectInvisibleFlag)}},
		"not-take":  {Object: LegacyObject{Name: "BladeNotTake", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flags(objectNotTakeFlag)}},
		"scenery":   {Object: LegacyObject{Name: "BladeScenery", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flags(objectSceneryFlag)}},
		"event":     {Object: LegacyObject{Name: "BladeEvent", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flags(objectEventFlag)}},
		"bag":       {Object: LegacyObject{Name: "BladeBag", Keys: [3]string{"blade-alias"}, Weight: 1, Flags: flags(itemContainerFlag)}},
		"other":     {Object: LegacyObject{Name: "Rock", Keys: [3]string{"stone"}, Weight: 1}},
		"beta-gem":  {Object: LegacyObject{Name: "Gem", Weight: 1}},
	}, Inventory: []string{"beta", "alpha", "hidden", "invisible", "not-take", "scenery", "event", "bag", "other"}}}}
	return s
}

func TestBankItemsByNameMovesMatchingRootsByKeyAndSkipsDepositIneligibleRoots(t *testing.T) {
	s := bankByNameItemFixture()
	before := append([]string(nil), s.Players["a"].Items.Inventory...)
	next, result, err := s.DepositBankItemsByName("a", "BLADE")
	if err != nil || result.Action != "bank-deposit-all-by-name" || result.Count != 2 || result.ItemName != "BladeTwo, BladeOne" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := next.BankAccounts["a"].Items.Inventory; !reflect.DeepEqual(got, []string{"alpha", "beta", "old"}) {
		t.Fatalf("bank order=%v", got)
	}
	if got := next.Players["a"].Items.Inventory; !reflect.DeepEqual(got, []string{"hidden", "quest", "event", "bag", "other"}) {
		t.Fatalf("player order=%v", got)
	}
	if !reflect.DeepEqual(s.Players["a"].Items.Inventory, before) {
		t.Fatal("deposit mutated source snapshot")
	}
	if _, ok := next.BankAccounts["a"].Items.Items["beta-gem"]; !ok {
		t.Fatal("canonical child was not moved with beta root")
	}
	if _, ok := next.Players["a"].Items.Items["beta"]; ok {
		t.Fatal("beta root retained by player")
	}
	noMatch, noMatchResult, err := next.DepositBankItemsByName("a", "missing")
	if err != nil || noMatchResult.Count != 0 || !reflect.DeepEqual(noMatch.Players["a"].Items.Inventory, next.Players["a"].Items.Inventory) {
		t.Fatalf("nonmatching result=%+v err=%v state=%v", noMatchResult, err, noMatch.Players["a"].Items.Inventory)
	}
}

func TestBankItemsByNameWithdrawsVisibleRootsAndSkipsInvisibleEvent(t *testing.T) {
	s := bankByNameWithdrawFixture()
	next, result, err := s.WithdrawBankItemsByName("a", "blade")
	if err != nil || result.Action != "bank-withdraw-all-by-name" || result.Count != 3 || result.ItemName != "BladeTwo, BladeOne, BladeBag" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := next.Players["a"].Items.Inventory; !reflect.DeepEqual(got, []string{"bag", "alpha", "beta", "owned"}) {
		t.Fatalf("player order=%v", got)
	}
	if got := next.BankAccounts["a"].Items.Inventory; !reflect.DeepEqual(got, []string{"hidden", "invisible", "not-take", "scenery", "event", "other"}) {
		t.Fatalf("bank order=%v", got)
	}
	if _, ok := next.Players["a"].Items.Items["beta-gem"]; !ok {
		t.Fatal("canonical child was not moved with beta root")
	}
	noMatch, noMatchResult, err := next.WithdrawBankItemsByName("a", "missing")
	if err != nil || noMatchResult.Count != 0 || !reflect.DeepEqual(noMatch.BankAccounts["a"].Items.Inventory, next.BankAccounts["a"].Items.Inventory) {
		t.Fatalf("nonmatching result=%+v err=%v state=%v", noMatchResult, err, noMatch.BankAccounts["a"].Items.Inventory)
	}
}

func TestBankItemsByNameWithdrawSkipsWeightAndCapacity(t *testing.T) {
	heavy := bankByNameWithdrawFixture()
	account := heavy.BankAccounts["a"]
	item := account.Items.Items["beta"]
	item.Object.Weight = 25
	account.Items.Items["beta"] = item
	heavy.BankAccounts["a"] = account
	next, result, err := heavy.WithdrawBankItemsByName("a", "blade")
	if err != nil || result.Count != 2 || result.ItemName != "BladeOne, BladeBag" {
		t.Fatalf("weight result=%+v err=%v", result, err)
	}
	if containsID(next.Players["a"].Items.Inventory, "beta") {
		t.Fatal("overweight matching root was withdrawn")
	}

	capacity := bankFixture()
	p := capacity.Players["a"]
	p.Body.Stats[0] = 20
	p.Items = &ItemCollection{Items: map[string]Item{}, Inventory: make([]string, 0, 150)}
	for i := 0; i < 150; i++ {
		id := fmt.Sprintf("owned-%03d", i)
		p.Items.Items[id] = Item{Object: LegacyObject{Name: id, Weight: 1}}
		p.Items.Inventory = append(p.Items.Inventory, id)
	}
	capacity.Players["a"] = p
	capacity.BankAccounts = map[string]BankAccount{"a": BankAccount{Items: &ItemCollection{
		Items: map[string]Item{
			"blade-one": {Object: LegacyObject{Name: "BladeOne", Keys: [3]string{"blade"}, Weight: 1}},
			"blade-two": {Object: LegacyObject{Name: "BladeTwo", Keys: [3]string{"blade"}, Weight: 1}},
		},
		Inventory: []string{"blade-one", "blade-two"},
	}}}
	next, result, err = capacity.WithdrawBankItemsByName("a", "blade")
	if err != nil || result.Count != 1 || result.ItemName != "BladeOne" || len(next.BankAccounts["a"].Items.Inventory) != 1 || len(next.Players["a"].Items.Inventory) != 151 {
		t.Fatalf("capacity result=%+v err=%v bank=%v player=%d", result, err, next.BankAccounts["a"].Items.Inventory, len(next.Players["a"].Items.Inventory))
	}
	if !containsID(next.Players["a"].Items.Inventory, "blade-one") || !containsID(next.BankAccounts["a"].Items.Inventory, "blade-two") {
		t.Fatalf("capacity boundary owners player=%v bank=%v", next.Players["a"].Items.Inventory, next.BankAccounts["a"].Items.Inventory)
	}
}

func TestBankInventoryDetectOnlyBypassesInvisible(t *testing.T) {
	s := bankByNameWithdrawFixture()
	p := s.Players["a"]
	p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = p
	got, err := s.BankInventory("a")
	if err != nil {
		t.Fatal(err)
	}
	want := "보관물:\r\n  BladeTwo, BladeOne, BladeInvisible, BladeEvent, BladeBag, Rock.\r\n"
	if got != want {
		t.Fatalf("listing=%q want=%q", got, want)
	}
}

func TestBankItemsByNameWithdrawDetectDoesNotBypassBankExclusions(t *testing.T) {
	s := bankByNameWithdrawFixture()
	p := s.Players["a"]
	p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = p
	next, result, err := s.WithdrawBankItemsByName("a", "blade")
	if err != nil || result.Count != 4 || result.ItemName != "BladeTwo, BladeOne, BladeInvisible, BladeBag" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := next.BankAccounts["a"].Items.Inventory; !reflect.DeepEqual(got, []string{"hidden", "not-take", "scenery", "event", "other"}) {
		t.Fatalf("bank order=%v", got)
	}
}

func TestBankItemsByNameUsesCanonicalSignedDestinationOrdering(t *testing.T) {
	s := bankFixture()
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"owned": {Object: LegacyObject{Name: "Owned", Weight: 1}},
	}, Inventory: []string{"owned"}}
	s.Players["a"] = p
	s.BankAccounts = map[string]BankAccount{"a": {Items: &ItemCollection{Items: map[string]Item{
		"high": {Object: LegacyObject{Name: "Blade", Keys: [3]string{"blade"}, Weight: 1, Adjustment: 2}},
		"low":  {Object: LegacyObject{Name: "Blade", Keys: [3]string{"blade"}, Weight: 1, Adjustment: 255}},
		"mid":  {Object: LegacyObject{Name: "Blade", Keys: [3]string{"blade"}, Weight: 1, Adjustment: 1}},
	}, Inventory: []string{"high", "low", "mid"}}}}

	next, result, err := s.WithdrawBankItemsByName("a", "blade")
	if err != nil || result.Count != 3 || result.ItemName != "Blade(+2), Blade(-1), Blade(+1)" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := next.Players["a"].Items.Inventory; !reflect.DeepEqual(got, []string{"low", "mid", "high", "owned"}) {
		t.Fatalf("canonical destination order=%v", got)
	}
}

func TestBankItemNamesGroupsContiguousNameAndAdjustmentWithinLimit(t *testing.T) {
	items := ItemCollection{Items: map[string]Item{
		"a": {Object: LegacyObject{Name: "Blade", Adjustment: 1}},
		"b": {Object: LegacyObject{Name: "Blade", Adjustment: 1}},
		"c": {Object: LegacyObject{Name: "Blade", Adjustment: 2}},
	}}
	roots := []string{"a", "b", "c"}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("long-%03d", i)
		items.Items[id] = Item{Object: LegacyObject{Name: strings.Repeat("x", 40) + fmt.Sprintf("-%03d", i)}}
		roots = append(roots, id)
	}
	got := bankItemNames(items, roots)
	if len(got) > bankItemResponseLimit || !strings.HasPrefix(got, "Blade(+1) x2, Blade(+2), ") {
		preview := got
		if len(preview) > 80 {
			preview = preview[:80]
		}
		t.Fatalf("grouped/bounded names len=%d value=%q", len(got), preview)
	}
}
