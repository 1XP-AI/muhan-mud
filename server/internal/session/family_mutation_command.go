package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var (
	// ErrUnsupportedFamilyMutationLine is returned before a durable receipt for
	// input outside this bounded membership-command surface.
	ErrUnsupportedFamilyMutationLine = errors.New("line is not an implemented family mutation command")
	// ErrFamilyMutationSelectionRequired marks the original bare
	// `패거리가입` command. Its list/selection/confirmation continuation is
	// connection-local and is deliberately not represented by a world receipt.
	ErrFamilyMutationSelectionRequired = errors.New("family application selection requires a connection-local continuation")
)

// FamilyApplicationSelectionPrompt and FamilyApplicationChoicePrompt are the
// connection-local prompts emitted by the original add_family editor.  The
// family directory itself is rendered by the world catalog so names never
// come from client input.
const (
	FamilyApplicationSelectionPrompt = "\n당신은 어떤 패거리에 가입을 원하십니까?\r\n패거리의 이름을 입력해 주십시요.  "
	FamilyApplicationConfirmPrompt   = "%s에 가입을 하시겠습니까? (예/아니오) "
	FamilyApplicationInvalidChoice   = "\n잘못된 선택입니다.\r\n"
	FamilyApplicationCancelResponse  = "\n가입 신청을 취소합니다."
)

// FamilyMutationCommand is the parser-owned projection of command11.c's
// membership aliases. FamilyName is display input only: the reducer resolves
// it against the immutable server-owned catalog before planning a proposal.
// TargetName identifies the exact target selector for `가입허가`; the world
// reducer resolves it against online canonical identities and never treats it
// as an authorization proof.
type FamilyMutationCommand struct {
	Action     world.FamilyMutationAction
	FamilyName string
	TargetName string
}

type familyMutationLineRequest struct {
	Kind       string                     `json:"kind"`
	Line       string                     `json:"line"`
	Action     world.FamilyMutationAction `json:"action"`
	FamilyName string                     `json:"family_name,omitempty"`
	TargetName string                     `json:"target_name,omitempty"`
}

func validFamilyMutationLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseFamilyMutationLine admits only the direct, confirmed application form
// and the pending-application cancellation form that can be represented by
// the current world State:
//
//	패거리가입 <패거리명>
//	패거리탈퇴
//
// The original C command starts an interactive list/selection/yes flow. The
// bare start is therefore intentionally left outside this parser. `가입허가`
// is identified as a ledger-gated action; the world reducer either proves the
// fee/member authority or returns a fail-closed error without a receipt.
func ParseFamilyMutationLine(line string) (FamilyMutationCommand, bool) {
	if !validFamilyMutationLine(line) {
		return FamilyMutationCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 {
		return FamilyMutationCommand{}, false
	}
	switch tokens[0] {
	case "패거리가입":
		if len(tokens) != 2 || tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
			return FamilyMutationCommand{}, false
		}
		return FamilyMutationCommand{Action: world.FamilyMutationApply, FamilyName: tokens[1]}, true
	case "패거리탈퇴":
		if len(tokens) != 1 {
			return FamilyMutationCommand{}, false
		}
		return FamilyMutationCommand{Action: world.FamilyMutationWithdraw}, true
	case "가입허가":
		// boss_family prompts for a target when no argument is given. That
		// connection-local continuation is outside this bounded adapter; only
		// the explicit target form is admitted so the world reducer can resolve
		// the exact canonical identity and ledger authority.
		if len(tokens) != 2 {
			return FamilyMutationCommand{}, false
		}
		command := FamilyMutationCommand{Action: world.FamilyMutationApprove}
		if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
			return FamilyMutationCommand{}, false
		}
		command.TargetName = tokens[1]
		return command, true
	case "패거리추방":
		if len(tokens) != 2 || tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
			return FamilyMutationCommand{}, false
		}
		return FamilyMutationCommand{Action: world.FamilyMutationExpel, TargetName: tokens[1]}, true
	default:
		return FamilyMutationCommand{}, false
	}
}

func IsFamilyMutationLine(line string) bool {
	_, ok := ParseFamilyMutationLine(line)
	return ok
}

// ParseFamilyMutationStartLine recognizes the bare interactive `패거리가입`
// entry point.  It is intentionally separate from ParseFamilyMutationLine:
// the former owns a connection-local selection/confirmation flow and must not
// create a durable receipt until the user confirms a concrete family.
func ParseFamilyMutationStartLine(line string) bool {
	if !validFamilyMutationLine(line) {
		return false
	}
	return strings.TrimSpace(line) == "패거리가입"
}

func IsFamilyMutationStartLine(line string) bool {
	return ParseFamilyMutationStartLine(line)
}

// Source-oriented parser aliases keep the C function vocabulary available to
// callers without introducing additional command semantics.
func ParseFamilyJoinLine(line string) (FamilyMutationCommand, bool) {
	command, ok := ParseFamilyMutationLine(line)
	return command, ok && command.Action == world.FamilyMutationApply
}

func IsFamilyJoinLine(line string) bool {
	_, ok := ParseFamilyJoinLine(line)
	return ok
}

func ParseFamilyApplicationLine(line string) (FamilyMutationCommand, bool) {
	return ParseFamilyJoinLine(line)
}

func IsFamilyApplicationLine(line string) bool {
	return IsFamilyJoinLine(line)
}

func ParseFamilyWithdrawalLine(line string) (FamilyMutationCommand, bool) {
	command, ok := ParseFamilyMutationLine(line)
	return command, ok && command.Action == world.FamilyMutationWithdraw
}

func IsFamilyWithdrawalLine(line string) bool {
	_, ok := ParseFamilyWithdrawalLine(line)
	return ok
}

func ParseFamilyLeaveLine(line string) (FamilyMutationCommand, bool) {
	return ParseFamilyWithdrawalLine(line)
}

func IsFamilyLeaveLine(line string) bool {
	return IsFamilyWithdrawalLine(line)
}

func ParseFamilyExpulsionLine(line string) (FamilyMutationCommand, bool) {
	command, ok := ParseFamilyMutationLine(line)
	return command, ok && command.Action == world.FamilyMutationExpel
}

func IsFamilyExpulsionLine(line string) bool {
	_, ok := ParseFamilyExpulsionLine(line)
	return ok
}

func unsupportedFamilyMutationStart() error {
	return errors.Join(ErrUnsupportedFamilyMutationLine, ErrFamilyMutationSelectionRequired)
}

// ExecuteFamilyMutationLineWithCatalog connects the admitted family
// application/cancellation proposal to ExecuteGame's actor-bound durable
// receipt. The catalog is a value dependency but owns a map; world planning
// validates and snapshots it before Apply, so a client cannot manufacture a
// numeric family ID or boss identity. Receipt replay happens before this
// reducer and consequently does not re-run membership validation or mutation.
func (o *Ownership) ExecuteFamilyMutationLineWithCatalog(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	trimmed := strings.TrimSpace(line)
	if validFamilyMutationLine(line) && trimmed == "패거리가입" {
		return storage.WorldReceipt{}, unsupportedFamilyMutationStart()
	}
	command, ok := ParseFamilyMutationLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyMutationLine
	}
	request, err := json.Marshal(familyMutationLineRequest{
		Kind: "family-mutation", Line: line, Action: command.Action,
		FamilyName: command.FamilyName, TargetName: command.TargetName,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			if command.Action == world.FamilyMutationApprove {
				return nil, nil, errors.Join(ErrUnsupportedFamilyMutationLine, err)
			}
			return nil, nil, err
		}
		var proposal world.FamilyMutationProposal
		switch command.Action {
		case world.FamilyMutationApply:
			proposal, err = state.PlanFamilyJoinByName(actorID, command.FamilyName, catalog)
		case world.FamilyMutationWithdraw:
			proposal, err = state.PlanFamilyWithdrawal(actorID, catalog)
		case world.FamilyMutationApprove:
			proposal, err = state.PlanFamilyApproval(actorID, command.TargetName, catalog)
		case world.FamilyMutationExpel:
			proposal, err = state.PlanFamilyExpulsion(actorID, command.TargetName, catalog)
		default:
			return nil, nil, ErrUnsupportedFamilyMutationLine
		}
		if err != nil {
			if command.Action == world.FamilyMutationApprove {
				return nil, nil, errors.Join(ErrUnsupportedFamilyMutationLine, err)
			}
			return nil, nil, err
		}
		next, result, err := state.ApplyFamilyMutation(proposal)
		if err != nil {
			return nil, nil, err
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return nextRaw, response, err
	})
}

// ExecuteFamilyMutationLine is the direct value-copy spelling used by most
// session adapters. The WithCatalog name remains available for callers that
// want to make the dependency explicit at call sites.
func (o *Ownership) ExecuteFamilyMutationLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteFamilyMutationLineWithCatalog(ctx, store, worldID, commandID, lease, line, catalog)
}

func (o *Ownership) ExecuteFamilyJoinLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	_, ok := ParseFamilyJoinLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyMutationLine
	}
	return o.ExecuteFamilyMutationLineWithCatalog(ctx, store, worldID, commandID, lease, line, catalog)
}

func (o *Ownership) ExecuteFamilyApplicationLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteFamilyJoinLine(ctx, store, worldID, commandID, lease, line, catalog)
}

func (o *Ownership) ExecuteFamilyWithdrawalLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	_, ok := ParseFamilyWithdrawalLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyMutationLine
	}
	return o.ExecuteFamilyMutationLineWithCatalog(ctx, store, worldID, commandID, lease, line, catalog)
}

func (o *Ownership) ExecuteFamilyLeaveLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteFamilyWithdrawalLine(ctx, store, worldID, commandID, lease, line, catalog)
}

func (o *Ownership) ExecuteFamilyExpulsionLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseFamilyMutationLine(line)
	if !ok || command.Action != world.FamilyMutationExpel {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyMutationLine
	}
	return o.ExecuteFamilyMutationLineWithCatalog(ctx, store, worldID, commandID, lease, line, catalog)
}

func (o *Ownership) ExecuteFamilyExpelLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteFamilyExpulsionLine(ctx, store, worldID, commandID, lease, line, catalog)
}
