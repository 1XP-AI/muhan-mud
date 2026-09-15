package world

import (
	"fmt"
	"strings"
)

const bankItemCapacity = 200

type BankItemResult struct {
	ItemName string
	Action   string
	Count    int
}

func (s State) bankItemsContext(actorID string) (State, PlayerState, BankAccount, error) {
	_, p, err := s.BankRoom(actorID)
	if err != nil {
		return State{}, PlayerState{}, BankAccount{}, err
	}
	if p.Items == nil {
		return State{}, PlayerState{}, BankAccount{}, fmt.Errorf("online player with migrated items required")
	}
	account := BankAccount{}
	if s.BankAccounts != nil {
		account = s.BankAccounts[actorID]
	}
	if account.Items != nil {
		if err := account.Items.Validate(); err != nil {
			return State{}, PlayerState{}, BankAccount{}, err
		}
	}
	return s, p, account, nil
}

func emptyBankItems(account BankAccount) ItemCollection {
	if account.Items == nil {
		return ItemCollection{Items: map[string]Item{}}
	}
	return account.Items.clone()
}

func bankVisible(object LegacyObject, detect bool) bool {
	return detect || (!flag(object.Flags[:], objectInvisibleFlag) && !flag(object.Flags[:], objectHiddenFlag) && !flag(object.Flags[:], objectNotTakeFlag) && !flag(object.Flags[:], objectSceneryFlag))
}

func bankItemListing(items ItemCollection, detect bool) string {
	entries := make([]string, 0, len(items.Inventory))
	for i := 0; i < len(items.Inventory); i++ {
		id := items.Inventory[i]
		item, ok := items.Items[id]
		if !ok || !bankVisible(item.Object, detect) {
			continue
		}
		name := displayItemName(item)
		count := 1
		for i+1 < len(items.Inventory) {
			next, ok := items.Items[items.Inventory[i+1]]
			if !ok || !bankVisible(next.Object, detect) || next.Object.Name != item.Object.Name || next.Object.Adjustment != item.Object.Adjustment {
				break
			}
			i++
			count++
		}
		if count > 1 {
			name = fmt.Sprintf("%s x%d", name, count)
		}
		entries = append(entries, name)
	}
	if len(entries) == 0 {
		return "보관물:\r\n  없음.\r\n"
	}
	return "보관물:\r\n  " + strings.Join(entries, ", ") + ".\r\n"
}

// BankInventory renders the bank object roots without exposing canonical IDs.
// A nil Items pointer is an explicit not-yet-imported empty view, not a reason
// to mint or mutate an object graph during a read.
func (s State) BankInventory(actorID string) (string, error) {
	_, p, account, err := s.bankItemsContext(actorID)
	if err != nil {
		return "", err
	}
	detect := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	items := emptyBankItems(account)
	if err := items.Validate(); err != nil {
		return "", err
	}
	_ = s
	return bankItemListing(items, detect), nil
}

// DepositBankItem moves one direct player inventory root into the per-player
// bank graph. Container roots are rejected exactly like input_bank; nested
// descendants remain attached if a previously imported non-container graph has
// them. The returned candidate is committed by the durable command executor.
func (s State) DepositBankItem(actorID, name string, occurrence int) (State, BankItemResult, error) {
	s, p, account, err := s.bankItemsContext(actorID)
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	if occurrence < 1 {
		return State{}, BankItemResult{}, fmt.Errorf("invalid item occurrence")
	}
	bank := emptyBankItems(account)
	if len(bank.Inventory) >= bankItemCapacity {
		return State{}, BankItemResult{}, fmt.Errorf("은행 보관함이 가득 찼습니다")
	}
	id, err := selectInventoryRoot(*p.Items, name, occurrence, nil)
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	item := p.Items.Items[id]
	if flag(item.Object.Flags[:], itemContainerFlag) {
		return State{}, BankItemResult{}, fmt.Errorf("컨테이너는 바로 보관할 수 없습니다")
	}
	plan, err := TransferItemRoots(*p.Items, bank, []string{id})
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &plan.Source
	next.Players[actorID] = nextPlayer
	if next.BankAccounts == nil {
		next.BankAccounts = map[string]BankAccount{}
	}
	next.BankAccounts[actorID] = BankAccount{Balance: account.Balance, Items: &plan.Destination}
	if err := next.Validate(); err != nil {
		return State{}, BankItemResult{}, err
	}
	return next, BankItemResult{ItemName: item.Object.Name, Action: "bank-deposit-item"}, nil
}

// WithdrawBankItem moves one bank root into the player's inventory while
// retaining canonical IDs and descendants. Visibility, carry weight and the
// legacy 150-item player limit are checked before the candidate is returned.
func (s State) WithdrawBankItem(actorID, name string, occurrence int) (State, BankItemResult, error) {
	s, p, account, err := s.bankItemsContext(actorID)
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	if account.Items == nil {
		return State{}, BankItemResult{}, fmt.Errorf("은행에 그런 물건이 없습니다")
	}
	detect := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	id, err := selectInventoryRoot(*account.Items, name, occurrence, func(object LegacyObject) bool { return bankVisible(object, detect) })
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	rootWeight, err := account.Items.objectWeight(id)
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	carried, err := p.Items.Weight()
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	capacity, err := p.Items.CapacityCount()
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	if carried+rootWeight > maxPlayerWeight(p.Body) || capacity >= 150 {
		return State{}, BankItemResult{}, fmt.Errorf("더이상 가질 수 없습니다")
	}
	item := account.Items.Items[id]
	if flag(item.Object.Flags[:], objectEventFlag) && !flag(item.Object.Flags[:], objectOneWevFlag) {
		return State{}, BankItemResult{}, fmt.Errorf("이벤트 물건의 소모 효과는 아직 구현되지 않았습니다")
	}
	plan, err := TransferItemRoots(*account.Items, *p.Items, []string{id})
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &plan.Destination
	next.Players[actorID] = nextPlayer
	if next.BankAccounts == nil {
		next.BankAccounts = map[string]BankAccount{}
	}
	next.BankAccounts[actorID] = BankAccount{Balance: account.Balance, Items: &plan.Source}
	if err := next.Validate(); err != nil {
		return State{}, BankItemResult{}, err
	}
	return next, BankItemResult{ItemName: item.Object.Name, Action: "bank-withdraw-item"}, nil
}

// DepositAllBankItems and WithdrawAllBankItems implement the source's
// `보관물 모두`/`받아 모두` shape. Ineligible roots are skipped; successful
// moves remain one atomic candidate and never allocate new IDs.
func (s State) DepositAllBankItems(actorID string) (State, BankItemResult, error) {
	s, p, account, err := s.bankItemsContext(actorID)
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	source := p.Items.clone()
	destination := emptyBankItems(account)
	ids := append([]string(nil), source.Inventory...)
	count := 0
	var names []string
	for _, id := range ids {
		if len(destination.Inventory) >= bankItemCapacity {
			break
		}
		item, ok := source.Items[id]
		if !ok || flag(item.Object.Flags[:], itemContainerFlag) || (item.Object.Quest != 0 && p.Body.Class < playerDMClass) || flag(item.Object.Flags[:], objectEventFlag) {
			continue
		}
		plan, err := TransferItemRoots(source, destination, []string{id})
		if err != nil {
			return State{}, BankItemResult{}, err
		}
		source, destination = plan.Source, plan.Destination
		count++
		names = append(names, item.Object.Name)
	}
	if count == 0 {
		return s.clone(), BankItemResult{Action: "bank-deposit-all"}, nil
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &source
	next.Players[actorID] = nextPlayer
	if next.BankAccounts == nil {
		next.BankAccounts = map[string]BankAccount{}
	}
	next.BankAccounts[actorID] = BankAccount{Balance: account.Balance, Items: &destination}
	if err := next.Validate(); err != nil {
		return State{}, BankItemResult{}, err
	}
	return next, BankItemResult{ItemName: strings.Join(names, ", "), Action: "bank-deposit-all", Count: count}, nil
}

func (s State) WithdrawAllBankItems(actorID string) (State, BankItemResult, error) {
	s, p, account, err := s.bankItemsContext(actorID)
	if err != nil {
		return State{}, BankItemResult{}, err
	}
	if account.Items == nil {
		return s.clone(), BankItemResult{Action: "bank-withdraw-all"}, nil
	}
	source := account.Items.clone()
	destination := p.Items.clone()
	ids := append([]string(nil), source.Inventory...)
	count := 0
	var names []string
	detect := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	for _, id := range ids {
		capacity, err := destination.CapacityCount()
		if err != nil {
			return State{}, BankItemResult{}, err
		}
		if capacity >= 150 {
			break
		}
		item, ok := source.Items[id]
		if !ok || !bankVisible(item.Object, detect) || (flag(item.Object.Flags[:], objectEventFlag) && !flag(item.Object.Flags[:], objectOneWevFlag)) {
			continue
		}
		rootWeight, err := source.objectWeight(id)
		if err != nil {
			return State{}, BankItemResult{}, err
		}
		carried, err := destination.Weight()
		if err != nil {
			return State{}, BankItemResult{}, err
		}
		if carried+rootWeight > maxPlayerWeight(p.Body) {
			continue
		}
		plan, err := TransferItemRoots(source, destination, []string{id})
		if err != nil {
			return State{}, BankItemResult{}, err
		}
		source, destination = plan.Source, plan.Destination
		count++
		names = append(names, item.Object.Name)
	}
	if count == 0 {
		return s.clone(), BankItemResult{Action: "bank-withdraw-all"}, nil
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &destination
	next.Players[actorID] = nextPlayer
	if next.BankAccounts == nil {
		next.BankAccounts = map[string]BankAccount{}
	}
	next.BankAccounts[actorID] = BankAccount{Balance: account.Balance, Items: &source}
	if err := next.Validate(); err != nil {
		return State{}, BankItemResult{}, err
	}
	return next, BankItemResult{ItemName: strings.Join(names, ", "), Action: "bank-withdraw-all", Count: count}, nil
}
