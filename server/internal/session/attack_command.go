package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedAttackLine = errors.New("line is not an implemented NPC attack command")

// AttackOptions are host-owned values used only when a swing kills an NPC.
// They are never accepted from the terminal line and are reused on command
// retry through the durable receipt rather than rerolling the attack.
type AttackOptions struct {
	Now      int32
	Allocate func() (string, error)
}

func attackTarget(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return "", false
	}
	switch fields[0] {
	case "공격", "공", "쳐", "때려":
		return fields[1], true
	default:
		return "", false
	}
}

// ExecuteAttackLine is the durable command boundary for the first player→NPC
// attack slice. Target identity, equipment, hit/damage RNG and enemy relation
// are derived from the committed world snapshot. The compatibility form uses
// no host allocator, so a lethal NPC with drops is rejected atomically.
func (o *Ownership) ExecuteAttackLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteAttackLineWithOptions(ctx, store, worldID, commandID, lease, line, roll, AttackOptions{})
}

// ExecuteAttackLineWithOptions supplies the world clock and canonical item ID
// allocator for a lethal NPC transition. Existing command IDs and receipts
// still fence retries before this callback is evaluated again.
func (o *Ownership) ExecuteAttackLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, roll func(int, int) int, options AttackOptions) (storage.WorldReceipt, error) {
	target, ok := attackTarget(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedAttackLine
	}
	payload, err := json.Marshal(struct{ Line string }{line})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		targetID, err := s.SelectNPCInRoom(actorID, target)
		if err != nil {
			// A rejected target still receives a durable no-op receipt, so a lost
			// response cannot cause a later retry to select a different NPC.
			response, marshalErr := json.Marshal("그런 몬스터는 여기 없습니다.\r\n")
			return raw, response, marshalErr
		}
		next, result, err := s.PlanNPCMeleeAttackWithOptions(actorID, targetID, roll, world.NPCMeleeAttackOptions{Now: options.Now, Allocate: options.Allocate})
		if errors.Is(err, world.ErrNPCDeathTransitionPending) {
			response, marshalErr := json.Marshal("현재 그 공격의 사망 처리는 아직 준비되지 않았습니다.\r\n")
			return raw, response, marshalErr
		}
		if err != nil {
			return nil, nil, err
		}
		weaponName := result.WeaponName
		if weaponName == "" {
			weaponName = "무기"
		}
		text := ""
		if result.WeaponBroken {
			if result.Hit {
				text += fmt.Sprintf("\n%s가 산산히 부서집니다.\r\n", weaponName)
			} else {
				text += fmt.Sprintf("\n%s가 부서져 버렸습니다.\r\n", weaponName)
			}
		}
		if result.WeaponDropped {
			text += fmt.Sprintf("\n당신은 %s을(를) 떨어뜨렸습니다.\r\n", weaponName)
		}
		if result.Killed {
			text += fmt.Sprintf("\n당신은 %s를 죽였습니다.\r\n", result.TargetName)
			if result.ExperienceAward > 0 {
				text += fmt.Sprintf("당신은 경험치 %d를 받았습니다.\r\n", result.ExperienceAward)
			}
			if result.QuestExperienceAward > 0 {
				text += fmt.Sprintf("임무 경험치 %d를 받았습니다.\r\n", result.QuestExperienceAward)
			}
		} else if !result.Hit {
			if text == "" {
				text = "\n허공을 쳤습니다.\r\n"
			}
		} else {
			text += fmt.Sprintf("\n당신은 %s에게 %d 만큼의 피해를 주었습니다.\r\n", result.TargetName, result.Damage)
		}
		state, marshalErr := json.Marshal(next)
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		response, marshalErr := json.Marshal(text)
		return state, response, marshalErr
	})
}
