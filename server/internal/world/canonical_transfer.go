package world

import "fmt"

// TransferWithIDs plans and applies against one authoritative snapshot. Resource
// and occupant copies supplied in in are replaced with this snapshot's values.
// Actor permissions are projected from the saved body, and canonical items
// override weight/fall/dexterity. NPC relationships, invitations and game time
// remain server-derived inputs, never client claims.
// The returned candidate still requires the executor's atomic durable commit.
func (s State) TransferWithIDs(in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, TransferProposal, error) {
	return s.transferWithPlanner(in, catalog, roll, allocate, PlanMovement)
}

// DirectionalTransferWithIDs uses move's semantics with canonical room/items.
// It returns one actor's intermediate candidate without consuming arrival-trap
// RNG. DirectionalStep owns trap timing, recursive followers and lethal effects.
func (s State) DirectionalTransferWithIDs(in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, TransferProposal, error) {
	return s.transferWithPlanner(in, catalog, roll, allocate, PlanDirectionalMovement)
}

func (s State) transferWithPlanner(in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error), move func(MovementInput, func(int, int) int) (MovementProposal, error)) (State, TransferProposal, error) {
	if err := s.Validate(); err != nil {
		return State{}, TransferProposal{}, err
	}
	actor, ok := s.Players[in.ActorID]
	if !ok || !actor.Online {
		return State{}, TransferProposal{}, fmt.Errorf("missing online actor")
	}
	var err error
	in.Movement.Source, err = s.ProjectRoom(actor.Body.RoomID)
	if err != nil {
		return State{}, TransferProposal{}, err
	}
	if s.NPCs != nil {
		in.Movement.Traversal.Fighting, in.Movement.EnemyOfPlayer, err = s.NPCMovementRelations(in.ActorID)
		if err != nil {
			return State{}, TransferProposal{}, err
		}
	}
	in.Movement.Traversal.HP = int(actor.Body.HPCurrent)
	in.Movement.Hidden = flag(actor.Body.Flags[:], 1)
	f := actor.Body.Flags[:]
	in.Movement.Traversal.Class = actor.Body.Class
	in.Movement.Visitor.Class = actor.Body.Class
	in.Movement.Visitor.Level = actor.Body.Level
	in.Movement.Traversal.Immobile = flag(f, 44)
	in.Movement.Traversal.Flying = flag(f, 31)
	in.Movement.Traversal.Invisible = flag(f, 2)
	in.Movement.Traversal.Male = flag(f, 12)
	in.Movement.Traversal.Levitating = flag(f, 25)
	in.Movement.Blind = flag(f, 42)
	in.Movement.DetectInvisible = flag(f, 21)
	in.Movement.Visitor.FamilyMember = flag(f, 55)
	in.Movement.Visitor.FamilyID = int16(actor.Body.Daily[9].Max)
	in.Movement.Visitor.MarriageID = int16(actor.Body.Daily[8].Max)
	if actor.Items != nil {
		stats, err := actor.Items.MovementStats(actor.Body.Stats[1])
		if err != nil {
			return State{}, TransferProposal{}, err
		}
		in.Movement.Traversal.CarriedWeight = stats.CarriedWeight
		in.Movement.Traversal.FallSkill = stats.FallSkill
		in.Movement.DexterityBonus = stats.DexterityBonus
	}
	in.SourcePlayers, err = s.RoomPlayers(actor.Body.RoomID)
	if err != nil {
		return State{}, TransferProposal{}, err
	}
	if in.Movement.Destination != nil {
		room, ok := s.Rooms[in.Movement.Destination.ID]
		if !ok {
			return State{}, TransferProposal{}, fmt.Errorf("destination absent")
		}
		projected, err := s.ProjectRoom(room.Resource.ID)
		if err != nil {
			return State{}, TransferProposal{}, err
		}
		in.Movement.Destination = &projected
		if s.Invitations != nil {
			in.Movement.Visitor.Invited = false
			for _, id := range s.Invitations[room.Resource.Special] {
				if id == in.ActorID {
					in.Movement.Visitor.Invited = true
					break
				}
			}
		}
		in.Movement.Occupants, err = s.RoomPlayers(room.Resource.ID)
		if err != nil {
			return State{}, TransferProposal{}, err
		}
	}
	var destinationItems *ItemCollection
	var npcDelta *npcEntryDelta
	p, err := planTransferMovement(in, roll, func(resource LegacyRoom, players []RoomPlayerView, entrant RoomPlayerView) (RoomEntry, error) {
		room := s.Rooms[resource.ID]
		if s.NPCs != nil {
			next, entry, delta, err := planCanonicalNPCEntry(room, s.NPCs, players, entrant, in.View, catalog, int32(in.Movement.Now), roll, allocate)
			if err == nil {
				destinationItems = next.Items
				npcDelta = delta
			}
			return entry, err
		}
		room.Resource = resource
		if room.Items == nil {
			return PlanRoomEntry(resource, players, entrant, in.View, catalog, int32(in.Movement.Now), roll)
		}
		next, entry, err := PlanCanonicalRoomEntry(room, players, entrant, in.View, catalog, int32(in.Movement.Now), roll, allocate)
		if err == nil {
			destinationItems = next.Items
		}
		return entry, err
	}, move)
	if err != nil {
		return State{}, TransferProposal{}, err
	}
	next, err := s.applyTransfer(in.ActorID, p, true, destinationItems, npcDelta)
	if err != nil {
		return State{}, TransferProposal{}, err
	}
	// Render from the completed destination snapshot, including refreshed floor
	// items and its occupants' lights, not source-room/caller capabilities.
	if p.Entry != nil && actor.Items != nil {
		p.Entry.Scene, err = next.CurrentScene(in.ActorID, in.View.Hour)
		if err != nil {
			return State{}, TransferProposal{}, err
		}
	}
	return next, p, nil
}

// attachArrivalTrap defers check_traps RNG until recursive followers and NPC
// movement have completed. Public transfer helpers intentionally do not attach
// this effect: persisting their intermediate candidate without the complete
// command boundary must not silently consume or skip a trap.
func (s State) attachArrivalTrap(actorID string, p *TransferProposal, roll func(int, int) int) error {
	if p.Entry == nil || p.ArrivalTrap != nil {
		return nil
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.RoomID != p.Entry.Room.ID {
		return fmt.Errorf("arrival trap actor is not in destination")
	}
	trap, err := PlanArrivalTrap(s.Rooms[actor.Body.RoomID].Resource, ArrivalTrapInput{
		HP:           int(actor.Body.HPCurrent),
		HPMax:        int(actor.Body.HPMax),
		MP:           int(actor.Body.MPCurrent),
		MPMax:        int(actor.Body.MPMax),
		Dexterity:    int(int8(actor.Body.Stats[1])),
		Intelligence: int(int8(actor.Body.Stats[3])),
		Levitating:   flag(actor.Body.Flags[:], 25),
		Prepared:     flag(actor.Body.Flags[:], 24),
	}, roll)
	if err != nil {
		return err
	}
	p.ArrivalTrap = &trap
	return nil
}
