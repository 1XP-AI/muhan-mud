package world

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// RBANK is the legacy room flag from mtype.h. The room flag array keeps
	// source bit numbering rather than packing flags into a Go enum.
	RoomBankFlag = 39
	// The C bank implementation rejects balances above three hundred million.
	MaxBankBalance int64 = 300000000
)

type BankMoneyOperation string

const (
	BankBalance  BankMoneyOperation = "balance"
	BankDeposit  BankMoneyOperation = "deposit"
	BankWithdraw BankMoneyOperation = "withdraw"
)

type BankMoneyResult struct {
	Operation BankMoneyOperation
	Amount    int64
	Balance   int64
	Gold      int64
	Response  string
}

// BankRoom reports whether an online character is physically in an RBANK room.
// The check is derived from the authoritative world snapshot, never from a
// client-provided room number.
func (s State) BankRoom(actorID string) (RoomState, PlayerState, error) {
	if err := s.Validate(); err != nil {
		return RoomState{}, PlayerState{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return RoomState{}, PlayerState{}, fmt.Errorf("character is not online")
	}
	room, ok := s.Rooms[p.Body.RoomID]
	if !ok {
		return RoomState{}, PlayerState{}, fmt.Errorf("character room absent")
	}
	if !flag(room.Resource.Flags[:], RoomBankFlag) {
		return RoomState{}, PlayerState{}, fmt.Errorf("은행에서만 사용할 수 있습니다")
	}
	return room, p, nil
}

// ApplyBankMoney atomically changes player gold and the matching bank account.
// amount is ignored for balance and is resolved from the current side for an
// all operation. The returned state is a candidate; engine.Execute owns the
// revision/receipt commit and retry semantics.
func (s State) ApplyBankMoney(actorID string, operation BankMoneyOperation, amount int64, all bool) (State, BankMoneyResult, error) {
	_, player, err := s.BankRoom(actorID)
	if err != nil {
		return State{}, BankMoneyResult{}, err
	}
	if operation != BankBalance && operation != BankDeposit && operation != BankWithdraw {
		return State{}, BankMoneyResult{}, fmt.Errorf("unknown bank operation")
	}
	if amount < 0 {
		return State{}, BankMoneyResult{}, fmt.Errorf("금액은 음수가 될 수 없습니다")
	}

	account := BankAccount{}
	if s.BankAccounts != nil {
		account = s.BankAccounts[actorID]
	}
	if account.Balance < 0 || account.Balance > MaxBankBalance {
		return State{}, BankMoneyResult{}, fmt.Errorf("은행 잔액이 손상되었습니다")
	}
	if operation == BankBalance {
		return s, BankMoneyResult{Operation: operation, Balance: account.Balance, Gold: int64(player.Body.Gold), Response: fmt.Sprintf("은행 잔액은 %d냥입니다.\r\n", account.Balance)}, nil
	}

	playerGold := int64(player.Body.Gold)
	switch operation {
	case BankDeposit:
		if all {
			amount = playerGold
		}
		if amount < 1 {
			return State{}, BankMoneyResult{}, fmt.Errorf("입금할 금액을 확인해 주세요")
		}
		if amount > playerGold {
			return State{}, BankMoneyResult{}, fmt.Errorf("그만큼의 돈을 가지고 있지 않습니다")
		}
		if account.Balance > MaxBankBalance-amount {
			return State{}, BankMoneyResult{}, fmt.Errorf("은행에는 3억냥까지만 보관할 수 있습니다")
		}
		account.Balance += amount
		playerGold -= amount
	case BankWithdraw:
		if all {
			amount = account.Balance
		}
		if amount < 1 {
			return State{}, BankMoneyResult{}, fmt.Errorf("출금할 금액을 확인해 주세요")
		}
		if amount > account.Balance {
			return State{}, BankMoneyResult{}, fmt.Errorf("은행 잔액보다 많이 출금할 수 없습니다")
		}
		if playerGold > MaxBankBalance-amount {
			return State{}, BankMoneyResult{}, fmt.Errorf("소지금은 3억냥까지만 보관할 수 있습니다")
		}
		account.Balance -= amount
		playerGold += amount
	}
	if playerGold < 0 || playerGold > MaxBankBalance || playerGold > int64(^uint32(0)>>1) {
		return State{}, BankMoneyResult{}, fmt.Errorf("소지금 범위를 벗어났습니다")
	}

	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Body.Gold = int32(playerGold)
	next.Players[actorID] = nextPlayer
	if next.BankAccounts == nil {
		next.BankAccounts = map[string]BankAccount{}
	}
	if account.Items == nil {
		// Money-only commands do not silently claim that the bank object graph
		// has been imported. Keep nil as the explicit not-yet-migrated marker.
		account.Items = next.BankAccounts[actorID].Items
	}
	next.BankAccounts[actorID] = account
	result := BankMoneyResult{Operation: operation, Amount: amount, Balance: account.Balance, Gold: playerGold}
	if operation == BankDeposit {
		result.Response = fmt.Sprintf("은행에 %d냥을 입금했습니다.\r\n은행 잔액은 %d냥입니다.\r\n", amount, account.Balance)
	} else {
		result.Response = fmt.Sprintf("은행에서 %d냥을 출금했습니다.\r\n은행 잔액은 %d냥입니다.\r\n", amount, account.Balance)
	}
	return next, result, nil
}

// ParseBankAmount accepts the source's decimal amount followed by the Korean
// currency suffix, while also accepting a bare decimal for terminal clients.
func ParseBankAmount(token string) (amount int64, all bool, err error) {
	token = strings.TrimSpace(token)
	if token == "모두" {
		return 0, true, nil
	}
	if strings.HasSuffix(token, "냥") {
		token = strings.TrimSuffix(token, "냥")
	}
	if token == "" {
		return 0, false, fmt.Errorf("금액이 비어 있습니다")
	}
	amount, err = strconv.ParseInt(token, 10, 64)
	if err != nil || amount < 1 {
		return 0, false, fmt.Errorf("금액을 확인해 주세요")
	}
	return amount, false, nil
}
