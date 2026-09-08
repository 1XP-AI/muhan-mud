package world

import "fmt"

// TransferInput must come from one authoritative world revision. View contains
// server-derived display capabilities, never browser-provided permissions.
type TransferInput struct {
	Movement      MovementInput
	ActorID       string
	SourcePlayers []RoomPlayerView
	View          SceneOptions
}

// TransferProposal is a single replacement candidate, not a committed move.
// Persist actor HP/hidden/location, Source, SourcePlayers and Entry together.
// A normal denied move may still change HP/hidden/track; Entry is then nil.
// Internal errors expose no candidate. The complete command orchestration
// (event formatting and combat tick) remains outside this intermediate value
// proposal. The durable DirectionalStep boundary additionally applies
// recursive followers, command2's ordered MFOLLO chase, ALARM NPC effects and
// the arrival trap before the receipt is published.
type TransferProposal struct {
	Movement      MovementProposal
	Source        LegacyRoom
	SourcePlayers []RoomPlayerView
	Entry         *RoomEntry
	// ArrivalTrap is computed only after destination admission succeeds.  A
	// denied/blocked/fall-stopped move must leave it nil, matching C's
	// check_traps call at the end of move().
	ArrivalTrap              *ArrivalTrapResult
	DeactivateSourceMonsters bool
}

func cloneRoom(room LegacyRoom) LegacyRoom {
	out := room
	out.Exits = append([]LegacyExit(nil), room.Exits...)
	out.Objects = cloneObjects(room.Objects)
	out.Monsters = append([]LegacyMonster(nil), room.Monsters...)
	for i := range out.Monsters {
		out.Monsters[i].Inventory = cloneObjects(room.Monsters[i].Inventory)
	}
	return out
}

func PlanTransfer(in TransferInput, catalog SpawnCatalog, roll func(int, int) int) (TransferProposal, error) {
	return planTransfer(in, roll, func(room LegacyRoom, players []RoomPlayerView, actor RoomPlayerView) (RoomEntry, error) {
		return PlanRoomEntry(room, players, actor, in.View, catalog, int32(in.Movement.Now), roll)
	})
}

func planTransfer(in TransferInput, roll func(int, int) int, enter func(LegacyRoom, []RoomPlayerView, RoomPlayerView) (RoomEntry, error)) (TransferProposal, error) {
	return planTransferMovement(in, roll, enter, PlanMovement)
}

func planTransferMovement(in TransferInput, roll func(int, int) int, enter func(LegacyRoom, []RoomPlayerView, RoomPlayerView) (RoomEntry, error), move func(MovementInput, func(int, int) int) (MovementProposal, error)) (TransferProposal, error) {
	departure, err := PlanRoomDeparture(in.SourcePlayers, in.ActorID)
	if err != nil {
		return TransferProposal{}, err
	}
	var actor RoomPlayerView
	for _, p := range in.SourcePlayers {
		if p.ID == in.ActorID {
			actor = p
			break
		}
	}
	if flag(actor.Flags[:], 1) != in.Movement.Hidden {
		return TransferProposal{}, fmt.Errorf("inconsistent actor hidden state")
	}
	// The resource timers are still the legacy signed 32-bit representation.
	// Reject narrowing until the runtime timer migration is implemented.
	if in.Movement.Now < -2147483648 || in.Movement.Now > 2147483647 {
		return TransferProposal{}, fmt.Errorf("room entry time outside legacy range")
	}
	seen := map[string]bool{}
	for _, p := range in.SourcePlayers {
		seen[p.ID] = true
	}
	for _, p := range in.Movement.Occupants {
		if p.ID == "" || p.ID == in.ActorID || seen[p.ID] {
			return TransferProposal{}, fmt.Errorf("invalid destination membership")
		}
		seen[p.ID] = true
	}
	movement, err := move(in.Movement, roll)
	if err != nil {
		return TransferProposal{}, err
	}
	actor.Flags[0] &^= 2
	if movement.Hidden {
		actor.Flags[0] |= 2
	}
	out := TransferProposal{Movement: movement, Source: cloneRoom(in.Movement.Source)}
	out.Source.Track = movement.Track
	if !movement.Moved {
		out.SourcePlayers = append([]RoomPlayerView(nil), in.SourcePlayers...)
		for i := range out.SourcePlayers {
			if out.SourcePlayers[i].ID == actor.ID {
				out.SourcePlayers[i] = actor
			}
		}
		return out, nil
	}
	entry, err := enter(*in.Movement.Destination, in.Movement.Occupants, actor)
	if err != nil {
		return TransferProposal{}, err
	}
	out.SourcePlayers = departure.Players
	out.DeactivateSourceMonsters = departure.DeactivateMonsters
	out.Entry = &entry
	return out, nil
}
