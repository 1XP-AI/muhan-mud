package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// These limits are the bounded fields used by src/alias.c. The canonical Go
// state keeps UTF-8 strings, but measures the same wire-facing byte budgets.
const (
	MaxPlayerAliases      = 50
	MaxAliasCount         = MaxPlayerAliases
	MaxAliasNameBytes     = 14
	MaxAliasBytes         = MaxAliasNameBytes
	MaxAliasProcessBytes  = 70
	MaxAliasProcessLength = MaxAliasProcessBytes
)

var (
	ErrAliasActorAbsent             = errors.New("online alias actor absent")
	ErrAliasNameRequired            = errors.New("alias name is required")
	ErrAliasNameTooLong             = errors.New("alias name exceeds the byte limit")
	ErrAliasProcessRequired         = errors.New("alias process is required")
	ErrAliasProcessTooLong          = errors.New("alias process exceeds the byte limit")
	ErrAliasForbidden               = errors.New("alias contains a forbidden sentinel")
	ErrAliasCommandSubstitution     = errors.New("alias command substitution is unsupported")
	ErrAliasSubstitutionUnsupported = ErrAliasCommandSubstitution
	ErrAliasDuplicate               = errors.New("alias is already configured")
	ErrAliasLimit                   = errors.New("alias limit reached")
	ErrAliasMissing                 = errors.New("alias is not configured")
	ErrAliasStaleProposal           = errors.New("stale alias proposal")
	ErrAliasInvalidAction           = errors.New("invalid alias action")
	ErrAliasInvalidEntry            = errors.New("invalid alias entry")
)

// PlayerAlias is one ordered alias.c entry. Process expansion deliberately
// remains outside this bounded slice; persisted entries are data, not a
// server-side command execution template.
type PlayerAlias struct {
	Alias   string `json:"alias"`
	Process string `json:"process"`
}

// AliasEntry is a descriptive compatibility spelling for callers that model
// the persisted row rather than the legacy command name.
type AliasEntry = PlayerAlias

type AliasAction string

const (
	AliasView   AliasAction = "view"
	AliasList   AliasAction = AliasView
	AliasAdd    AliasAction = "add"
	AliasDelete AliasAction = "delete"
)

// AliasResult is the deterministic actor response stored in an ExecuteGame
// receipt. Aliases is always returned in canonical insertion order.
type AliasResult struct {
	Action          AliasAction   `json:"action"`
	ActorID         string        `json:"actor_id"`
	Alias           string        `json:"alias,omitempty"`
	Process         string        `json:"process,omitempty"`
	PreviousProcess string        `json:"previous_process,omitempty"`
	Aliases         []PlayerAlias `json:"aliases,omitempty"`
	Changed         bool          `json:"changed"`
	Response        string        `json:"response"`
}

// AliasProposal is bound to one actor snapshot. The private expected values
// ensure a plan cannot overwrite aliases added by a later command.
type AliasProposal struct {
	Action  AliasAction
	ActorID string
	Alias   string
	Process string

	expectedBody    LegacyMonster
	expectedOnline  bool
	expectedAliases []PlayerAlias
}

// ValidatePlayerAliasName enforces the fixed 14-byte alias field and rejects
// spaces/control characters that the legacy tokenizer would split or erase.
func ValidatePlayerAliasName(name string) error {
	if name == "" {
		return ErrAliasNameRequired
	}
	if !utf8.ValidString(name) {
		return ErrAliasInvalidEntry
	}
	if len(name) > MaxAliasNameBytes {
		return ErrAliasNameTooLong
	}
	if strings.TrimSpace(name) != name {
		return ErrAliasNameRequired
	}
	if strings.Contains(name, "~!") {
		return ErrAliasForbidden
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || unicode.IsSpace(r) {
			return ErrAliasInvalidEntry
		}
	}
	return nil
}

// ValidateAliasName is the concise spelling used by command adapters.
func ValidateAliasName(name string) error { return ValidatePlayerAliasName(name) }

// ValidatePlayerAliasProcess keeps ordinary ASCII spaces meaningful while
// rejecting terminal/control injection, the on-disk ~! sentinel, and all
// legacy $N/$* command substitution. Substitution is a deliberate
// fail-closed boundary until a command queue contract is implemented.
func ValidatePlayerAliasProcess(process string) error {
	if process == "" {
		return ErrAliasProcessRequired
	}
	if !utf8.ValidString(process) {
		return ErrAliasInvalidEntry
	}
	if len(process) > MaxAliasProcessBytes {
		return ErrAliasProcessTooLong
	}
	if strings.TrimSpace(process) != process {
		return ErrAliasProcessRequired
	}
	if strings.Contains(process, "~!") {
		return ErrAliasForbidden
	}
	if strings.ContainsRune(process, '$') {
		return ErrAliasCommandSubstitution
	}
	for _, r := range process {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrAliasInvalidEntry
		}
		if unicode.IsSpace(r) && r != ' ' {
			return ErrAliasInvalidEntry
		}
	}
	return nil
}

// ValidateAliasProcess is the concise spelling used by command adapters.
func ValidateAliasProcess(process string) error { return ValidatePlayerAliasProcess(process) }

func validatePlayerAlias(entry PlayerAlias) error {
	if err := ValidatePlayerAliasName(entry.Alias); err != nil {
		return err
	}
	return ValidatePlayerAliasProcess(entry.Process)
}

func validatePlayerAliases(aliases []PlayerAlias) error {
	if len(aliases) > MaxPlayerAliases {
		return ErrAliasLimit
	}
	seen := make(map[string]struct{}, len(aliases))
	for _, entry := range aliases {
		if err := validatePlayerAlias(entry); err != nil {
			return err
		}
		if _, ok := seen[entry.Alias]; ok {
			return ErrAliasDuplicate
		}
		seen[entry.Alias] = struct{}{}
	}
	return nil
}

func clonePlayerAliases(aliases []PlayerAlias) []PlayerAlias {
	if aliases == nil {
		return nil
	}
	clone := make([]PlayerAlias, len(aliases))
	copy(clone, aliases)
	return clone
}

func aliasActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, ErrAliasActorAbsent
	}
	return actor, nil
}

func aliasProposal(actor PlayerState, action AliasAction, actorID, alias, process string) AliasProposal {
	return AliasProposal{
		Action:          action,
		ActorID:         actorID,
		Alias:           alias,
		Process:         process,
		expectedBody:    actor.Body,
		expectedOnline:  actor.Online,
		expectedAliases: clonePlayerAliases(actor.Aliases),
	}
}

// PlanAliases creates the read-only bare `줄임말` listing candidate.
func (s State) PlanAliases(actorID string) (AliasProposal, error) {
	actor, err := aliasActor(s, actorID)
	if err != nil {
		return AliasProposal{}, err
	}
	return aliasProposal(actor, AliasView, actorID, "", ""), nil
}

// PlanAddAlias validates and plans one ordered alias insertion.
func (s State) PlanAddAlias(actorID, alias, process string) (AliasProposal, error) {
	actor, err := aliasActor(s, actorID)
	if err != nil {
		return AliasProposal{}, err
	}
	if err := ValidatePlayerAliasName(alias); err != nil {
		return AliasProposal{}, err
	}
	if err := ValidatePlayerAliasProcess(process); err != nil {
		return AliasProposal{}, err
	}
	if len(actor.Aliases) >= MaxPlayerAliases {
		return AliasProposal{}, ErrAliasLimit
	}
	for _, entry := range actor.Aliases {
		if entry.Alias == alias {
			return AliasProposal{}, ErrAliasDuplicate
		}
	}
	return aliasProposal(actor, AliasAdd, actorID, alias, process), nil
}

// PlanDeleteAlias plans the exact alias removal while preserving all other
// entries' order. A missing alias is a rejected command, not an inferred
// substitution or a fuzzy name match.
func (s State) PlanDeleteAlias(actorID, alias string) (AliasProposal, error) {
	actor, err := aliasActor(s, actorID)
	if err != nil {
		return AliasProposal{}, err
	}
	if err := ValidatePlayerAliasName(alias); err != nil {
		return AliasProposal{}, err
	}
	for _, entry := range actor.Aliases {
		if entry.Alias == alias {
			return aliasProposal(actor, AliasDelete, actorID, alias, ""), nil
		}
	}
	return AliasProposal{}, ErrAliasMissing
}

// PlanAlias is the action-oriented adapter used by session command code.
func (s State) PlanAlias(actorID string, action AliasAction, alias, process string) (AliasProposal, error) {
	switch action {
	case AliasView:
		if alias != "" || process != "" {
			return AliasProposal{}, ErrAliasInvalidAction
		}
		return s.PlanAliases(actorID)
	case AliasAdd:
		return s.PlanAddAlias(actorID, alias, process)
	case AliasDelete:
		if process != "" {
			return AliasProposal{}, ErrAliasInvalidAction
		}
		return s.PlanDeleteAlias(actorID, alias)
	default:
		return AliasProposal{}, ErrAliasInvalidAction
	}
}

func aliasListResponse(aliases []PlayerAlias) string {
	if len(aliases) == 0 {
		return "설정된 줄임말이 없습니다.\r\n"
	}
	var b strings.Builder
	b.WriteString("줄임말:\r\n")
	for _, entry := range aliases {
		fmt.Fprintf(&b, "  %-14s: %s\r\n", entry.Alias, entry.Process)
	}
	return b.String()
}

// ApplyAlias applies one state-bound alias candidate without command
// substitution or other side effects. It performs no random work and keeps
// the canonical slice order stable for JSON snapshots and replay receipts.
func (s State) ApplyAlias(proposal AliasProposal) (State, AliasResult, error) {
	actor, err := aliasActor(s, proposal.ActorID)
	if err != nil {
		return State{}, AliasResult{}, err
	}
	if proposal.ActorID == "" || proposal.expectedOnline != actor.Online || !reflect.DeepEqual(proposal.expectedBody, actor.Body) || !reflect.DeepEqual(proposal.expectedAliases, actor.Aliases) {
		return State{}, AliasResult{}, ErrAliasStaleProposal
	}
	if proposal.Action != AliasView && proposal.Action != AliasAdd && proposal.Action != AliasDelete {
		return State{}, AliasResult{}, ErrAliasInvalidAction
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	result := AliasResult{Action: proposal.Action, ActorID: proposal.ActorID, Alias: proposal.Alias, Process: proposal.Process}
	switch proposal.Action {
	case AliasView:
		result.Aliases = clonePlayerAliases(nextActor.Aliases)
		result.Response = aliasListResponse(result.Aliases)
	case AliasAdd:
		if err := ValidatePlayerAliasName(proposal.Alias); err != nil {
			return State{}, AliasResult{}, err
		}
		if err := ValidatePlayerAliasProcess(proposal.Process); err != nil {
			return State{}, AliasResult{}, err
		}
		if len(nextActor.Aliases) >= MaxPlayerAliases {
			return State{}, AliasResult{}, ErrAliasLimit
		}
		for _, entry := range nextActor.Aliases {
			if entry.Alias == proposal.Alias {
				return State{}, AliasResult{}, ErrAliasDuplicate
			}
		}
		nextActor.Aliases = append(clonePlayerAliases(nextActor.Aliases), PlayerAlias{Alias: proposal.Alias, Process: proposal.Process})
		result.Changed = true
		result.Aliases = clonePlayerAliases(nextActor.Aliases)
		result.Response = "줄임말이 설정되었습니다.\r\n"
	case AliasDelete:
		index := -1
		for i, entry := range nextActor.Aliases {
			if entry.Alias == proposal.Alias {
				index = i
				result.PreviousProcess = entry.Process
				break
			}
		}
		if index < 0 {
			return State{}, AliasResult{}, ErrAliasStaleProposal
		}
		aliases := clonePlayerAliases(nextActor.Aliases)
		copy(aliases[index:], aliases[index+1:])
		aliases = aliases[:len(aliases)-1]
		nextActor.Aliases = aliases
		result.Changed = true
		result.Aliases = clonePlayerAliases(nextActor.Aliases)
		result.Response = "줄임말이 삭제되었습니다.\r\n"
	}
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, AliasResult{}, err
	}
	return next, result, nil
}
