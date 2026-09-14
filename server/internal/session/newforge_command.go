package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedNewForgeLine = errors.New("line is not an implemented newforge command")

// NewForgeCommand is the parser-facing form of command7.c:newforge. C
// parse() takes the last token as the verb; this bounded adapter admits
// only the exact cmdlist-85 alias `무기만들기`. Distinct from `제련`/forge.
type NewForgeCommand struct {
	Alias string
}

// NewForgeSelectArmCommand is the parser-facing form of select_newarm case 2.
// C inspects only str[0]; this adapter keeps that first-byte rule and
// admits invalid digits so the reducer can re-prompt.
type NewForgeSelectArmCommand struct {
	Input  string
	Choice int
}

// NewForgeSelectMaterialCommand is the parser-facing form of select_newarm
// case 3. C uses low(str[0]) for 에메랄드/티타늄/일루션 1-3 and admits
// invalid first bytes so the reducer can re-prompt. Distinct from 제련
// 강철/귀금속/금강석.
type NewForgeSelectMaterialCommand struct {
	Input  string
	Choice int
}

// NewForgeSelectQuenchCommand is the parser-facing form of select_newarm
// case 4. C uses low(str[0]) for 담금질 1-5 and admits invalid first bytes
// so the reducer can re-prompt. Distinct from 제련 오만냥/이십만냥 costs.
type NewForgeSelectQuenchCommand struct {
	Input  string
	Choice int
}

// NewForgeSelectNameCommand is the parser-facing form of select_newarm
// case 5. C copies the whole line onto obj_ptr->name after length/paren
// checks; this adapter admits invalid names so the reducer can re-prompt.
// Distinct from 제련 select_arm case 5.
type NewForgeSelectNameCommand struct {
	Input string
}

// NewForgeSelectConfirmCommand is the parser-facing form of select_newarm
// case 6. C strncmp(str,"예",2) treats a 예 prefix as yes; any other
// admitted line is cancel. Distinct from 제련 select_arm case 6.
type NewForgeSelectConfirmCommand struct {
	Input string
	Yes   bool
}

type newForgeLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
}

type newForgeSelectArmRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Choice int    `json:"choice,omitempty"`
}

type newForgeSelectMaterialRequest struct {
	Kind     string `json:"kind"`
	Line     string `json:"line"`
	Choice   int    `json:"choice,omitempty"`
	ObjectID int16  `json:"object_id,omitempty"`
}

type newForgeSelectQuenchRequest struct {
	Kind        string `json:"kind"`
	Line        string `json:"line"`
	Choice      int    `json:"choice,omitempty"`
	ObjectID    int16  `json:"object_id,omitempty"`
	MaterialSum int32  `json:"material_sum,omitempty"`
}

type newForgeSelectNameRequest struct {
	Kind         string `json:"kind"`
	Line         string `json:"line"`
	ObjectID     int16  `json:"object_id,omitempty"`
	MaterialSum  int32  `json:"material_sum,omitempty"`
	QuenchChoice int    `json:"quench_choice,omitempty"`
}

type newForgeSelectConfirmRequest struct {
	Kind         string `json:"kind"`
	Line         string `json:"line"`
	ObjectID     int16  `json:"object_id,omitempty"`
	MaterialSum  int32  `json:"material_sum,omitempty"`
	QuenchChoice int    `json:"quench_choice,omitempty"`
	WeaponName   string `json:"weapon_name,omitempty"`
}

func validNewForgeLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

// ParseNewForgeLine admits the original Korean alias. Outer terminal
// whitespace is harmless; arguments, prefix forms, and 제련 remain
// outside this slice.
func ParseNewForgeLine(line string) (NewForgeCommand, bool) {
	if !validNewForgeLine(line) || strings.TrimSpace(line) != "무기만들기" {
		return NewForgeCommand{}, false
	}
	return NewForgeCommand{Alias: "무기만들기"}, true
}

func IsNewForgeLine(line string) bool {
	_, ok := ParseNewForgeLine(line)
	return ok
}

// ParseNewForgeSelectArmLine admits a connection-local select_newarm case-2
// answer. The start alias `무기만들기` stays on ExecuteNewForgeLine; this
// parser does not claim it. `제련` is an invalid digit, not a forge start.
// Control/invalid UTF-8 is rejected before a receipt.
func ParseNewForgeSelectArmLine(line string) (NewForgeSelectArmCommand, bool) {
	if !validNewForgeLine(line) || strings.TrimSpace(line) == "무기만들기" {
		return NewForgeSelectArmCommand{}, false
	}
	choice := 0
	if line != "" && line[0] >= '1' && line[0] <= '5' {
		choice = int(line[0] - '0')
	}
	return NewForgeSelectArmCommand{Input: line, Choice: choice}, true
}

func IsNewForgeSelectArmLine(line string) bool {
	_, ok := ParseNewForgeSelectArmLine(line)
	return ok
}

func newForgeMaterialChoice(line string) int {
	if line == "" {
		return 0
	}
	ch := line[0]
	if ch >= 'A' && ch <= 'Z' {
		ch += 'a' - 'A'
	}
	if ch < '1' || ch > '3' {
		return 0
	}
	return int(ch - '0')
}

// ParseNewForgeSelectMaterialLine admits a connection-local select_newarm
// case-3 answer. The start alias `무기만들기` stays on ExecuteNewForgeLine;
// this parser does not claim it. `제련` is an invalid digit, not a forge
// start. Control/invalid UTF-8 is rejected before a receipt.
func ParseNewForgeSelectMaterialLine(line string) (NewForgeSelectMaterialCommand, bool) {
	if !validNewForgeLine(line) || strings.TrimSpace(line) == "무기만들기" {
		return NewForgeSelectMaterialCommand{}, false
	}
	return NewForgeSelectMaterialCommand{Input: line, Choice: newForgeMaterialChoice(line)}, true
}

func IsNewForgeSelectMaterialLine(line string) bool {
	_, ok := ParseNewForgeSelectMaterialLine(line)
	return ok
}

func newForgeQuenchChoice(line string) int {
	if line == "" {
		return 0
	}
	ch := line[0]
	if ch >= 'A' && ch <= 'Z' {
		ch += 'a' - 'A'
	}
	if ch < '1' || ch > '5' {
		return 0
	}
	return int(ch - '0')
}

// ParseNewForgeSelectQuenchLine admits a connection-local select_newarm
// case-4 answer. The start alias `무기만들기` stays on ExecuteNewForgeLine;
// this parser does not claim it. `제련` is an invalid digit, not a forge
// start. Control/invalid UTF-8 is rejected before a receipt.
func ParseNewForgeSelectQuenchLine(line string) (NewForgeSelectQuenchCommand, bool) {
	if !validNewForgeLine(line) || strings.TrimSpace(line) == "무기만들기" {
		return NewForgeSelectQuenchCommand{}, false
	}
	return NewForgeSelectQuenchCommand{Input: line, Choice: newForgeQuenchChoice(line)}, true
}

func IsNewForgeSelectQuenchLine(line string) bool {
	_, ok := ParseNewForgeSelectQuenchLine(line)
	return ok
}

// ParseNewForgeSelectNameLine admits a connection-local select_newarm case-5
// answer. The start alias `무기만들기` stays on ExecuteNewForgeLine; this
// parser does not claim it. `제련` is a name candidate, not a forge start.
// Control/invalid UTF-8 is rejected before a receipt.
func ParseNewForgeSelectNameLine(line string) (NewForgeSelectNameCommand, bool) {
	if !validNewForgeLine(line) || strings.TrimSpace(line) == "무기만들기" {
		return NewForgeSelectNameCommand{}, false
	}
	return NewForgeSelectNameCommand{Input: line}, true
}

func IsNewForgeSelectNameLine(line string) bool {
	_, ok := ParseNewForgeSelectNameLine(line)
	return ok
}

// ParseNewForgeSelectConfirmLine admits a connection-local select_newarm
// case-6 answer, including empty cancel and a 예 prefix. Control/invalid
// UTF-8 is rejected before a receipt. `제련` is cancel, not a forge start.
func ParseNewForgeSelectConfirmLine(line string) (NewForgeSelectConfirmCommand, bool) {
	if !validNewForgeLine(line) {
		return NewForgeSelectConfirmCommand{}, false
	}
	return NewForgeSelectConfirmCommand{Input: line, Yes: strings.HasPrefix(line, "예")}, true
}

func IsNewForgeSelectConfirmLine(line string) bool {
	_, ok := ParseNewForgeSelectConfirmLine(line)
	return ok
}

// ExecuteNewForgeLine starts the select_newarm continuation through the
// durable receipt boundary. An RFORGE room 611 commits PREADI for the
// weapon-type prompt and clears PNOBRD after that case-1 print, matching C.
// A missing room bit or a non-611 RFORGE room is a typed no-op. Replay of
// the same command ID does not re-commit or re-prompt. Case 2 is
// ExecuteNewForgeSelectArmLine. Case 3 is ExecuteNewForgeSelectMaterialLine.
// Case 4 is ExecuteNewForgeSelectQuenchLine. Case 5 is
// ExecuteNewForgeSelectNameLine. Case 6 is ExecuteNewForgeSelectConfirmLine.
func (o *Ownership) ExecuteNewForgeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseNewForgeLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNewForgeLine
	}
	payload, err := json.Marshal(newForgeLineRequest{Kind: "newforge", Line: line, Alias: command.Alias})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNewForge(actorID)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNewForge(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

// ExecuteNewForgeSelectArmLine applies select_newarm case 2 through the
// durable receipt boundary. Digits 1-5 load object 900-904 and print the
// newforge material prompt; any other admitted input re-prompts without
// charging gold. Unmigrated catalog 900-904 fails closed before commit.
// Replay of the same command ID does not re-commit. Case 3 is
// ExecuteNewForgeSelectMaterialLine. Case 4 is ExecuteNewForgeSelectQuenchLine.
// Case 5 is ExecuteNewForgeSelectNameLine. Case 6 is ExecuteNewForgeSelectConfirmLine.
func (o *Ownership) ExecuteNewForgeSelectArmLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseNewForgeSelectArmLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNewForgeLine
	}
	payload, err := json.Marshal(newForgeSelectArmRequest{Kind: "newforge-select-arm", Line: line, Choice: command.Choice})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNewForgeSelectArm(actorID, command.Input, catalog)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNewForgeSelectArm(proposal, catalog)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

// ExecuteNewForgeSelectMaterialLine applies select_newarm case 3 through
// the durable receipt boundary after ExecuteNewForgeSelectArmLine has
// loaded object 900-904. Digits 1-3 set OENCHA/ndice/sdice/pdice and the
// forge2 sum (에메랄드/티타늄/일루션) without charging gold; any other
// admitted input re-prompts. Cleric/Paladin/Mage may pick 일루션.
// Unmigrated catalog 900-904 fails closed before commit. Replay of the
// same command ID does not re-commit. Case 4 is ExecuteNewForgeSelectQuenchLine.
// Case 5 is ExecuteNewForgeSelectNameLine. Case 6 is ExecuteNewForgeSelectConfirmLine.
func (o *Ownership) ExecuteNewForgeSelectMaterialLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16) (storage.WorldReceipt, error) {
	command, ok := ParseNewForgeSelectMaterialLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNewForgeLine
	}
	payload, err := json.Marshal(newForgeSelectMaterialRequest{
		Kind: "newforge-select-material", Line: line, Choice: command.Choice, ObjectID: objectID,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNewForgeSelectMaterial(actorID, command.Input, catalog, objectID)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNewForgeSelectMaterial(proposal, catalog)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

// ExecuteNewForgeSelectQuenchLine applies select_newarm case 4 through
// the durable receipt boundary after ExecuteNewForgeSelectMaterialLine has
// set OENCHA/dice and the forge2 material sum. Digits 1-5 set
// shotsmax/shotscur (100/200/300/400/500, not the printed 100/300/500/700/900)
// and add 100만/200만/300만/400만/500만 to forge2 without charging gold;
// any other admitted input re-prompts. Unmigrated catalog 900-904 or a
// non-newforge forge2 sum fails closed before commit. Replay of the same
// command ID does not re-commit. Case 5 is ExecuteNewForgeSelectNameLine.
// Case 6 is ExecuteNewForgeSelectConfirmLine.
func (o *Ownership) ExecuteNewForgeSelectQuenchLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16, materialSum int32) (storage.WorldReceipt, error) {
	command, ok := ParseNewForgeSelectQuenchLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNewForgeLine
	}
	payload, err := json.Marshal(newForgeSelectQuenchRequest{
		Kind: "newforge-select-quench", Line: line, Choice: command.Choice, ObjectID: objectID, MaterialSum: materialSum,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNewForgeSelectQuench(actorID, command.Input, catalog, objectID, materialSum)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNewForgeSelectQuench(proposal, catalog)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

// ExecuteNewForgeSelectNameLine applies select_newarm case 5 through the
// durable receipt boundary after ExecuteNewForgeSelectQuenchLine has set
// shots/sum. A 3-20 byte name without parentheses is copied onto the
// template identity and prints the confirm prompt; invalid names
// re-prompt at param 5. Gold is not charged. Unmigrated catalog 900-904
// or a non-newforge forge2/quench identity fails closed before commit.
// Replay of the same command ID does not re-commit. Case 6 is
// ExecuteNewForgeSelectConfirmLine.
func (o *Ownership) ExecuteNewForgeSelectNameLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16, materialSum int32, quenchChoice int) (storage.WorldReceipt, error) {
	command, ok := ParseNewForgeSelectNameLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNewForgeLine
	}
	payload, err := json.Marshal(newForgeSelectNameRequest{
		Kind: "newforge-select-name", Line: line, ObjectID: objectID, MaterialSum: materialSum, QuenchChoice: quenchChoice,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNewForgeSelectName(actorID, command.Input, catalog, objectID, materialSum, quenchChoice)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNewForgeSelectName(proposal, catalog)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

// ExecuteNewForgeSelectConfirmLine applies select_newarm case 6 through the
// durable receipt boundary after ExecuteNewForgeSelectNameLine has set the
// weapon name. A 예 prefix with gold >= sum charges gold and inserts the
// named weapon; insufficient gold and any other admitted line clear
// PREADI without charging. Unmigrated gold/object graph or identity
// fails closed before commit. Replay of the same command ID does not
// re-charge. Item IDs are deterministic descendants of commandID.
// Distinct from ExecuteForgeSelectConfirmLine.
func (o *Ownership) ExecuteNewForgeSelectConfirmLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16, materialSum int32, quenchChoice int, weaponName string) (storage.WorldReceipt, error) {
	command, ok := ParseNewForgeSelectConfirmLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNewForgeLine
	}
	payload, err := json.Marshal(newForgeSelectConfirmRequest{
		Kind: "newforge-select-confirm", Line: line, ObjectID: objectID, MaterialSum: materialSum, QuenchChoice: quenchChoice, WeaponName: weaponName,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	allocate := func() (string, error) {
		return fmt.Sprintf("newforge-%s-1", commandID), nil
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNewForgeSelectConfirm(actorID, command.Input, catalog, objectID, materialSum, quenchChoice, weaponName, allocate)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNewForgeSelectConfirm(proposal, catalog, allocate)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}
