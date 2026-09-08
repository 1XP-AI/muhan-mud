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

func parseBankLine(line string) (bankAction, bool) {
	fields := strings.Fields(line)
	if len(fields) == 1 && fields[0] == "잔액" {
		return bankAction{kind: "balance", operation: world.BankBalance}, true
	}
	if len(fields) == 1 && fields[0] == "보관물" {
		return bankAction{kind: "inventory", occurrence: 1}, true
	}
	if len(fields) == 2 && (fields[0] == "보관물" || fields[0] == "받아") {
		if fields[1] == "모두" {
			kind := "withdraw-items"
			if fields[0] == "보관물" {
				kind = "deposit-items"
			}
			return bankAction{kind: kind, name: fields[1], all: true, occurrence: 1}, true
		}
		kind := "withdraw-item"
		if fields[0] == "보관물" {
			kind = "deposit-item"
		}
		return bankAction{kind: kind, name: fields[1], occurrence: 1}, true
	}
	if len(fields) == 3 && (fields[0] == "보관물" || fields[0] == "받아") {
		occurrence, err := strconv.Atoi(fields[2])
		if err != nil || occurrence < 1 || fields[1] == "모두" {
			return bankAction{}, false
		}
		kind := "withdraw-item"
		if fields[0] == "보관물" {
			kind = "deposit-item"
		}
		return bankAction{kind: kind, name: fields[1], occurrence: occurrence}, true
	}
	if len(fields) != 2 || (fields[0] != "입금" && fields[0] != "출금") {
		return bankAction{}, false
	}
	amount, all, err := world.ParseBankAmount(fields[1])
	if err != nil {
		return bankAction{}, false
	}
	action := bankAction{kind: "money", amount: amount, all: all, occurrence: 1}
	if fields[0] == "입금" {
		action.operation = world.BankDeposit
	} else {
		action.operation = world.BankWithdraw
	}
	return action, true
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
