package world

import "fmt"

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func removeString(values []string, want string) ([]string, bool) {
	for i, value := range values {
		if value == want {
			if values == nil {
				return nil, true
			}
			out := make([]string, 0, len(values)-1)
			out = append(out, values[:i]...)
			out = append(out, values[i+1:]...)
			return out, true
		}
	}
	if values == nil {
		return nil, false
	}
	return append([]string{}, values...), false
}

func removeFollowerRef(values []EntityRef, want EntityRef) ([]EntityRef, bool) {
	for i, value := range values {
		if value == want {
			out := make([]EntityRef, 0, len(values)-1)
			out = append(out, values[:i]...)
			out = append(out, values[i+1:]...)
			return out, true
		}
	}
	return append([]EntityRef(nil), values...), false
}

// mixedFollowerRefs reconstructs a deterministic order only for legacy
// snapshots that have not imported the C first_fol interleaving. Once a
// nonnil FollowerRefs slice exists, it is authoritative and is never inferred
// from display names or map iteration.
func mixedFollowerRefs(player PlayerState) []EntityRef {
	if player.FollowerRefs != nil {
		return append([]EntityRef{}, player.FollowerRefs...)
	}
	refs := make([]EntityRef, 0, len(player.FollowerIDs)+len(player.NPCFollowerIDs))
	for _, id := range player.FollowerIDs {
		refs = append(refs, EntityRef{Kind: "player", ID: id})
	}
	for _, id := range player.NPCFollowerIDs {
		refs = append(refs, EntityRef{Kind: "npc", ID: id})
	}
	return refs
}

func prependFollowerRef(player PlayerState, ref EntityRef) []EntityRef {
	refs := mixedFollowerRefs(player)
	if without, removed := removeFollowerRef(refs, ref); removed {
		refs = without
	}
	return append([]EntityRef{ref}, refs...)
}

// FollowPlayer ports command4.c: a player has one leader, new followers are
// inserted at the head, and cycles/self-follow are rejected before any state
// changes. Same-room/online checks are authority checks, never client claims.
func (s State) FollowPlayer(followerID, leaderID string) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	follower, ok := s.Players[followerID]
	if !ok || !follower.Online {
		return State{}, fmt.Errorf("missing online follower")
	}
	leader, ok := s.Players[leaderID]
	if !ok || !leader.Online {
		return State{}, fmt.Errorf("missing online leader")
	}
	if followerID == leaderID || follower.Body.RoomID != leader.Body.RoomID {
		return State{}, fmt.Errorf("cannot follow self or another room")
	}
	for cursor := leaderID; cursor != ""; {
		if cursor == followerID {
			return State{}, fmt.Errorf("following cycle")
		}
		next, ok := s.Players[cursor]
		if !ok {
			return State{}, fmt.Errorf("missing leader chain")
		}
		cursor = next.FollowingID
	}
	next := s.clone()
	follower = next.Players[followerID]
	if follower.FollowingID != "" {
		oldLeaderID := follower.FollowingID
		oldLeader := next.Players[oldLeaderID]
		var removed bool
		oldLeader.FollowerIDs, removed = removeString(oldLeader.FollowerIDs, followerID)
		if !removed {
			return State{}, fmt.Errorf("old following edge is not reciprocal")
		}
		if oldLeader.FollowerRefs != nil {
			oldLeader.FollowerRefs, removed = removeFollowerRef(oldLeader.FollowerRefs, EntityRef{Kind: "player", ID: followerID})
			if !removed {
				return State{}, fmt.Errorf("old mixed following edge is not reciprocal")
			}
		}
		next.Players[oldLeaderID] = oldLeader
	}
	follower.FollowingID = leaderID
	next.Players[followerID] = follower
	leader = next.Players[leaderID]
	leader.FollowerIDs = append([]string{followerID}, leader.FollowerIDs...)
	leader.FollowerRefs = prependFollowerRef(leader, EntityRef{Kind: "player", ID: followerID})
	next.Players[leaderID] = leader
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// UnfollowPlayer removes one follower from its leader while preserving the
// remaining first_fol order. It is the state counterpart of lose with no
// broadcast side effects.
func (s State) UnfollowPlayer(followerID string) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	follower, ok := s.Players[followerID]
	if !ok || follower.FollowingID == "" {
		return State{}, fmt.Errorf("player is not following")
	}
	leader, ok := s.Players[follower.FollowingID]
	if !ok {
		return State{}, fmt.Errorf("following leader absent")
	}
	next := s.clone()
	leaderID := follower.FollowingID
	leader = next.Players[leaderID]
	var removed bool
	leader.FollowerIDs, removed = removeString(leader.FollowerIDs, followerID)
	if !removed {
		return State{}, fmt.Errorf("following edge is not reciprocal")
	}
	if leader.FollowerRefs != nil {
		leader.FollowerRefs, removed = removeFollowerRef(leader.FollowerRefs, EntityRef{Kind: "player", ID: followerID})
		if !removed {
			return State{}, fmt.Errorf("mixed following edge is not reciprocal")
		}
	}
	next.Players[leaderID] = leader
	follower = next.Players[followerID]
	follower.FollowingID = ""
	next.Players[followerID] = follower
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// FollowNPCToPlayer records the monster portion of legacy first_fol. The NPC
// must already be canonical and in the same room; insertion is at the head so
// command2/command6 can replay the original ordered traversal. It does not
// force MDMFOL: the legacy flag controls whether movement is applied.
func (s State) FollowNPCToPlayer(npcID, playerID string) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	player, ok := s.Players[playerID]
	if !ok || !player.Online {
		return State{}, fmt.Errorf("missing online NPC follower leader")
	}
	npc, ok := s.NPCs[npcID]
	if !ok || npc.Body.RoomID != player.Body.RoomID || npc.Body.Type != 1 {
		return State{}, fmt.Errorf("missing same-room NPC follower")
	}
	next := s.clone()
	if npc.FollowingPlayerID != "" {
		old := next.Players[npc.FollowingPlayerID]
		var removed bool
		old.NPCFollowerIDs, removed = removeString(old.NPCFollowerIDs, npcID)
		if !removed {
			return State{}, fmt.Errorf("old NPC following edge is not reciprocal")
		}
		if old.FollowerRefs != nil {
			old.FollowerRefs, removed = removeFollowerRef(old.FollowerRefs, EntityRef{Kind: "npc", ID: npcID})
			if !removed {
				return State{}, fmt.Errorf("old mixed NPC following edge is not reciprocal")
			}
		}
		next.Players[npc.FollowingPlayerID] = old
	}
	npc = next.NPCs[npcID]
	npc.FollowingPlayerID = playerID
	next.NPCs[npcID] = npc
	player = next.Players[playerID]
	player.NPCFollowerIDs = append([]string{npcID}, player.NPCFollowerIDs...)
	player.FollowerRefs = prependFollowerRef(player, EntityRef{Kind: "npc", ID: npcID})
	next.Players[playerID] = player
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// UnfollowNPCFromPlayer removes the NPC from its canonical first_fol owner.
func (s State) UnfollowNPCFromPlayer(npcID string) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	npc, ok := s.NPCs[npcID]
	if !ok || npc.FollowingPlayerID == "" {
		return State{}, fmt.Errorf("NPC is not following a player")
	}
	player, ok := s.Players[npc.FollowingPlayerID]
	if !ok {
		return State{}, fmt.Errorf("NPC follower leader absent")
	}
	next := s.clone()
	player = next.Players[npc.FollowingPlayerID]
	var removed bool
	player.NPCFollowerIDs, removed = removeString(player.NPCFollowerIDs, npcID)
	if !removed {
		return State{}, fmt.Errorf("NPC following edge is not reciprocal")
	}
	if player.FollowerRefs != nil {
		player.FollowerRefs, removed = removeFollowerRef(player.FollowerRefs, EntityRef{Kind: "npc", ID: npcID})
		if !removed {
			return State{}, fmt.Errorf("mixed NPC following edge is not reciprocal")
		}
	}
	next.Players[npc.FollowingPlayerID] = player
	npc = next.NPCs[npcID]
	npc.FollowingPlayerID = ""
	next.NPCs[npcID] = npc
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// clearNPCFollowLock drops FollowingPlayerID together with persisted MDMFOL.
// C discards in-memory monsters on process death; Go snapshots keep Body.Flags,
// so logout and RecoverOffline must F_CLR bit 46 or *따르기 reappears after boot.
func clearNPCFollowLock(npc NPCState) NPCState {
	npc.FollowingPlayerID = ""
	npc.Body = setNPCFlag(npc.Body, npcDMFollowFlag, false)
	return npc
}

func (s State) requireResolvedNPCFollowers(actorID string, actor PlayerState) error {
	if len(actor.NPCFollowerIDs) > 0 && s.ActiveNPCIDs == nil {
		return fmt.Errorf("logout requires resolved NPC active order")
	}
	for _, npcID := range actor.NPCFollowerIDs {
		npc, ok := s.NPCs[npcID]
		if !ok {
			return fmt.Errorf("NPC follower absent")
		}
		if npc.Enemies == nil {
			return fmt.Errorf("logout requires resolved NPC follower enemies")
		}
		if npc.FollowingPlayerID != actorID {
			return fmt.Errorf("NPC following edge is not reciprocal")
		}
	}
	return nil
}

// DetachPlayerRelationships follows uninit_ply's relationship cleanup. It is
// used before logout/recovery so a stale socket cannot leave a persisted group
// edge pointing at an offline player. MDMFOL is cleared with the reciprocal
// first_fol edge because Go persists NPC flags; C's in-memory monster would
// otherwise keep following after the socket is gone.
func (s State) DetachPlayerRelationships(actorID string) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if _, ok := s.Players[actorID]; !ok {
		return State{}, fmt.Errorf("player absent")
	}
	next := s.clone()
	actor := next.Players[actorID]
	if actor.FollowingID != "" {
		leader := next.Players[actor.FollowingID]
		var removed bool
		leader.FollowerIDs, removed = removeString(leader.FollowerIDs, actorID)
		if !removed {
			return State{}, fmt.Errorf("actor following edge is not reciprocal")
		}
		if leader.FollowerRefs != nil {
			leader.FollowerRefs, removed = removeFollowerRef(leader.FollowerRefs, EntityRef{Kind: "player", ID: actorID})
			if !removed {
				return State{}, fmt.Errorf("actor mixed following edge is not reciprocal")
			}
		}
		next.Players[actor.FollowingID] = leader
		actor.FollowingID = ""
	}
	for _, followerID := range append([]string(nil), actor.FollowerIDs...) {
		follower, ok := next.Players[followerID]
		if !ok {
			return State{}, fmt.Errorf("follower absent")
		}
		follower.FollowingID = ""
		next.Players[followerID] = follower
	}
	actor.FollowerIDs = nil
	if err := next.requireResolvedNPCFollowers(actorID, actor); err != nil {
		return State{}, err
	}
	for _, npcID := range append([]string(nil), actor.NPCFollowerIDs...) {
		next.NPCs[npcID] = clearNPCFollowLock(next.NPCs[npcID])
	}
	actor.NPCFollowerIDs = nil
	actor.FollowerRefs = nil
	next.Players[actorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}
