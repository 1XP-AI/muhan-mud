package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// The legacy memo handler appends one short line to player/fal/<name>.  The
// Go authority keeps the same ordered, append-only meaning, but stores a
// pointer-free record under the canonical recipient ID.  In particular, a
// raw player path and all credential fields are intentionally absent.
const (
	MaxMemoIDBytes               = 128
	MaxMemoBodyBytes             = 80
	MaxMemoRecipients            = 100000
	MaxMemoRecords               = 1000000
	MemoResponse                 = "메모를 남겼습니다."
	MemoAppend        MemoAction = "append"
)

// Compatibility names keep the source operation discoverable without
// creating a second state or receipt representation.
const (
	CharacterMemoAppend = MemoAppend
)

var (
	ErrMemoStateUnresolved        = errors.New("memo state migration is incomplete")
	ErrMemoActorAbsent            = errors.New("online canonical memo actor absent")
	ErrMemoTargetRequired         = errors.New("memo target is required")
	ErrMemoTargetUnavailable      = errors.New("memo target is unavailable")
	ErrMemoTargetAmbiguous        = errors.New("memo target identity is ambiguous")
	ErrMemoTargetOffline          = errors.New("memo target offline policy violation")
	ErrMemoBodyEmpty              = errors.New("memo body is empty")
	ErrMemoBodyInvalidUTF8        = errors.New("memo body is not valid UTF-8")
	ErrMemoBodyControl            = errors.New("memo body contains a control character")
	ErrMemoBodyTooLong            = errors.New("memo body exceeds the byte limit")
	ErrMemoInvalidID              = errors.New("invalid memo ID")
	ErrMemoInvalidTimestamp       = errors.New("invalid memo timestamp")
	ErrMemoInvalidRecord          = errors.New("invalid memo record")
	ErrMemoLimit                  = errors.New("memo limit exceeded")
	ErrMemoStaleProposal          = errors.New("stale memo proposal")
	ErrMemoInvalidProposal        = errors.New("invalid memo proposal")
	ErrMemoTargetNameNonCanonical = errors.New("memo target name is not canonical")
)

// Source-oriented aliases avoid making adapter callers depend on one naming
// choice for the recipient side of the same canonical identity check.
var (
	ErrMemoRecipientRequired    = ErrMemoTargetRequired
	ErrMemoRecipientUnavailable = ErrMemoTargetUnavailable
	ErrMemoRecipientAmbiguous   = ErrMemoTargetAmbiguous
	ErrMemoTargetAbsent         = ErrMemoTargetUnavailable
	ErrMemoIdentityUnresolved   = ErrMemoTargetUnavailable
)

type MemoAction string

// CharacterMemo is the canonical equivalent of one legacy fal append.  The
// sender identity is durable and canonical; SenderName is a display snapshot
// used for deterministic projections and is never accepted from the client.
type CharacterMemo struct {
	ID         string    `json:"id"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// Memo is a concise compatibility spelling for CharacterMemo.
type Memo = CharacterMemo

// MemoResult is the actor-facing durable receipt projection.  No transport
// event is emitted: the C command only appends the recipient's durable memo
// file, including when that recipient is offline.
type MemoResult struct {
	Action     MemoAction    `json:"action"`
	ActorID    string        `json:"actor_id"`
	ActorName  string        `json:"actor_name"`
	TargetID   string        `json:"target_id"`
	TargetName string        `json:"target_name"`
	Memo       CharacterMemo `json:"memo"`
	Response   string        `json:"response"`
	Changed    bool          `json:"changed"`
}

// MemoOptions binds values which would otherwise be process-local or
// nondeterministic. ID is normally the durable command ID, so a record has a
// stable identity even when a commit result has to be reconciled.
type MemoOptions struct {
	Now time.Time
	ID  string
}

// MemoProposal is a snapshot-bound append candidate. Private expected fields
// make it impossible for a caller to manufacture a target or overwrite a
// concurrent append without going through PlanMemo.
type MemoProposal struct {
	Action     MemoAction
	ActorID    string
	ActorName  string
	TargetID   string
	TargetName string
	Memo       CharacterMemo
	Response   string
	Changed    bool

	expectedActor       PlayerState
	expectedTarget      PlayerState
	expectedMemos       []CharacterMemo
	expectedTargetEntry bool
	expectedImported    bool
}

// ValidateMemoBody applies the explicit UTF-8 and terminal-safety boundary
// for one-line memo content. The legacy command had a small 80-byte command
// budget; rejecting at the same byte boundary prevents truncating a UTF-8
// sequence or persisting protocol controls.
func ValidateMemoBody(body string) error {
	if body == "" || strings.TrimSpace(body) == "" {
		return ErrMemoBodyEmpty
	}
	if !utf8.ValidString(body) {
		return ErrMemoBodyInvalidUTF8
	}
	if len(body) > MaxMemoBodyBytes {
		return ErrMemoBodyTooLong
	}
	for _, r := range body {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrMemoBodyControl
		}
	}
	return nil
}

// ValidateMemoTargetName validates one display-name token without silently
// trimming or accepting path-like input. CanonicalName applies the same
// ASCII-only case rule as the legacy lowercize(..., 1) path.
func ValidateMemoTargetName(name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return ErrMemoTargetRequired
	}
	if _, err := identity.CanonicalName(name); err != nil {
		return ErrMemoTargetUnavailable
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrMemoTargetUnavailable
		}
	}
	return nil
}

func validMemoID(id string) bool {
	if id == "" || len(id) > MaxMemoIDBytes || !utf8.ValidString(id) || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validMemoPlayer(player PlayerState) bool {
	if player.Body.Type != 0 || player.Body.Name == "" || strings.TrimSpace(player.Body.Name) != player.Body.Name {
		return false
	}
	canonical, err := identity.CanonicalName(player.Body.Name)
	return err == nil && canonical == player.Body.Name
}

func validateMemoRecord(s State, recipientID string, memo CharacterMemo, seen map[string]struct{}) error {
	if !validMemoID(memo.ID) {
		return ErrMemoInvalidID
	}
	if _, ok := seen[memo.ID]; ok {
		return fmt.Errorf("%w: duplicate ID", ErrMemoInvalidRecord)
	}
	if recipientID == "" {
		return ErrMemoInvalidRecord
	}
	recipient, ok := s.Players[recipientID]
	if !ok || !validMemoPlayer(recipient) {
		return ErrMemoInvalidRecord
	}
	sender, ok := s.Players[memo.SenderID]
	if !ok || !validMemoPlayer(sender) || memo.SenderName != sender.Body.Name {
		return ErrMemoInvalidRecord
	}
	if err := ValidateMemoBody(memo.Body); err != nil {
		return err
	}
	if memo.CreatedAt.IsZero() || memo.CreatedAt.Before(time.Unix(0, 0)) {
		return ErrMemoInvalidTimestamp
	}
	seen[memo.ID] = struct{}{}
	return nil
}

// validateMemos treats nil as the explicit pre-migration marker. An empty,
// nonnil map is the imported empty aggregate and is therefore writable.
func (s State) validateMemos() error {
	if s.Memos == nil {
		return nil
	}
	if len(s.Memos) > MaxMemoRecipients {
		return fmt.Errorf("%w: recipients", ErrMemoLimit)
	}
	seen := make(map[string]struct{})
	total := 0
	for recipientID, records := range s.Memos {
		if recipientID == "" {
			return ErrMemoInvalidRecord
		}
		recipient, ok := s.Players[recipientID]
		if !ok || !validMemoPlayer(recipient) {
			return ErrMemoTargetUnavailable
		}
		if len(records) > MaxMemoRecords || total > MaxMemoRecords-len(records) {
			return ErrMemoLimit
		}
		for _, memo := range records {
			if err := validateMemoRecord(s, recipientID, memo, seen); err != nil {
				return fmt.Errorf("%w: %v", ErrMemoInvalidRecord, err)
			}
		}
		total += len(records)
	}
	return nil
}

func copyMemos(records []CharacterMemo) []CharacterMemo {
	if records == nil {
		return nil
	}
	return append([]CharacterMemo(nil), records...)
}

// ResolveMemoTargetID maps one exact character-name token to one canonical
// player ID. Offline players remain valid recipients, matching memo's legacy
// file semantics; only the authenticated actor is required to be online.
func (s State) ResolveMemoTargetID(name string) (string, error) {
	if s.Memos == nil {
		return "", ErrMemoStateUnresolved
	}
	if err := ValidateMemoTargetName(name); err != nil {
		return "", err
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", ErrMemoTargetUnavailable
	}
	var resolved string
	for id, player := range s.Players {
		if !validMemoPlayer(player) || player.Body.Name != canonical {
			continue
		}
		if resolved != "" {
			return "", ErrMemoTargetAmbiguous
		}
		resolved = id
	}
	if resolved == "" {
		return "", ErrMemoTargetUnavailable
	}
	return resolved, nil
}

// ResolveMemoRecipientID is the mail-style alias for callers porting the
// recipient vocabulary from post.c.
func (s State) ResolveMemoRecipientID(name string) (string, error) {
	return s.ResolveMemoTargetID(name)
}

func (s State) memoActor(actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	if s.Memos == nil {
		return PlayerState{}, ErrMemoStateUnresolved
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || !validMemoPlayer(actor) {
		return PlayerState{}, ErrMemoActorAbsent
	}
	return actor, nil
}

func normalizeMemoTimestamp(now time.Time) (time.Time, error) {
	if now.IsZero() || now.Before(time.Unix(0, 0)) {
		return time.Time{}, ErrMemoInvalidTimestamp
	}
	return now.Round(0).UTC(), nil
}

// PlanMemo creates an append candidate with a caller-provided timestamp. The
// simple form is useful for deterministic unit callers; session adapters use
// PlanMemoWithOptions so the durable command ID becomes the record ID.
func (s State) PlanMemo(actorID, targetName, body string, now time.Time) (MemoProposal, error) {
	return s.PlanMemoWithOptions(actorID, targetName, body, MemoOptions{Now: now})
}

func (s State) PlanMemoWithOptions(actorID, targetName, body string, options MemoOptions) (MemoProposal, error) {
	actor, err := s.memoActor(actorID)
	if err != nil {
		return MemoProposal{}, err
	}
	if err := ValidateMemoBody(body); err != nil {
		return MemoProposal{}, err
	}
	targetID, err := s.ResolveMemoTargetID(targetName)
	if err != nil {
		return MemoProposal{}, err
	}
	target := s.Players[targetID]
	createdAt, err := normalizeMemoTimestamp(options.Now)
	if err != nil {
		return MemoProposal{}, err
	}
	id := options.ID
	if id == "" {
		// Direct pure callers do not necessarily have a command ID. Keep a
		// valid deterministic placeholder only for the proposal; session
		// receipts always pass their real durable command ID.
		id = fmt.Sprintf("memo-%d", createdAt.UnixNano())
	}
	if !validMemoID(id) {
		return MemoProposal{}, ErrMemoInvalidID
	}
	for _, records := range s.Memos {
		for _, record := range records {
			if record.ID == id {
				return MemoProposal{}, ErrMemoInvalidID
			}
		}
	}
	record := CharacterMemo{ID: id, SenderID: actorID, SenderName: actor.Body.Name, Body: body, CreatedAt: createdAt}
	return MemoProposal{
		Action: MemoAppend, ActorID: actorID, ActorName: actor.Body.Name,
		TargetID: targetID, TargetName: target.Body.Name, Memo: record,
		Response: MemoResponse, Changed: true, expectedActor: actor,
		expectedTarget: target, expectedMemos: copyMemos(s.Memos[targetID]),
		expectedTargetEntry: func() bool { _, ok := s.Memos[targetID]; return ok }(),
		expectedImported:    true,
	}, nil
}

func sameMemoRecords(a, b []CharacterMemo) bool { return reflect.DeepEqual(a, b) }

func (s State) staleMemoProposal(proposal MemoProposal) error {
	if proposal.Action != MemoAppend || !proposal.expectedImported || s.Memos == nil {
		return ErrMemoInvalidProposal
	}
	actor, err := s.memoActor(proposal.ActorID)
	if err != nil {
		return err
	}
	target, ok := s.Players[proposal.TargetID]
	if !ok || !validMemoPlayer(target) {
		return ErrMemoTargetUnavailable
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) || !reflect.DeepEqual(target, proposal.expectedTarget) ||
		actor.Body.Name != proposal.ActorName || target.Body.Name != proposal.TargetName {
		return ErrMemoStaleProposal
	}
	current, present := s.Memos[proposal.TargetID]
	if present != proposal.expectedTargetEntry || !sameMemoRecords(current, proposal.expectedMemos) {
		return ErrMemoStaleProposal
	}
	return nil
}

// ApplyMemo rechecks all authority and appends exactly one record. Durable
// command replay is handled by engine.Execute before this reducer is called.
func (s State) ApplyMemo(proposal MemoProposal) (State, MemoResult, error) {
	if err := s.staleMemoProposal(proposal); err != nil {
		return State{}, MemoResult{}, err
	}
	if err := validateMemoRecord(s, proposal.TargetID, proposal.Memo, map[string]struct{}{}); err != nil {
		return State{}, MemoResult{}, err
	}
	next := s.clone()
	next.Memos[proposal.TargetID] = append(next.Memos[proposal.TargetID], proposal.Memo)
	if err := next.Validate(); err != nil {
		return State{}, MemoResult{}, err
	}
	result := MemoResult{
		Action: MemoAppend, ActorID: proposal.ActorID, ActorName: proposal.ActorName,
		TargetID: proposal.TargetID, TargetName: proposal.TargetName, Memo: proposal.Memo,
		Response: proposal.Response, Changed: true,
	}
	return next, result, nil
}

// AppendMemo is the direct pure convenience used by tests and import tools.
func (s State) AppendMemo(actorID, targetName, body string, now time.Time) (State, MemoResult, error) {
	proposal, err := s.PlanMemo(actorID, targetName, body, now)
	if err != nil {
		return State{}, MemoResult{}, err
	}
	return s.ApplyMemo(proposal)
}

// ApplyCharacterMemo and PlanCharacterMemo are source-neutral aliases.
func (s State) PlanCharacterMemo(actorID, targetName, body string, now time.Time) (MemoProposal, error) {
	return s.PlanMemo(actorID, targetName, body, now)
}

func (s State) ApplyCharacterMemo(proposal MemoProposal) (State, MemoResult, error) {
	return s.ApplyMemo(proposal)
}
