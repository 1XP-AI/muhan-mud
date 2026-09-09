package world

import (
	"fmt"
	"sort"
)

type EntityRef struct{ Kind, ID string }
type NPCEnemy struct {
	Target EntityRef
	Damage int32
}

// NPCPermanentOrigin identifies the legacy permanent-monster slot that owns
// an NPC instance.  C's die_perm_crt updates that room slot before moving the
// creature.  Keeping the origin by identity (rather than re-matching names)
// is required when two permanent templates share a display name.
type NPCPermanentOrigin struct {
	RoomID int16
	Slot   uint8
}

type NPCState struct {
	Body LegacyMonster
	// Nil means the NPC inventory has not undergone explicit ID-based
	// migration. Once present, Body.Inventory must be empty and ownership is
	// represented only by this canonical graph.
	Items *ItemCollection
	// Nil means unresolved legacy runtime relations, not confirmed peaceful.
	Enemies []NPCEnemy
	// Nil is allowed for imported legacy instances whose permanent origin has
	// not yet been resolved.  A chase that moves MPERMT requires this field.
	PermanentOrigin *NPCPermanentOrigin
	// FollowingPlayerID identifies a canonical player entry in first_fol.
	// It is used by the command6 MDMFOL movement slice; an empty value means
	// this NPC is not attached to a player follower list.
	FollowingPlayerID string
	// TradeOffers is the explicit migration of command10.c's MTRADE
	// carry[0..4]/carry[5..9] pairs. Nil means the offer table has not been
	// migrated; an empty non-nil slice is a known MTRADE NPC with no offers.
	// These are immutable object templates, not player-owned item identities.
	// The trade reducer materializes a fresh canonical graph for each reward.
	TradeOffers []NPCTradeOffer
}

// NPCTradeOffer is one source-backed MTRADE contract. Wanted is compared with
// the player's exact canonical inventory root by name and key[0], matching the
// two fields command10.c compares after load_obj. Reward is nil when C's paired
// carry entry is zero, in which case the offered item is consumed without a
// replacement object.
type NPCTradeOffer struct {
	Wanted LegacyObject  `json:"wanted"`
	Reward *LegacyObject `json:"reward,omitempty"`
}

func (s State) validateNPCs() error {
	if s.ActiveNPCIDs != nil && s.NPCs == nil {
		return fmt.Errorf("active NPC list without canonical NPC state")
	}
	active := map[string]bool{}
	for _, id := range s.ActiveNPCIDs {
		if _, ok := s.NPCs[id]; !ok || id == "" || active[id] {
			return fmt.Errorf("invalid active NPC identity")
		}
		active[id] = true
	}
	seen := map[string]bool{}
	for roomID, room := range s.Rooms {
		if s.NPCs == nil {
			if len(room.NPCIDs) != 0 {
				return fmt.Errorf("NPC identities without canonical NPC state")
			}
			continue
		}
		if len(room.Resource.Monsters) != 0 {
			return fmt.Errorf("duplicate canonical and legacy NPC state")
		}
		for _, id := range room.NPCIDs {
			npc, ok := s.NPCs[id]
			if !ok || id == "" || seen[id] || npc.Body.RoomID != roomID {
				return fmt.Errorf("invalid NPC room membership")
			}
			seen[id] = true
		}
	}
	for id, npc := range s.NPCs {
		if id == "" || !seen[id] || npc.Body.Type != 1 || npc.Body.Name == "" {
			return fmt.Errorf("invalid NPC identity or type")
		}
		if err := validateNPCTradeOffers(npc); err != nil {
			return fmt.Errorf("NPC %s trade offers: %w", id, err)
		}
		enemies := map[EntityRef]bool{}
		for _, enemy := range npc.Enemies {
			valid := false
			switch enemy.Target.Kind {
			case "player":
				_, valid = s.Players[enemy.Target.ID]
			case "npc":
				_, valid = s.NPCs[enemy.Target.ID]
			}
			if !valid || enemy.Target.ID == "" || enemies[enemy.Target] {
				return fmt.Errorf("invalid NPC enemy reference")
			}
			enemies[enemy.Target] = true
		}
		if npc.FollowingPlayerID != "" {
			player, ok := s.Players[npc.FollowingPlayerID]
			if !ok || !containsString(player.NPCFollowerIDs, id) {
				return fmt.Errorf("NPC following edge is not reciprocal")
			}
		}
	}
	for playerID, player := range s.Players {
		seenFollowers := map[string]bool{}
		for _, npcID := range player.NPCFollowerIDs {
			npc, ok := s.NPCs[npcID]
			if !ok || npcID == "" || seenFollowers[npcID] || npc.FollowingPlayerID != playerID {
				return fmt.Errorf("invalid NPC follower edge")
			}
			seenFollowers[npcID] = true
		}
	}
	return nil
}

// ImportNPCs converts each instance once, never matching by name or template.
// The containing room defines location (as add_crt_rom does when loading C).
// Legacy room records omit runtime enemy lists; resolving them is a separate
// explicit migration, not an assumption that every imported NPC is peaceful.
func (s State) ImportNPCs(allocate func() (string, error)) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if s.NPCs != nil || allocate == nil {
		return State{}, fmt.Errorf("NPC import requires allocator and unimported state")
	}
	next := s.clone()
	next.NPCs = map[string]NPCState{}
	roomIDs := make([]int, 0, len(s.Rooms))
	for id := range s.Rooms {
		roomIDs = append(roomIDs, int(id))
	}
	sort.Ints(roomIDs)
	for _, key := range roomIDs {
		roomID := int16(key)
		room := next.Rooms[roomID]
		for _, body := range room.Resource.Monsters {
			id, err := allocate()
			if err != nil {
				return State{}, err
			}
			if _, exists := next.NPCs[id]; id == "" || exists {
				return State{}, fmt.Errorf("duplicate or empty NPC ID")
			}
			body.RoomID = roomID
			next.NPCs[id] = NPCState{Body: body}
			room.NPCIDs = append(room.NPCIDs, id)
		}
		room.Resource.Monsters = nil
		next.Rooms[roomID] = room
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// ImportNPCItems is the explicit one-time migration from canonical NPC
// identities whose bodies still carry legacy nested inventory values. NPC IDs
// are visited in room order, then the existing room slice order, so allocator
// replay is deterministic. A failed migration returns no partial snapshot.
func (s State) ImportNPCItems(allocate func() (string, error)) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if s.NPCs == nil || allocate == nil {
		return State{}, fmt.Errorf("NPC item import requires canonical NPCs and allocator")
	}
	for id, npc := range s.NPCs {
		if npc.Items != nil {
			return State{}, fmt.Errorf("NPC %s already has canonical items", id)
		}
	}

	next := s.clone()
	roomIDs := make([]int, 0, len(next.Rooms))
	for id := range next.Rooms {
		roomIDs = append(roomIDs, int(id))
	}
	sort.Ints(roomIDs)
	seen := map[string]bool{}
	for _, key := range roomIDs {
		roomID := int16(key)
		for _, npcID := range next.Rooms[roomID].NPCIDs {
			if seen[npcID] {
				continue
			}
			seen[npcID] = true
			npc, ok := next.NPCs[npcID]
			if !ok {
				return State{}, fmt.Errorf("NPC %s absent during item migration", npcID)
			}
			items, err := ImportItems(npc.Body.Inventory, allocate)
			if err != nil {
				return State{}, fmt.Errorf("NPC %s item migration: %w", npcID, err)
			}
			npc.Body.Inventory = nil
			npc.Items = &items
			next.NPCs[npcID] = npc
		}
	}
	if len(seen) != len(next.NPCs) {
		return State{}, fmt.Errorf("NPC item migration found unlisted identity")
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// ProjectRoom is a read-only value projection. It does not refresh resources,
// allocate IDs or resolve combat relationships, and must not be persisted as
// Resource while canonical NPC state exists.
func (s State) ProjectRoom(id int16) (LegacyRoom, error) {
	if err := s.Validate(); err != nil {
		return LegacyRoom{}, err
	}
	room, ok := s.Rooms[id]
	if !ok {
		return LegacyRoom{}, fmt.Errorf("room absent")
	}
	resource := cloneRoom(room.Resource)
	if s.NPCs != nil {
		for _, npcID := range room.NPCIDs {
			npc := s.NPCs[npcID]
			body := npc.Body
			if npc.Items != nil {
				inventory, err := npc.Items.LegacyInventory()
				if err != nil {
					return LegacyRoom{}, err
				}
				body.Inventory = inventory
			} else {
				body.Inventory = cloneObjects(body.Inventory)
			}
			resource.Monsters = append(resource.Monsters, body)
		}
	}
	return resource, nil
}

// NPCMovementRelations preserves the differing C predicates: combat checks
// find_enm_crt >= 0, while the stealth blocker checks membership alone.
// Both outputs use the exact source NPC order, not a client-supplied index map.
func (s State) NPCMovementRelations(actorID string) (bool, []bool, error) {
	if err := s.Validate(); err != nil {
		return false, nil, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || s.NPCs == nil {
		return false, nil, fmt.Errorf("canonical NPC movement context required")
	}
	ids := s.Rooms[actor.Body.RoomID].NPCIDs
	enemies := make([]bool, len(ids))
	fighting := false
	for i, id := range ids {
		npc := s.NPCs[id]
		if npc.Enemies == nil {
			return false, nil, fmt.Errorf("NPC enemy relations unresolved")
		}
		for _, enemy := range npc.Enemies {
			if enemy.Target == (EntityRef{Kind: "player", ID: actorID}) {
				enemies[i] = true
				fighting = fighting || enemy.Damage >= 0
			}
		}
	}
	return fighting, enemies, nil
}
