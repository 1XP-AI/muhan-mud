package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedBankLine = errors.New("line is not an implemented bank money command")

type bankAction struct {
	kind       string
	operation  world.BankMoneyOperation
	name       string
	occurrence int
	amount     int64
	all        bool
}

func isBankVerb(token string) bool {
	switch token {
	case "잔액", "보관물", "받아", "입금", "출금":
		return true
	default:
		return false
	}
}

// parseBankLine admits prefix `입금 250냥` / `보관물 검` and C parse() last-token
// `250냥 입금` / `모두 출금` / `검 보관물` / `검 받아`. Last-token verbs win when
// both ends are bank aliases. Prefix extras stay fail-closed.
func parseBankLine(line string) (bankAction, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return bankAction{}, false
	}
	verb := fields[0]
	rest := fields[1:]
	if isBankVerb(fields[len(fields)-1]) {
		verb = fields[len(fields)-1]
		rest = fields[:len(fields)-1]
	} else if !isBankVerb(verb) {
		return bankAction{}, false
	}
	return parseBankVerbRest(verb, rest)
}

func parseBankVerbRest(verb string, rest []string) (bankAction, bool) {
	if verb == "잔액" {
		if len(rest) != 0 {
			return bankAction{}, false
		}
		return bankAction{kind: "balance", operation: world.BankBalance}, true
	}
	if verb == "보관물" && len(rest) == 0 {
		return bankAction{kind: "inventory", occurrence: 1}, true
	}
	if len(rest) == 1 && (verb == "보관물" || verb == "받아") {
		if rest[0] == "모두" {
			kind := "withdraw-items"
			if verb == "보관물" {
				kind = "deposit-items"
			}
			return bankAction{kind: kind, name: rest[0], all: true, occurrence: 1}, true
		}
		kind := "withdraw-item"
		if verb == "보관물" {
			kind = "deposit-item"
		}
		return bankAction{kind: kind, name: rest[0], occurrence: 1}, true
	}
	if len(rest) == 2 && (verb == "보관물" || verb == "받아") {
		occurrence, err := strconv.Atoi(rest[1])
		if err != nil || occurrence < 1 || rest[0] == "모두" {
			return bankAction{}, false
		}
		kind := "withdraw-item"
		if verb == "보관물" {
			kind = "deposit-item"
		}
		return bankAction{kind: kind, name: rest[0], occurrence: occurrence}, true
	}
	if len(rest) != 1 || (verb != "입금" && verb != "출금") {
		return bankAction{}, false
	}
	amount, all, err := world.ParseBankAmount(rest[0])
	if err != nil {
		return bankAction{}, false
	}
	action := bankAction{kind: "money", amount: amount, all: all, occurrence: 1}
	if verb == "입금" {
		action.operation = world.BankDeposit
	} else {
		action.operation = world.BankWithdraw
	}
	return action, true
}

func IsBankLine(line string) bool {
	_, ok := parseBankLine(line)
	return ok
}

// ExecuteBankLine owns bank money and canonical item-graph commands. Every
// balance or object move shares the same world receipt/replay boundary.
func (o *Ownership) ExecuteBankLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	action, ok := parseBankLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedBankLine
	}
	payload, err := json.Marshal(struct {
		Line       string                   `json:"line"`
		Kind       string                   `json:"kind"`
		Operation  world.BankMoneyOperation `json:"operation"`
		Name       string                   `json:"name"`
		Occurrence int                      `json:"occurrence"`
		Amount     int64                    `json:"amount"`
		All        bool                     `json:"all"`
	}{line, action.kind, action.operation, action.name, action.occurrence, action.amount, action.all})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		state := raw
		var responseText string
		switch action.kind {
		case "balance", "money":
			next, result, applyErr := s.ApplyBankMoney(actorID, action.operation, action.amount, action.all)
			if applyErr != nil {
				return nil, nil, applyErr
			}
			responseText = result.Response
			if action.operation != world.BankBalance {
				state, err = json.Marshal(next)
				if err != nil {
					return nil, nil, err
				}
			}
		case "inventory":
			responseText, err = s.BankInventory(actorID)
		case "deposit-item":
			next, result, applyErr := s.DepositBankItem(actorID, action.name, action.occurrence)
			if applyErr != nil {
				return nil, nil, applyErr
			}
			state, err = json.Marshal(next)
			responseText = fmt.Sprintf("%s을(를) 은행에 보관했습니다.\r\n", result.ItemName)
		case "deposit-items":
			next, result, applyErr := s.DepositAllBankItems(actorID)
			if applyErr != nil {
				return nil, nil, applyErr
			}
			state, err = json.Marshal(next)
			if result.Count == 0 {
				responseText = "은행에 보관할 수 있는 물건이 없습니다.\r\n"
			} else {
				responseText = fmt.Sprintf("%s을(를) 은행에 보관했습니다.\r\n", result.ItemName)
			}
		case "withdraw-item":
			next, result, applyErr := s.WithdrawBankItem(actorID, action.name, action.occurrence)
			if applyErr != nil {
				return nil, nil, applyErr
			}
			state, err = json.Marshal(next)
			responseText = fmt.Sprintf("%s을(를) 은행에서 받았습니다.\r\n", result.ItemName)
		case "withdraw-items":
			next, result, applyErr := s.WithdrawAllBankItems(actorID)
			if applyErr != nil {
				return nil, nil, applyErr
			}
			state, err = json.Marshal(next)
			if result.Count == 0 {
				responseText = "은행에서 받을 수 있는 물건이 없습니다.\r\n"
			} else {
				responseText = fmt.Sprintf("%s을(를) 은행에서 받았습니다.\r\n", result.ItemName)
			}
		default:
			return nil, nil, fmt.Errorf("unknown bank action")
		}
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(responseText)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal bank response: %w", err)
		}
		return state, response, nil
	})
}
