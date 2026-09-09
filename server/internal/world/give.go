package world

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GiveKind identifies the two source branches in command8.c:give.  The
// command boundary decides whether the first argument is an item or a token
// ending in 냥; the world reducer never guesses between the two.
type GiveKind string

const (
	GiveItem  GiveKind = "item"
	GiveMoney GiveKind = "money"
)

// GiveTargetKind is closed to canonical player recipients.  NPC targets are
// still represented so a caller can distinguish a missing target from the
// deliberately unimplemented NPC receive branch.
type GiveTargetKind string

const (
	GiveTargetPlayer GiveTargetKind = "player"
	GiveTargetNPC    GiveTargetKind = "npc"
)

var (
	ErrGiveActorAbsent            = errors.New("give actor absent")
	ErrGiveRoomMembership         = errors.New("give actor room membership absent")
	ErrGiveInventoryPending       = errors.New("give canonical inventory pending")
	ErrGiveItemAbsent             = errors.New("give item not found")
	ErrGiveTargetAbsent           = errors.New("give target not found")
	ErrGiveSelf                   = errors.New("give cannot target self")
	ErrGiveNPCPending             = errors.New("give NPC recipient pending")
	ErrGiveNPCIdentityPending     = errors.New("give NPC identity pending")
	ErrGiveAmountInvalid          = errors.New("give money amount invalid")
	ErrGiveInsufficientGold       = errors.New("give insufficient gold")
	ErrGiveGoldOverflow           = errors.New("give recipient gold overflow")
	ErrGiveCapacity               = errors.New("give recipient capacity exceeded")
	ErrGiveProtectedPending       = errors.New("give protected item semantics pending")
	ErrGiveQuestPending           = errors.New("give quest item semantics pending")
	ErrGiveEventPending           = errors.New("give event item semantics pending")
	ErrGiveNestedProtectedPending = errors.New("give nested protected item semantics pending")
	ErrGiveStaleProposal          = errors.New("stale or invalid give proposal")
)

// Descriptive aliases keep source-oriented callers from having to know the
// internal spelling chosen for this bounded lane.  They intentionally point
// at the same sentinels, so errors.Is remains stable across adapters.
var (
	ErrGiveCanonicalInventoryRequired = ErrGiveInventoryPending
	ErrGiveTargetUnavailable          = ErrGiveTargetAbsent
	ErrGiveNPCRecipientPending        = ErrGiveNPCPending
	ErrGiveQuestTransferPending       = ErrGiveQuestPending
	ErrGiveEventTransferPending       = ErrGiveEventPending
	ErrGiveProtectedItemPending       = ErrGiveProtectedPending
	ErrGiveNestedProtectionPending    = ErrGiveNestedProtectedPending
)

// GiveInput is the trusted, already-parsed command shape.  Display selectors
// remain names; the reducer resolves canonical IDs from the authoritative
// snapshot.  Zero occurrences are normalized to one for the exact three-token
// form, matching cmd->val's default in the C parser.
type GiveInput struct {
	ActorID          string
	Kind             GiveKind
	ItemName         string
	ItemOccurrence   int
	TargetName       string
	TargetOccurrence int
	Amount           int64
}

// GiveEvent contains post-commit projections.  Result.Response is for the
// actor; TargetResponse and ObserverResponse are kept separate so a future
// transport cannot accidentally send a private target message to the room.
// IDs are canonical audit data and are never accepted from a terminal line.
type GiveEvent struct {
	RoomID          int16          `json:"room_id"`
	ActorID         string         `json:"actor_id"`
	ActorName       string         `json:"actor_name"`
	TargetID        string         `json:"target_id"`
	TargetKind      GiveTargetKind `json:"target_kind"`
	TargetName      string         `json:"target_name"`
	ExcludeActorID  string         `json:"exclude_actor_id"`
	ExcludeTargetID string         `json:"exclude_target_id,omitempty"`
	Text            string         `json:"text,omitempty"`
	ObserverText    string         `json:"observer_text,omitempty"`
	TargetText      string         `json:"target_text,omitempty"`
}

// GiveResult is the durable actor result and recipient projection for one
// command.  The state candidate and this result are committed atomically by
// the session engine.
type GiveResult struct {
	Action           string         `json:"action"`
	Kind             GiveKind       `json:"kind"`
	RoomID           int16          `json:"room_id"`
	ActorID          string         `json:"actor_id"`
	ActorName        string         `json:"actor_name"`
	TargetID         string         `json:"target_id"`
	TargetKind       GiveTargetKind `json:"target_kind"`
	TargetName       string         `json:"target_name"`
	ItemID           string         `json:"item_id,omitempty"`
	ItemName         string         `json:"item_name,omitempty"`
	Amount           int64          `json:"amount,omitempty"`
	GoldBefore       int64          `json:"gold_before,omitempty"`
	GoldAfter        int64          `json:"gold_after,omitempty"`
	Response         string         `json:"response"`
	TargetResponse   string         `json:"target_response,omitempty"`
	ObserverResponse string         `json:"observer_response,omitempty"`
	Broadcast        bool           `json:"broadcast"`
	Event            *GiveEvent     `json:"event,omitempty"`
}

// GiveProposal is bound to one exact State snapshot.  The private before
// value prevents a proposal from being applied after either player's gold,
// room membership, or item graph has changed.
type GiveProposal struct {
	Kind             GiveKind
	ActorID          string
	ActorName        string
	RoomID           int16
	TargetID         string
	TargetKind       GiveTargetKind
	TargetName       string
	ItemID           string
	ItemName         string
	ItemOccurrence   int
	TargetOccurrence int
	Amount           int64
	Response         string
	TargetResponse   string
	ObserverResponse string
	Broadcast        bool
	ClearHidden      bool
	ExpectedEvent    *GiveEvent

	before State
}

type giveTarget struct {
	ID   string
	Kind GiveTargetKind
}

const giveMaxPlayerGold int64 = 1<<31 - 1

// ParseGiveAmount accepts the money token used by give_money.  C calls atol
// after checking only the trailing 냥 suffix; the Go boundary uses a strict
// decimal token so malformed or overflowing text cannot be normalized into a
// different amount during JSON/retry processing.
func ParseGiveAmount(token string) (int64, error) {
	if token == "" || !strings.HasSuffix(token, "냥") {
		return 0, fmt.Errorf("%w: amount must end in 냥", ErrGiveAmountInvalid)
	}
	digits := strings.TrimSuffix(token, "냥")
	if digits == "" {
		return 0, fmt.Errorf("%w: amount is empty", ErrGiveAmountInvalid)
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%w: decimal digits required", ErrGiveAmountInvalid)
		}
	}
	amount, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || amount < 1 {
		return 0, fmt.Errorf("%w: amount must be positive", ErrGiveAmountInvalid)
	}
	return amount, nil
}

// ParseGiveMoneyAmount is a source-oriented alias for callers that name the
// branch rather than the command as a whole.
func ParseGiveMoneyAmount(token string) (int64, error) { return ParseGiveAmount(token) }

func validGiveName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func (s State) giveActor(actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validGiveName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, ErrGiveActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, fmt.Errorf("%w: room %d", ErrGiveRoomMembership, actor.Body.RoomID)
	}
	if !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, ErrGiveRoomMembership
	}
	return actor, room, nil
}

func givePlayerVisible(actor, target LegacyMonster) bool {
	// creature.c:find_crt always hides caretaker-class PDMINV identities and
	// requires PDINVI for ordinary PINVIS identities.
	if target.Class >= playerCaretakerClass && flag(target.Flags[:], playerDMInvisibleFlag) {
		return false
	}
	return !flag(target.Flags[:], playerInvisibleFlag) || flag(actor.Flags[:], playerDetectInvisibleFlag)
}

func giveNPCVisible(actor, target LegacyMonster) bool {
	return !flag(target.Flags[:], npcInvisibleFlag) || flag(actor.Flags[:], playerDetectInvisibleFlag)
}

// selectGiveTarget preserves give()'s player-list-first, monster-list-second
// lookup.  Prefix/key matching is intentionally not guessed here: the
// bounded command admits exact display selectors only.  An unresolved legacy
// monster list is a migration error, never a reason to invent an NPC ID.
func (s State) selectGiveTarget(actorID, name string, occurrence int) (giveTarget, error) {
	actor, room, err := s.giveActor(actorID)
	if err != nil {
		return giveTarget{}, err
	}
	if !validGiveName(name) {
		return giveTarget{}, fmt.Errorf("%w: invalid target name", ErrGiveTargetAbsent)
	}
	if occurrence < 1 {
		return giveTarget{}, fmt.Errorf("%w: invalid target occurrence", ErrGiveTargetAbsent)
	}
	found := 0
	for _, id := range room.PlayerIDs {
		target, ok := s.Players[id]
		if id == "" || !ok || !target.Online || target.Body.Type != 0 || target.Body.RoomID != room.Resource.ID || !validGiveName(target.Body.Name) {
			return giveTarget{}, fmt.Errorf("%w: unresolved player identity", ErrGiveTargetAbsent)
		}
		if !strings.EqualFold(target.Body.Name, name) || !givePlayerVisible(actor.Body, target.Body) {
			continue
		}
		found++
		if found == occurrence {
			return giveTarget{ID: id, Kind: GiveTargetPlayer}, nil
		}
	}

	if s.NPCs == nil {
		if len(room.NPCIDs) != 0 || len(room.Resource.Monsters) != 0 {
			return giveTarget{}, ErrGiveNPCIdentityPending
		}
		return giveTarget{}, ErrGiveTargetAbsent
	}
	found = 0
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validGiveName(npc.Body.Name) {
			return giveTarget{}, fmt.Errorf("%w: unresolved NPC identity", ErrGiveNPCIdentityPending)
		}
		if !strings.EqualFold(npc.Body.Name, name) || !giveNPCVisible(actor.Body, npc.Body) {
			continue
		}
		found++
		if found == occurrence {
			return giveTarget{ID: id, Kind: GiveTargetNPC}, nil
		}
	}
	return giveTarget{}, ErrGiveTargetAbsent
}

func selectGiveItem(c ItemCollection, name string, occurrence int, detectInvisible bool) (string, Item, error) {
	if err := c.Validate(); err != nil {
		return "", Item{}, err
	}
	if !validGiveName(name) || occurrence < 1 {
		return "", Item{}, ErrGiveItemAbsent
	}
	found := 0
	for _, id := range c.Inventory {
		item, ok := c.Items[id]
		if id == "" || !ok || item.Object.Name == "" || !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !detectInvisible {
			continue
		}
		found++
		if found == occurrence {
			return id, item, nil
		}
	}
	return "", Item{}, ErrGiveItemAbsent
}

func giveProtectedError(kind error, nested bool) error {
	protected := fmt.Errorf("%w: %w", ErrGiveProtectedPending, kind)
	if nested {
		return fmt.Errorf("%w: %w", ErrGiveNestedProtectedPending, protected)
	}
	return protected
}

// validateGiveProtection mirrors command8.c's root and direct-child checks.
// Canonical trees can be deeper than C's linked-list loop; every descendant is
// inspected so a protected nested object cannot escape through an ID graph.
// DM and above retain the source exception.  The special root event key is
// also retained, while protected descendants remain fail-closed.
func validateGiveProtection(items ItemCollection, root string, class byte) error {
	if class >= playerDMClass {
		return nil
	}
	item, ok := items.Items[root]
	if !ok {
		return ErrGiveItemAbsent
	}
	if item.Object.Quest != 0 {
		return giveProtectedError(ErrGiveQuestPending, false)
	}
	if flag(item.Object.Flags[:], objectOneWevFlag) && item.Object.Keys[2] != "이벤트" {
		return giveProtectedError(ErrGiveEventPending, false)
	}
	seen := map[string]bool{root: true}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] {
			return fmt.Errorf("cyclic give item tree")
		}
		seen[id] = true
		child, exists := items.Items[id]
		if !exists {
			return fmt.Errorf("missing give item child")
		}
		if child.Object.Quest != 0 {
			return giveProtectedError(ErrGiveQuestPending, true)
		}
		if flag(child.Object.Flags[:], objectOneWevFlag) {
			return giveProtectedError(ErrGiveEventPending, true)
		}
		for _, nested := range child.Contents {
			if err := visit(nested); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range item.Contents {
		if err := visit(child); err != nil {
			return err
		}
	}
	return nil
}

func validateGiveCapacity(actor PlayerState, target PlayerState, source ItemCollection, itemID string) error {
	if target.Items == nil || len(target.Body.Inventory) != 0 {
		return ErrGiveInventoryPending
	}
	if err := target.Items.Validate(); err != nil {
		return err
	}
	rootWeight, err := source.objectWeight(itemID)
	if err != nil {
		return err
	}
	carriedWeight, err := target.Items.Weight()
	if err != nil {
		return err
	}
	if rootWeight < 0 || carriedWeight < 0 {
		return fmt.Errorf("%w: negative item weight", ErrGiveCapacity)
	}
	if int64(rootWeight) > int64(maxPlayerWeight(target.Body))-int64(carriedWeight) {
		return fmt.Errorf("%w: recipient can no longer carry the item", ErrGiveCapacity)
	}
	count, err := target.Items.CapacityCount()
	if err != nil {
		return err
	}
	// command8.c checks the recipient's count before add_obj_crt, so a count
	// of exactly 150 is still accepted and a pre-existing count above 150 is
	// rejected.  ItemCollection caps this value at 200, preserving that gate.
	if count > 150 {
		return fmt.Errorf("%w: 더이상 가질 수 없습니다", ErrGiveCapacity)
	}
	_ = actor // kept in the signature to make the source/recipient boundary explicit
	return nil
}

func giveItemProjection(actor LegacyMonster, target LegacyMonster, item Item) (string, string, string, *GiveEvent) {
	actorDisplay := actor.Name + "님"
	targetDisplay := target.Name + "님"
	itemName := item.Object.Name
	particle := valueObjectParticle(itemName)
	actorText := fmt.Sprintf("당신은 %s에게 %s%s 줍니다.\r\n", targetDisplay, itemName, particle)
	targetText := fmt.Sprintf("\n%s이 당신에게 %s%s 줍니다.\r\n", actorDisplay, itemName, particle)
	observerText := fmt.Sprintf("\n%s이 %s에게 %s%s 줍니다.\r\n", actorDisplay, targetDisplay, itemName, particle)
	event := &GiveEvent{
		RoomID: actor.RoomID, ActorName: actor.Name, TargetName: target.Name,
		TargetKind: GiveTargetPlayer, Text: observerText, ObserverText: observerText,
		TargetText: targetText,
	}
	return actorText, targetText, observerText, event
}

func giveMoneyProjection(actor LegacyMonster, target LegacyMonster, amount int64) (string, string, string, *GiveEvent) {
	actorDisplay := actor.Name + "님"
	targetDisplay := target.Name + "님"
	actorText := fmt.Sprintf("당신은 %s에게 %d냥을 주었습니다.\r\n", targetDisplay, amount)
	targetText := fmt.Sprintf("\n%s이 당신에게 %d냥을 주었습니다.\r\n", actorDisplay, amount)
	observerText := fmt.Sprintf("\n%s이 %s에게 %d냥을 주었습니다.\r\n", actorDisplay, targetDisplay, amount)
	event := &GiveEvent{
		RoomID: actor.RoomID, ActorName: actor.Name, TargetName: target.Name,
		TargetKind: GiveTargetPlayer, Text: observerText, ObserverText: observerText,
		TargetText: targetText,
	}
	return actorText, targetText, observerText, event
}

func (s State) planGive(in GiveInput) (GiveProposal, error) {
	actor, room, err := s.giveActor(in.ActorID)
	if err != nil {
		return GiveProposal{}, err
	}
	itemOccurrence := in.ItemOccurrence
	if itemOccurrence == 0 {
		itemOccurrence = 1
	}
	targetOccurrence := in.TargetOccurrence
	if targetOccurrence == 0 {
		targetOccurrence = 1
	}
	if itemOccurrence < 1 || targetOccurrence < 1 || !validGiveName(in.TargetName) {
		return GiveProposal{}, fmt.Errorf("%w: invalid give selector", ErrGiveTargetAbsent)
	}
	proposal := GiveProposal{
		Kind: in.Kind, ActorID: in.ActorID, ActorName: actor.Body.Name,
		RoomID: room.Resource.ID, TargetName: in.TargetName,
		ItemOccurrence: itemOccurrence, TargetOccurrence: targetOccurrence,
		before: s.clone(),
	}
	switch in.Kind {
	case GiveItem:
		if in.ItemName == "" || in.Amount != 0 {
			return GiveProposal{}, fmt.Errorf("%w: invalid item branch", ErrGiveItemAbsent)
		}
		if actor.Items == nil || len(actor.Body.Inventory) != 0 {
			return GiveProposal{}, ErrGiveInventoryPending
		}
		itemID, item, err := selectGiveItem(*actor.Items, in.ItemName, itemOccurrence, flag(actor.Body.Flags[:], playerDetectInvisibleFlag))
		if err != nil {
			return GiveProposal{}, err
		}
		// This lookup deliberately follows item selection, matching give()'s
		// source order and ensuring a missing item cannot reveal target state.
		target, err := s.selectGiveTarget(in.ActorID, in.TargetName, targetOccurrence)
		if err != nil {
			return GiveProposal{}, err
		}
		if target.Kind == GiveTargetNPC {
			return GiveProposal{}, ErrGiveNPCPending
		}
		if target.ID == in.ActorID {
			return GiveProposal{}, ErrGiveSelf
		}
		targetPlayer, ok := s.Players[target.ID]
		if !ok || !targetPlayer.Online || targetPlayer.Body.RoomID != room.Resource.ID {
			return GiveProposal{}, ErrGiveTargetAbsent
		}
		if err := validateGiveProtection(*actor.Items, itemID, actor.Body.Class); err != nil {
			return GiveProposal{}, err
		}
		if err := validateGiveCapacity(actor, targetPlayer, *actor.Items, itemID); err != nil {
			return GiveProposal{}, err
		}
		plan, err := TransferItemRoots(*actor.Items, *targetPlayer.Items, []string{itemID})
		if err != nil {
			return GiveProposal{}, err
		}
		_ = plan
		proposal.TargetID, proposal.TargetKind = target.ID, target.Kind
		proposal.TargetName = targetPlayer.Body.Name
		proposal.ItemID, proposal.ItemName = itemID, item.Object.Name
		proposal.ClearHidden = true
		proposal.Response, proposal.TargetResponse, proposal.ObserverResponse, proposal.ExpectedEvent = giveItemProjection(actor.Body, targetPlayer.Body, item)
		proposal.ExpectedEvent.ActorID = in.ActorID
		proposal.ExpectedEvent.TargetID = target.ID
		proposal.ExpectedEvent.ExcludeActorID = in.ActorID
		proposal.ExpectedEvent.ExcludeTargetID = target.ID
		proposal.Broadcast = true
		return proposal, nil
	case GiveMoney:
		if in.Amount < 1 {
			return GiveProposal{}, ErrGiveAmountInvalid
		}
		if actor.Body.Gold < 0 {
			return GiveProposal{}, fmt.Errorf("%w: actor gold is negative", ErrGiveAmountInvalid)
		}
		// give_money checks amount and actor funds before it resolves the target.
		if in.Amount > int64(actor.Body.Gold) {
			return GiveProposal{}, ErrGiveInsufficientGold
		}
		target, err := s.selectGiveTarget(in.ActorID, in.TargetName, targetOccurrence)
		if err != nil {
			return GiveProposal{}, err
		}
		if target.Kind == GiveTargetNPC {
			return GiveProposal{}, ErrGiveNPCPending
		}
		if target.ID == in.ActorID {
			return GiveProposal{}, ErrGiveSelf
		}
		targetPlayer, ok := s.Players[target.ID]
		if !ok || !targetPlayer.Online || targetPlayer.Body.RoomID != room.Resource.ID {
			return GiveProposal{}, ErrGiveTargetAbsent
		}
		if targetPlayer.Body.Gold < 0 {
			return GiveProposal{}, fmt.Errorf("%w: recipient gold is negative", ErrGiveGoldOverflow)
		}
		if int64(targetPlayer.Body.Gold) > giveMaxPlayerGold-in.Amount {
			return GiveProposal{}, ErrGiveGoldOverflow
		}
		proposal.TargetID, proposal.TargetKind = target.ID, target.Kind
		proposal.TargetName = targetPlayer.Body.Name
		proposal.Amount = in.Amount
		proposal.Response, proposal.TargetResponse, proposal.ObserverResponse, proposal.ExpectedEvent = giveMoneyProjection(actor.Body, targetPlayer.Body, in.Amount)
		proposal.ExpectedEvent.ActorID = in.ActorID
		proposal.ExpectedEvent.TargetID = target.ID
		proposal.ExpectedEvent.ExcludeActorID = in.ActorID
		proposal.ExpectedEvent.ExcludeTargetID = target.ID
		proposal.Broadcast = true
		return proposal, nil
	default:
		return GiveProposal{}, fmt.Errorf("unsupported give branch %q", in.Kind)
	}
}

// PlanGive validates source ordering, canonical identity, and all transfer
// guards without mutating the source snapshot.
func (s State) PlanGive(in GiveInput) (GiveProposal, error) {
	if err := s.Validate(); err != nil {
		return GiveProposal{}, err
	}
	return s.planGive(in)
}

func (s State) validateGiveProposalCommon(p GiveProposal) (PlayerState, RoomState, PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, PlayerState{}, err
	}
	if p.ActorID == "" || p.Response == "" || p.TargetResponse == "" || p.ObserverResponse == "" || !p.Broadcast || p.ExpectedEvent == nil || !reflect.DeepEqual(s, p.before) {
		return PlayerState{}, RoomState{}, PlayerState{}, ErrGiveStaleProposal
	}
	actor, room, err := s.giveActor(p.ActorID)
	if err != nil || room.Resource.ID != p.RoomID {
		return PlayerState{}, RoomState{}, PlayerState{}, ErrGiveStaleProposal
	}
	if p.TargetKind != GiveTargetPlayer || p.TargetID == "" || p.TargetID == p.ActorID {
		return PlayerState{}, RoomState{}, PlayerState{}, ErrGiveStaleProposal
	}
	target, ok := s.Players[p.TargetID]
	if !ok || !target.Online || target.Body.Type != 0 || target.Body.RoomID != p.RoomID || target.Body.Name != p.TargetName || !containsString(room.PlayerIDs, p.TargetID) || !givePlayerVisible(actor.Body, target.Body) {
		return PlayerState{}, RoomState{}, PlayerState{}, ErrGiveStaleProposal
	}
	return actor, room, target, nil
}

func eventMatchesGive(got, want *GiveEvent) bool {
	return got != nil && want != nil && reflect.DeepEqual(got, want)
}

// ApplyGive atomically applies a previously planned item subtree or gold
// transfer.  It rechecks all source conditions and validates the resulting
// State before returning a candidate for the durable command executor.
func (s State) ApplyGive(p GiveProposal) (State, GiveResult, error) {
	actor, _, target, err := s.validateGiveProposalCommon(p)
	if err != nil {
		return State{}, GiveResult{}, err
	}
	result := GiveResult{
		Action: "give", Kind: p.Kind, RoomID: p.RoomID, ActorID: p.ActorID,
		ActorName: actor.Body.Name, TargetID: p.TargetID, TargetKind: p.TargetKind,
		TargetName: target.Body.Name, ItemID: p.ItemID, ItemName: p.ItemName,
		Amount: p.Amount, Response: p.Response, TargetResponse: p.TargetResponse,
		ObserverResponse: p.ObserverResponse, Broadcast: p.Broadcast,
	}
	switch p.Kind {
	case GiveItem:
		if p.ItemID == "" || p.ItemName == "" || p.Amount != 0 || p.ItemOccurrence < 1 || p.TargetOccurrence < 1 || !p.ClearHidden || actor.Items == nil || target.Items == nil || len(actor.Body.Inventory) != 0 || len(target.Body.Inventory) != 0 {
			return State{}, GiveResult{}, ErrGiveStaleProposal
		}
		item, ok := actor.Items.Items[p.ItemID]
		if !ok || item.Object.Name != p.ItemName || !containsString(actor.Items.Inventory, p.ItemID) {
			return State{}, GiveResult{}, ErrGiveStaleProposal
		}
		if err := validateGiveProtection(*actor.Items, p.ItemID, actor.Body.Class); err != nil {
			return State{}, GiveResult{}, err
		}
		if err := validateGiveCapacity(actor, target, *actor.Items, p.ItemID); err != nil {
			return State{}, GiveResult{}, err
		}
		plan, err := TransferItemRoots(*actor.Items, *target.Items, []string{p.ItemID})
		if err != nil {
			return State{}, GiveResult{}, err
		}
		response, targetResponse, observerResponse, event := giveItemProjection(actor.Body, target.Body, item)
		event.ActorID, event.TargetID, event.ExcludeActorID, event.ExcludeTargetID = p.ActorID, p.TargetID, p.ActorID, p.TargetID
		if p.Response != response || p.TargetResponse != targetResponse || p.ObserverResponse != observerResponse || !eventMatchesGive(p.ExpectedEvent, event) {
			return State{}, GiveResult{}, ErrGiveStaleProposal
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		nextActor.Items = &plan.Source
		nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
		next.Players[p.ActorID] = nextActor
		nextTarget := next.Players[p.TargetID]
		nextTarget.Items = &plan.Destination
		next.Players[p.TargetID] = nextTarget
		if err := next.Validate(); err != nil {
			return State{}, GiveResult{}, err
		}
		result.Event = event
		return next, result, nil
	case GiveMoney:
		if p.Amount < 1 || p.ItemID != "" || p.ItemName != "" || actor.Body.Gold < 0 || target.Body.Gold < 0 || p.Amount > int64(actor.Body.Gold) || int64(target.Body.Gold) > giveMaxPlayerGold-p.Amount {
			return State{}, GiveResult{}, ErrGiveStaleProposal
		}
		response, targetResponse, observerResponse, event := giveMoneyProjection(actor.Body, target.Body, p.Amount)
		event.ActorID, event.TargetID, event.ExcludeActorID, event.ExcludeTargetID = p.ActorID, p.TargetID, p.ActorID, p.TargetID
		if p.Response != response || p.TargetResponse != targetResponse || p.ObserverResponse != observerResponse || !eventMatchesGive(p.ExpectedEvent, event) {
			return State{}, GiveResult{}, ErrGiveStaleProposal
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		nextTarget := next.Players[p.TargetID]
		goldBefore := int64(nextActor.Body.Gold)
		nextActor.Body.Gold -= int32(p.Amount)
		nextTarget.Body.Gold += int32(p.Amount)
		next.Players[p.ActorID] = nextActor
		next.Players[p.TargetID] = nextTarget
		if err := next.Validate(); err != nil {
			return State{}, GiveResult{}, err
		}
		result.GoldBefore, result.GoldAfter = goldBefore, int64(nextActor.Body.Gold)
		result.Event = event
		return next, result, nil
	default:
		return State{}, GiveResult{}, ErrGiveStaleProposal
	}
}

// GiveItem moves one canonical inventory-root subtree to another same-room
// player.  Occurrences are one-based and resolved in stored root order.
func (s State) GiveItem(actorID, itemName string, itemOccurrence int, targetName string, targetOccurrence int) (State, GiveResult, error) {
	proposal, err := s.PlanGive(GiveInput{
		ActorID: actorID, Kind: GiveItem, ItemName: itemName,
		ItemOccurrence: itemOccurrence, TargetName: targetName,
		TargetOccurrence: targetOccurrence,
	})
	if err != nil {
		return State{}, GiveResult{}, err
	}
	return s.ApplyGive(proposal)
}

// GiveItemByName is the exact bounded command convenience form (occurrence 1
// for both selectors).
func (s State) GiveItemByName(actorID, itemName, targetName string) (State, GiveResult, error) {
	return s.GiveItem(actorID, itemName, 1, targetName, 1)
}

// GiveMoney atomically debits the actor and credits a same-room player.
func (s State) GiveMoney(actorID, targetName string, amount int64, targetOccurrence int) (State, GiveResult, error) {
	proposal, err := s.PlanGive(GiveInput{
		ActorID: actorID, Kind: GiveMoney, TargetName: targetName,
		TargetOccurrence: targetOccurrence, Amount: amount,
	})
	if err != nil {
		return State{}, GiveResult{}, err
	}
	return s.ApplyGive(proposal)
}

// GiveMoneyByName is the exact bounded command convenience form (target
// occurrence 1).
func (s State) GiveMoneyByName(actorID, targetName string, amount int64) (State, GiveResult, error) {
	return s.GiveMoney(actorID, targetName, amount, 1)
}

// Give dispatches the source token branch for non-session callers.  Session
// handlers should prefer ParseGiveLine so malformed money text is rejected at
// the terminal boundary before a receipt request is created.
func (s State) Give(actorID, source, targetName string) (State, GiveResult, error) {
	if strings.HasSuffix(source, "냥") {
		amount, err := ParseGiveAmount(source)
		if err != nil {
			return State{}, GiveResult{}, err
		}
		return s.GiveMoneyByName(actorID, targetName, amount)
	}
	return s.GiveItemByName(actorID, source, targetName)
}
