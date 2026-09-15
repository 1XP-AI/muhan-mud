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

var ErrUnsupportedForgeLine = errors.New("line is not an implemented forge command")

// ForgeCommand is the parser-facing form of command7.c:forge. C parse()
// takes the last token as the verb; this bounded adapter admits only the
// exact cmdlist-85 alias `제련`. `무기만들기`/newforge stays unknown.
type ForgeCommand struct {
	Alias string
}

// ForgeSelectArmCommand is the parser-facing form of select_arm case 2.
// C inspects only str[0]; this adapter keeps that first-byte rule and
// admits invalid digits so the reducer can re-prompt.
type ForgeSelectArmCommand struct {
	Input  string
	Choice int
}

// ForgeSelectMaterialCommand is the parser-facing form of select_arm case 3.
// C uses low(str[0]) for materials 1-3 and admits invalid first bytes so
// the reducer can re-prompt.
type ForgeSelectMaterialCommand struct {
	Input  string
	Choice int
}

// ForgeSelectQuenchCommand is the parser-facing form of select_arm case 4.
// C uses low(str[0]) for 담금질 1-5 and admits invalid first bytes so the
// reducer can re-prompt.
type ForgeSelectQuenchCommand struct {
	Input  string
	Choice int
}

// ForgeSelectNameCommand is the parser-facing form of select_arm case 5.
// C copies the whole line onto obj_ptr->name after length/paren checks;
// this adapter admits invalid names so the reducer can re-prompt.
type ForgeSelectNameCommand struct {
	Input string
}

// ForgeSelectConfirmCommand is the parser-facing form of select_arm case 6.
// C strncmp(str,"예",2) treats a 예 prefix as yes; any other admitted line
// is cancel. This adapter does not claim the start alias as a newforge.
type ForgeSelectConfirmCommand struct {
	Input string
	Yes   bool
}

type forgeLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
}

type forgeSelectArmRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Choice int    `json:"choice,omitempty"`
}

type forgeSelectMaterialRequest struct {
	Kind     string `json:"kind"`
	Line     string `json:"line"`
	Choice   int    `json:"choice,omitempty"`
	ObjectID int16  `json:"object_id,omitempty"`
}

type forgeSelectQuenchRequest struct {
	Kind        string `json:"kind"`
	Line        string `json:"line"`
	Choice      int    `json:"choice,omitempty"`
	ObjectID    int16  `json:"object_id,omitempty"`
	MaterialSum int32  `json:"material_sum,omitempty"`
}

type forgeSelectNameRequest struct {
	Kind         string `json:"kind"`
	Line         string `json:"line"`
	ObjectID     int16  `json:"object_id,omitempty"`
	MaterialSum  int32  `json:"material_sum,omitempty"`
	QuenchChoice int    `json:"quench_choice,omitempty"`
}

type forgeSelectConfirmRequest struct {
	Kind         string `json:"kind"`
	Line         string `json:"line"`
	ObjectID     int16  `json:"object_id,omitempty"`
	MaterialSum  int32  `json:"material_sum,omitempty"`
	QuenchChoice int    `json:"quench_choice,omitempty"`
	WeaponName   string `json:"weapon_name,omitempty"`
}

func validForgeLine(line string) bool {
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

// ParseForgeLine admits the original Korean alias. Outer terminal
// whitespace is harmless; arguments, prefix forms, and 무기만들기 remain
// outside this slice.
func ParseForgeLine(line string) (ForgeCommand, bool) {
	if !validForgeLine(line) || strings.TrimSpace(line) != "제련" {
		return ForgeCommand{}, false
	}
	return ForgeCommand{Alias: "제련"}, true
}

func IsForgeLine(line string) bool {
	_, ok := ParseForgeLine(line)
	return ok
}

func ParseForgeStartLine(line string) bool { return IsForgeLine(line) }
func IsForgeStartLine(line string) bool    { return IsForgeLine(line) }

// ParseForgeSelectArmLine admits a connection-local select_arm case-2
// answer. The start alias `제련` stays on ExecuteForgeLine; this parser
// does not claim it. `무기만들기` is an invalid digit, not a newforge
// start. Control/invalid UTF-8 is rejected before a receipt.
func ParseForgeSelectArmLine(line string) (ForgeSelectArmCommand, bool) {
	if !validForgeLine(line) || strings.TrimSpace(line) == "제련" {
		return ForgeSelectArmCommand{}, false
	}
	choice := 0
	if line != "" && line[0] >= '1' && line[0] <= '5' {
		choice = int(line[0] - '0')
	}
	return ForgeSelectArmCommand{Input: line, Choice: choice}, true
}

func IsForgeSelectArmLine(line string) bool {
	_, ok := ParseForgeSelectArmLine(line)
	return ok
}

func forgeMaterialChoice(line string) int {
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

// ParseForgeSelectMaterialLine admits a connection-local select_arm case-3
// answer. The start alias `제련` stays on ExecuteForgeLine; this parser
// does not claim it. `무기만들기` is an invalid digit, not a newforge
// start. Control/invalid UTF-8 is rejected before a receipt.
func ParseForgeSelectMaterialLine(line string) (ForgeSelectMaterialCommand, bool) {
	if !validForgeLine(line) || strings.TrimSpace(line) == "제련" {
		return ForgeSelectMaterialCommand{}, false
	}
	return ForgeSelectMaterialCommand{Input: line, Choice: forgeMaterialChoice(line)}, true
}

func IsForgeSelectMaterialLine(line string) bool {
	_, ok := ParseForgeSelectMaterialLine(line)
	return ok
}

func forgeQuenchChoice(line string) int {
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

// ParseForgeSelectQuenchLine admits a connection-local select_arm case-4
// answer. The start alias `제련` stays on ExecuteForgeLine; this parser
// does not claim it. `무기만들기` is an invalid digit, not a newforge
// start. Control/invalid UTF-8 is rejected before a receipt.
func ParseForgeSelectQuenchLine(line string) (ForgeSelectQuenchCommand, bool) {
	if !validForgeLine(line) || strings.TrimSpace(line) == "제련" {
		return ForgeSelectQuenchCommand{}, false
	}
	return ForgeSelectQuenchCommand{Input: line, Choice: forgeQuenchChoice(line)}, true
}

func IsForgeSelectQuenchLine(line string) bool {
	_, ok := ParseForgeSelectQuenchLine(line)
	return ok
}

// ParseForgeSelectNameLine admits a connection-local select_arm case-5
// answer. The start alias `제련` stays on ExecuteForgeLine; this parser
// does not claim it. `무기만들기` is a name candidate, not a newforge
// start. Control/invalid UTF-8 is rejected before a receipt.
func ParseForgeSelectNameLine(line string) (ForgeSelectNameCommand, bool) {
	if !validForgeLine(line) || strings.TrimSpace(line) == "제련" {
		return ForgeSelectNameCommand{}, false
	}
	return ForgeSelectNameCommand{Input: line}, true
}

func IsForgeSelectNameLine(line string) bool {
	_, ok := ParseForgeSelectNameLine(line)
	return ok
}

// ParseForgeSelectConfirmLine admits a connection-local select_arm case-6
// answer, including empty cancel and a 예 prefix. Control/invalid UTF-8 is
// rejected before a receipt. `무기만들기` is cancel, not a newforge start.
func ParseForgeSelectConfirmLine(line string) (ForgeSelectConfirmCommand, bool) {
	if !validForgeLine(line) {
		return ForgeSelectConfirmCommand{}, false
	}
	return ForgeSelectConfirmCommand{Input: line, Yes: strings.HasPrefix(line, "예")}, true
}

func IsForgeSelectConfirmLine(line string) bool {
	_, ok := ParseForgeSelectConfirmLine(line)
	return ok
}

// ExecuteForgeLine starts the select_arm continuation through the durable
// receipt boundary. An RFORGE room commits PREADI for the weapon-type
// prompt and clears PNOBRD after that case-1 print, matching C. A missing
// room bit is a typed no-op. Replay of the same command ID does not
// re-commit or re-prompt.
func (o *Ownership) ExecuteForgeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseForgeLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedForgeLine
	}
	payload, err := json.Marshal(forgeLineRequest{Kind: "forge", Line: line, Alias: command.Alias})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanForge(actorID)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyForge(proposal)
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

// ExecuteForgeSelectArmLine applies select_arm case 2 through the durable
// receipt boundary. Digits 1-5 load object 900-904 and print the material
// prompt; any other admitted input re-prompts without charging gold.
// Unmigrated catalog 900-904 fails closed before commit. Replay of the
// same command ID does not re-commit.
func (o *Ownership) ExecuteForgeSelectArmLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseForgeSelectArmLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedForgeLine
	}
	payload, err := json.Marshal(forgeSelectArmRequest{Kind: "forge-select-arm", Line: line, Choice: command.Choice})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanForgeSelectArm(actorID, command.Input, catalog)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyForgeSelectArm(proposal, catalog)
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

// ExecuteForgeSelectMaterialLine applies select_arm case 3 through the
// durable receipt boundary after ExecuteForgeSelectArmLine has loaded
// object 900-904. Digits 1-3 set sdice/sum (강철/귀금속/금강석) without
// charging gold; any other admitted input re-prompts. Cleric/Paladin/Mage
// cannot pick 금강석. Unmigrated gold/object graph fails closed before
// commit. Replay of the same command ID does not re-commit.
func (o *Ownership) ExecuteForgeSelectMaterialLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16) (storage.WorldReceipt, error) {
	command, ok := ParseForgeSelectMaterialLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedForgeLine
	}
	payload, err := json.Marshal(forgeSelectMaterialRequest{
		Kind: "forge-select-material", Line: line, Choice: command.Choice, ObjectID: objectID,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanForgeSelectMaterial(actorID, command.Input, catalog, objectID)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyForgeSelectMaterial(proposal, catalog)
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

// ExecuteForgeSelectQuenchLine applies select_arm case 4 through the
// durable receipt boundary after ExecuteForgeSelectMaterialLine has set
// sdice/sum. Digits 1-5 set shotsmax/shotscur and add to the running sum
// without charging gold; any other admitted input re-prompts. Unmigrated
// gold/object graph or a non-material forge2 sum fails closed before
// commit. Replay of the same command ID does not re-commit. Case 5 is
// ExecuteForgeSelectNameLine. Case 6 stays fail-closed.
func (o *Ownership) ExecuteForgeSelectQuenchLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16, materialSum int32) (storage.WorldReceipt, error) {
	command, ok := ParseForgeSelectQuenchLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedForgeLine
	}
	payload, err := json.Marshal(forgeSelectQuenchRequest{
		Kind: "forge-select-quench", Line: line, Choice: command.Choice, ObjectID: objectID, MaterialSum: materialSum,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanForgeSelectQuench(actorID, command.Input, catalog, objectID, materialSum)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyForgeSelectQuench(proposal, catalog)
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

// ExecuteForgeSelectNameLine applies select_arm case 5 through the
// durable receipt boundary after ExecuteForgeSelectQuenchLine has set
// shots/sum. A 3-20 byte name without parentheses is copied onto the
// template identity and prints the confirm prompt; invalid names
// re-prompt at param 5. Gold is not charged. Unmigrated gold/object
// graph or quench identity fails closed before commit. Replay of the
// same command ID does not re-commit. Case 6 is ExecuteForgeSelectConfirmLine.
func (o *Ownership) ExecuteForgeSelectNameLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16, materialSum int32, quenchChoice int) (storage.WorldReceipt, error) {
	command, ok := ParseForgeSelectNameLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedForgeLine
	}
	payload, err := json.Marshal(forgeSelectNameRequest{
		Kind: "forge-select-name", Line: line, ObjectID: objectID, MaterialSum: materialSum, QuenchChoice: quenchChoice,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanForgeSelectName(actorID, command.Input, catalog, objectID, materialSum, quenchChoice)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyForgeSelectName(proposal, catalog)
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

// ExecuteForgeSelectConfirmLine applies select_arm case 6 through the
// durable receipt boundary after ExecuteForgeSelectNameLine has set the
// weapon name. A 예 prefix with gold >= sum charges gold and inserts the
// named weapon; insufficient gold and any other admitted line clear
// PREADI without charging. Unmigrated gold/object graph or identity
// fails closed before commit. Replay of the same command ID does not
// re-charge. Item IDs are deterministic descendants of commandID.
func (o *Ownership) ExecuteForgeSelectConfirmLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.SpawnCatalog, objectID int16, materialSum int32, quenchChoice int, weaponName string) (storage.WorldReceipt, error) {
	command, ok := ParseForgeSelectConfirmLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedForgeLine
	}
	payload, err := json.Marshal(forgeSelectConfirmRequest{
		Kind: "forge-select-confirm", Line: line, ObjectID: objectID, MaterialSum: materialSum, QuenchChoice: quenchChoice, WeaponName: weaponName,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	allocate := func() (string, error) {
		return fmt.Sprintf("forge-%s-1", commandID), nil
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanForgeSelectConfirm(actorID, command.Input, catalog, objectID, materialSum, quenchChoice, weaponName, allocate)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyForgeSelectConfirm(proposal, catalog, allocate)
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
