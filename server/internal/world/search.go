package world

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	searchTimerIndex     = 7 // LT_SERCH
	searchRangerClass    = 7
	searchCaretakerClass = 10
)

// SearchTarget is an authoritative identity found by a search.  The client
// supplies no identity: PlanSearch resolves these IDs from the actor's room
// and ApplySearch verifies them against the same snapshot before committing.
type SearchTarget struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// SearchProposal is the pure, deterministic output of PlanSearch.  It is
// deliberately independent of State so a caller can inspect or test the
// chance/cooldown/target decisions before applying them atomically.
//
// Targets are limited to canonical same-room players and NPCs.  Legacy
// hidden exits/objects are intentionally not guessed here; the corresponding
// resource graphs do not yet have the durable search contract needed by this
// slice.
type SearchProposal struct {
	ActorID     string
	RoomID      int16
	Now         int32
	Chance      int
	Interval    int32
	WaitSeconds int32
	Cooldown    bool
	ClearHidden bool
	Targets     []SearchTarget
	Response    string
	Broadcast   bool
}

// SearchResult is the actor response and the post-commit event input stored
// in a command receipt.  Persisting Targets is important: after a successful
// search the targets remain hidden, so re-running the RNG from transport
// would be both nondeterministic and an accidental second reducer execution.
type SearchResult struct {
	Response  string         `json:"response"`
	Broadcast bool           `json:"broadcast"`
	Targets   []SearchTarget `json:"targets,omitempty"`
}

// SearchEvent is a committed-state room projection.  C emits two room
// broadcasts in order: the search action itself, followed by a hint when a
// hidden target was found.  The actor is excluded because it already receives
// the durable receipt response.
type SearchEvent struct {
	RoomID         int16    `json:"room_id"`
	ExcludeActorID string   `json:"exclude_actor_id"`
	Texts          []string `json:"texts"`
}

// SearchChance ports the chance calculation at command5.c:search.  Stats[4]
// is piety in the admitted LegacyMonster layout.  Undefined signed/array
// accesses are rejected instead of allowing a Go panic or silently clamping
// a legacy value.  Ranger and caretaker overrides, including the blind
// ordering, intentionally follow the C statement order.
func SearchChance(player LegacyMonster) (int, error) {
	if player.Class > 12 || player.Stats[4] > 63 {
		return 0, fmt.Errorf("search actor has undefined legacy class or piety")
	}
	chance := 15 + 5*legacyStatBonus[player.Stats[4]] + ((int(player.Level)+3)/4)*2
	if chance > 90 {
		chance = 90
	}
	if player.Class == searchRangerClass {
		chance = 100
	}
	if flag(player.Flags[:], playerBlindFlag) && chance > 20 {
		chance = 20
	}
	if player.Class >= searchCaretakerClass {
		chance = 100
	}
	return chance, nil
}

func searchRoll(roll func(int, int) int, chance int) (bool, error) {
	if roll == nil {
		return false, fmt.Errorf("missing search random source")
	}
	value := roll(1, 100)
	if value < 1 || value > 100 {
		return false, fmt.Errorf("search random value outside 1..100")
	}
	return value <= chance, nil
}

func validSearchName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\r' || r == '\n' {
			return false
		}
	}
	return strings.TrimSpace(name) == name
}

// PlanSearch implements the bounded same-room hidden-target part of
// command5.c:search.  It consumes one 1..100 roll for every hidden canonical
// target in C's player-then-monster order, even when an invisible target is
// not visible to the actor; this preserves the source condition ordering.
// Objects and exits are not admitted by this slice and are never treated as
// found.  Cooldown returns a successful no-op proposal, matching C's early
// please_wait return before PHIDDN/timer mutation or room broadcast.
func (s State) PlanSearch(actorID string, now int32, roll func(int, int) int) (SearchProposal, error) {
	if err := s.Validate(); err != nil {
		return SearchProposal{}, err
	}
	if actorID == "" || now < 0 {
		return SearchProposal{}, fmt.Errorf("invalid search actor or clock")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validSearchName(actor.Body.Name) {
		return SearchProposal{}, fmt.Errorf("online search actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return SearchProposal{}, fmt.Errorf("search actor room absent")
	}
	chance, err := SearchChance(actor.Body)
	if err != nil {
		return SearchProposal{}, err
	}
	timer := actor.Body.Timers[searchTimerIndex]
	if timer.Interval < 0 || timer.LastTime < 0 {
		return SearchProposal{}, fmt.Errorf("search timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	proposal := SearchProposal{
		ActorID:  actorID,
		RoomID:   actor.Body.RoomID,
		Now:      now,
		Chance:   chance,
		Interval: 7,
	}
	if actor.Body.Class == searchRangerClass {
		proposal.Interval = 3
	}
	if int64(now) < deadline {
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(deadline - int64(now))
		if proposal.WaitSeconds == 1 {
			proposal.Response = "1초만 기다리세요.\n"
		} else {
			proposal.Response = fmt.Sprintf("%d초동안 기다리세요.\n", proposal.WaitSeconds)
		}
		return proposal, nil
	}
	proposal.ClearHidden = true

	// The canonical room lists are authoritative. Validate has already
	// checked their referential integrity; repeat the identity checks here so
	// this reducer remains fail-closed if it is called with a hand-built state
	// in a future migration boundary.
	for _, id := range room.PlayerIDs {
		target, exists := s.Players[id]
		if !exists || id == "" || !target.Online || target.Body.Type != 0 || target.Body.RoomID != room.Resource.ID || !validSearchName(target.Body.Name) {
			return SearchProposal{}, fmt.Errorf("unresolved search player identity")
		}
		// C clears the actor's PHIDDN before walking first_ply, so a hidden
		// actor is never rediscovered as their own target.
		if id == actorID {
			continue
		}
		if !flag(target.Body.Flags[:], playerHiddenStateFlag) || flag(target.Body.Flags[:], playerDMInvisibleFlag) {
			continue
		}
		found, err := searchRoll(roll, chance)
		if err != nil {
			return SearchProposal{}, err
		}
		if found && (flag(actor.Body.Flags[:], playerDetectInvisibleFlag) || !flag(target.Body.Flags[:], playerInvisibleFlag)) {
			proposal.Targets = append(proposal.Targets, SearchTarget{ID: id, Kind: "player", Name: target.Body.Name})
		}
	}
	if s.NPCs != nil {
		for _, id := range room.NPCIDs {
			target, exists := s.NPCs[id]
			if !exists || id == "" || target.Body.Type != 1 || target.Body.RoomID != room.Resource.ID || !validSearchName(target.Body.Name) {
				return SearchProposal{}, fmt.Errorf("unresolved search NPC identity")
			}
			if !flag(target.Body.Flags[:], npcHiddenFlag) {
				continue
			}
			found, err := searchRoll(roll, chance)
			if err != nil {
				return SearchProposal{}, err
			}
			if found && (flag(actor.Body.Flags[:], playerDetectInvisibleFlag) || !flag(target.Body.Flags[:], npcInvisibleFlag)) {
				proposal.Targets = append(proposal.Targets, SearchTarget{ID: id, Kind: "npc", Name: target.Body.Name})
			}
		}
	}
	if len(proposal.Targets) == 0 {
		proposal.Response = "당신은 아무것도 찾지 못했습니다.\n"
	} else {
		var out strings.Builder
		for _, target := range proposal.Targets {
			fmt.Fprintf(&out, "\n당신은 숨어있는 %s 찾아내었습니다.", target.Name)
		}
		proposal.Response = out.String()
	}
	proposal.Broadcast = true
	return proposal, nil
}

// ApplySearch atomically applies a previously planned search.  It verifies
// that every found identity is still the hidden same-room target represented
// by the proposal; a stale proposal is rejected rather than applying only a
// subset of the result.
func (s State) ApplySearch(proposal SearchProposal) (State, SearchResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, SearchResult{}, err
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.RoomID != proposal.RoomID {
		return State{}, SearchResult{}, fmt.Errorf("search proposal actor changed")
	}
	if proposal.Now < 0 || proposal.Interval < 1 || proposal.Interval > 7 || proposal.Chance < -100 || proposal.Chance > 100 {
		return State{}, SearchResult{}, fmt.Errorf("invalid search proposal")
	}
	if proposal.Cooldown {
		if proposal.WaitSeconds < 1 || proposal.Response == "" || proposal.Broadcast || proposal.ClearHidden {
			return State{}, SearchResult{}, fmt.Errorf("invalid search cooldown proposal")
		}
		deadline := int64(actor.Body.Timers[searchTimerIndex].LastTime) + int64(actor.Body.Timers[searchTimerIndex].Interval)
		if int64(proposal.Now) >= deadline || int64(proposal.WaitSeconds) != deadline-int64(proposal.Now) {
			return State{}, SearchResult{}, fmt.Errorf("stale search cooldown proposal")
		}
		return s, SearchResult{Response: proposal.Response}, nil
	}
	if !proposal.ClearHidden || !proposal.Broadcast || proposal.Response == "" {
		return State{}, SearchResult{}, fmt.Errorf("invalid search proposal")
	}
	room, ok := s.Rooms[proposal.RoomID]
	if !ok {
		return State{}, SearchResult{}, fmt.Errorf("search proposal room absent")
	}
	for _, found := range proposal.Targets {
		if found.ID == "" || !validSearchName(found.Name) {
			return State{}, SearchResult{}, fmt.Errorf("invalid search target")
		}
		switch found.Kind {
		case "player":
			if !containsString(room.PlayerIDs, found.ID) {
				return State{}, SearchResult{}, fmt.Errorf("search player moved")
			}
			target, exists := s.Players[found.ID]
			if !exists || !target.Online || target.Body.Type != 0 || target.Body.RoomID != proposal.RoomID || target.Body.Name != found.Name || !flag(target.Body.Flags[:], playerHiddenStateFlag) || flag(target.Body.Flags[:], playerDMInvisibleFlag) {
				return State{}, SearchResult{}, fmt.Errorf("search player target changed")
			}
		case "npc":
			if !containsString(room.NPCIDs, found.ID) || s.NPCs == nil {
				return State{}, SearchResult{}, fmt.Errorf("search NPC moved")
			}
			target, exists := s.NPCs[found.ID]
			if !exists || target.Body.Type != 1 || target.Body.RoomID != proposal.RoomID || target.Body.Name != found.Name || !flag(target.Body.Flags[:], npcHiddenFlag) {
				return State{}, SearchResult{}, fmt.Errorf("search NPC target changed")
			}
		default:
			return State{}, SearchResult{}, fmt.Errorf("unsupported search target kind %q", found.Kind)
		}
	}
	next := s.clone()
	actor = next.Players[proposal.ActorID]
	actor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	actor.Body.Timers[searchTimerIndex].LastTime = proposal.Now
	actor.Body.Timers[searchTimerIndex].Interval = proposal.Interval
	next.Players[proposal.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, SearchResult{}, err
	}
	return next, SearchResult{Response: proposal.Response, Broadcast: true, Targets: append([]SearchTarget(nil), proposal.Targets...)}, nil
}

const (
	npcHiddenFlag = 1 // MHIDDN
)

// RoomSearchEvent derives the committed room fan-out from receipt-persisted
// target identities. It never consumes random input and is not called on
// replay. A changed target fails closed so stale transport state cannot claim
// a hidden identity was found.
func (s State) RoomSearchEvent(actorID string, targets []SearchTarget) (SearchEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return SearchEvent{}, false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validSearchName(actor.Body.Name) {
		return SearchEvent{}, false, fmt.Errorf("online search actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return SearchEvent{}, false, fmt.Errorf("search event room absent")
	}
	for _, target := range targets {
		if target.ID == "" || !validSearchName(target.Name) {
			return SearchEvent{}, false, fmt.Errorf("invalid search event target")
		}
		switch target.Kind {
		case "player":
			p, exists := s.Players[target.ID]
			if !exists || !containsString(room.PlayerIDs, target.ID) || !p.Online || p.Body.RoomID != room.Resource.ID || p.Body.Name != target.Name || !flag(p.Body.Flags[:], playerHiddenStateFlag) || flag(p.Body.Flags[:], playerDMInvisibleFlag) {
				return SearchEvent{}, false, fmt.Errorf("search event player target changed")
			}
		case "npc":
			npc, exists := s.NPCs[target.ID]
			if s.NPCs == nil || !exists || !containsString(room.NPCIDs, target.ID) || npc.Body.RoomID != room.Resource.ID || npc.Body.Name != target.Name || !flag(npc.Body.Flags[:], npcHiddenFlag) {
				return SearchEvent{}, false, fmt.Errorf("search event NPC target changed")
			}
		default:
			return SearchEvent{}, false, fmt.Errorf("unsupported search event target kind %q", target.Kind)
		}
	}
	texts := []string{fmt.Sprintf("\n%s님이 주변을 샅샅이 뒤져봅니다.\r\n", actor.Body.Name)}
	if len(targets) > 0 {
		pronoun := "그녀"
		if flag(actor.Body.Flags[:], playerMaleFlag) {
			pronoun = "그"
		}
		texts = append(texts, fmt.Sprintf("\n%s가 뭘 발견한 것 같군요!\r\n", pronoun))
	}
	return SearchEvent{RoomID: actor.Body.RoomID, ExcludeActorID: actorID, Texts: texts}, true, nil
}

const playerMaleFlag = 12 // PMALES
