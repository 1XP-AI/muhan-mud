package world

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

// State is the versioned, internal world snapshot under construction. Body and
// Resource still use migration numeric types. This is NOT a complete legacy
// player import schema: item migration admission, relationships and live NPC
// identities remain incomplete. Credentials never belong here.
type State struct {
	Version int
	Rooms   map[int16]RoomState
	Players map[string]PlayerState
	// Nil means the legacy post directory has not been imported. A nonnil map
	// is the canonical, per-recipient ordered mailbox projection. Mailbox
	// ordering is the slice order; it must never be reconstructed from map
	// iteration or a client supplied name.
	Mailboxes map[string][]MailMessage
	// Nil means board indexes have not been imported. A nonnil BoardState is
	// the canonical ordered board/post projection and is validated as part of
	// the same world snapshot.
	Boards *BoardState
	// BankAccounts is the canonical Go representation of the legacy per-player
	// bank file. A missing map is an unimported bank domain; an account with a
	// zero balance is still explicit. Item storage is kept separate until the
	// object-transfer slice is admitted.
	BankAccounts map[string]BankAccount
	// Nil means family-war state has not been imported, not confirmed peace.
	War *FamilyWar
	// Nil means legacy family_member_<n> ledgers have not been imported. A
	// non-nil FamilyState with an empty member slice is an explicit empty
	// ledger, never an invitation to discover members from player flags.
	Family *FamilyState
	// Nil means legacy family_news_<n> files have not been imported. A non-nil
	// FamilyNewsState with a missing family key is C's missing notice file.
	FamilyNews *FamilyNewsState
	// Property special number -> invited character IDs (legacy invite_N, 10 slots).
	// Nil is unimported; a nonnil empty map explicitly means no invitations.
	Invitations map[int16][]string
	// Nil means the legacy player/vote/<name>_v domain has not been imported.
	// A nonnil VoteState is the canonical active-ballot plus append-only vote
	// history aggregate; its maps/slices retain their own nil-vs-empty markers.
	// Vote reducers never consult the legacy files as a fallback.
	Votes *VoteState
	// Nil means the legacy player/fal memo files have not been imported. A
	// nonnil map is the canonical ordered memo append projection keyed by the
	// recipient's immutable player ID. Memo writers never consult or construct
	// a raw player path and never carry credentials.
	Memos map[string][]CharacterMemo
	// Nil means the legacy DM notepad has not been imported. A nonnil slice is
	// the canonical ordered file-line projection; an empty nonnil slice means
	// the legacy file is absent. Lines contain no newline terminator so the
	// projection remains pointer-free and can be rendered deterministically.
	Notepad []string
	// Nil is pre-migration. Canonical NPC bodies are owned here, not by rooms.
	NPCs map[string]NPCState
	// Global C first_active order. Nil is unresolved; [] is known inactive.
	ActiveNPCIDs []string
}

// BankAccount contains the money and, once migrated, the ordered bank object
// graph for one character. Balance mutations are always performed together
// with the player body in one world receipt.
type BankAccount struct {
	Balance int64
	Items   *ItemCollection
}

type RoomState struct {
	Resource LegacyRoom
	// Nil is pre-migration; otherwise Resource.Objects must be empty.
	// Inventory roots are floor roots. A room cannot equip items.
	Items *ItemCollection
	// Ordered identities, not duplicate copies of player attributes.
	PlayerIDs []string
	NPCIDs    []string
}

type PlayerState struct {
	Body   LegacyMonster
	Online bool
	// Title is the canonical player-selected title from alias.c. An empty
	// value means no custom title; legacy alias files are never consulted by
	// gameplay reducers.
	Title string
	// Aliases is the canonical ordered alias.c list. A nil slice means the
	// alias domain has not been imported for this player; a nonnil empty slice
	// is an explicitly known empty list. Alias expansion is not performed by
	// Go command reducers until its substitution contract is admitted.
	Aliases []PlayerAlias
	// FollowingID is the single leader pointer from creature.following.
	// FollowerIDs preserves first_fol head-insertion order, so movement can
	// replay C's recursive follower batch without matching by display name.
	FollowingID string
	FollowerIDs []string
	// NPCFollowerIDs preserves the monster entries in this player's legacy
	// first_fol list. Nil remains unresolved; a nonnil empty slice is a known
	// empty monster-follower list. Entries are head-inserted like FollowerIDs.
	NPCFollowerIDs []string
	// FollowerRefs is the canonical mixed C first_fol order once imported. Nil
	// means the legacy interleaving was not resolved; a nonnil slice is the
	// authoritative head-to-tail order across player and NPC followers. The
	// category-specific lists above remain indexes for their existing reducers.
	FollowerRefs []EntityRef
	// Nil means inventory has not yet undergone explicit ID-based migration.
	Items *ItemCollection
	// Player-only enemy identities; NPC relationships require their own IDs.
	PlayerEnemies []string
	// CharmRefs is the canonical ordered first_charm projection for this
	// player. Nil means the legacy charm relation is unresolved; a nonnil
	// empty slice is a known empty list. Entries retain C add_charm_crt's
	// head-insertion order and resolve only through immutable player/NPC IDs.
	CharmRefs []EntityRef
}

// Validate checks cross-room referential integrity, not all gameplay rules.
// Offline characters retain a saved location but occupy no room. Online status
// must be reconciled on process recovery before sessions can issue commands.
func (s State) Validate() error {
	if s.Version != 1 || s.Rooms == nil || s.Players == nil {
		return fmt.Errorf("unsupported or incomplete world snapshot")
	}
	if err := s.validateNPCs(); err != nil {
		return err
	}
	if err := s.validateCharmRelations(); err != nil {
		return err
	}
	if err := s.validateMailboxes(); err != nil {
		return err
	}
	if err := s.validateMemos(); err != nil {
		return err
	}
	if err := s.validateNotepad(); err != nil {
		return err
	}
	if s.Boards != nil {
		if err := s.Boards.Validate(); err != nil {
			return fmt.Errorf("invalid board state: %w", err)
		}
	}
	if s.Votes != nil {
		if err := s.Votes.Validate(); err != nil {
			return fmt.Errorf("invalid vote state: %w", err)
		}
		for actorID := range s.Votes.Ballots {
			if _, ok := s.Players[actorID]; !ok {
				return fmt.Errorf("%w: vote ballot actor absent", ErrVoteStateInvalid)
			}
		}
		for _, entry := range s.Votes.History {
			if _, ok := s.Players[entry.ActorID]; !ok {
				return fmt.Errorf("%w: vote history actor absent", ErrVoteStateInvalid)
			}
		}
	}
	if s.Family != nil {
		if err := s.Family.Validate(); err != nil {
			return fmt.Errorf("invalid family state: %w", err)
		}
		for familyID, members := range s.Family.Members {
			for _, member := range members {
				player, ok := s.Players[member.ID]
				if !ok || player.Body.Name != member.Name || player.Body.Class != member.Class {
					return fmt.Errorf("invalid family member identity %q in family %d", member.ID, familyID)
				}
			}
		}
	}
	if s.FamilyNews != nil {
		if err := s.FamilyNews.Validate(); err != nil {
			return fmt.Errorf("invalid family news: %w", err)
		}
	}
	for _, ids := range s.Invitations {
		if len(ids) > 10 {
			return fmt.Errorf("too many property invitations")
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if _, ok := s.Players[id]; !ok || id == "" || seen[id] {
				return fmt.Errorf("invalid invitation identity")
			}
			seen[id] = true
		}
	}
	seen := map[string]bool{}
	itemOwners := map[string]bool{}
	if s.NPCs != nil {
		for npcID, npc := range s.NPCs {
			if npc.Items == nil {
				continue
			}
			if len(npc.Body.Inventory) != 0 {
				return fmt.Errorf("duplicate legacy and canonical NPC inventory")
			}
			if err := npc.Items.Validate(); err != nil {
				return err
			}
			for itemID := range npc.Items.Items {
				if itemOwners[itemID] {
					return fmt.Errorf("item owned by multiple entities")
				}
				itemOwners[itemID] = true
			}
			if npcID == "" {
				return fmt.Errorf("empty NPC item owner")
			}
		}
	}
	for characterID, account := range s.BankAccounts {
		if characterID == "" || account.Balance < 0 || account.Balance > MaxBankBalance {
			return fmt.Errorf("invalid bank account")
		}
		if _, ok := s.Players[characterID]; !ok {
			return fmt.Errorf("bank account owner absent")
		}
		if account.Items == nil {
			continue
		}
		if err := account.Items.Validate(); err != nil {
			return err
		}
		for itemID := range account.Items.Items {
			if itemOwners[itemID] {
				return fmt.Errorf("item owned by multiple entities")
			}
			itemOwners[itemID] = true
		}
	}
	for id, room := range s.Rooms {
		if room.Resource.ID != id {
			return fmt.Errorf("room key mismatch")
		}
		if room.Items != nil {
			if len(room.Resource.Objects) != 0 {
				return fmt.Errorf("duplicate legacy and canonical floor")
			}
			if err := room.Items.Validate(); err != nil {
				return err
			}
			for _, equipped := range room.Items.Ready {
				if equipped != "" {
					return fmt.Errorf("room cannot equip items")
				}
			}
			for itemID := range room.Items.Items {
				if itemOwners[itemID] {
					return fmt.Errorf("item owned by multiple rooms")
				}
				itemOwners[itemID] = true
			}
		}
		for _, playerID := range room.PlayerIDs {
			p, ok := s.Players[playerID]
			if !ok || playerID == "" || seen[playerID] || !p.Online || p.Body.RoomID != id {
				return fmt.Errorf("inconsistent room membership")
			}
			seen[playerID] = true
		}
	}
	for id, p := range s.Players {
		if p.FollowingID == id {
			return fmt.Errorf("player follows self")
		}
		if p.FollowingID != "" {
			leader, ok := s.Players[p.FollowingID]
			if !ok || !containsString(leader.FollowerIDs, id) {
				return fmt.Errorf("following edge is not reciprocal")
			}
		}
		followers := map[string]bool{}
		for _, followerID := range p.FollowerIDs {
			follower, ok := s.Players[followerID]
			if !ok || followerID == "" || followerID == id || followers[followerID] || follower.FollowingID != id {
				return fmt.Errorf("invalid follower edge")
			}
			followers[followerID] = true
		}
		if p.FollowerRefs != nil {
			refs := map[EntityRef]bool{}
			for _, ref := range p.FollowerRefs {
				if ref.ID == "" || refs[ref] {
					return fmt.Errorf("invalid mixed follower order")
				}
				switch ref.Kind {
				case "player":
					follower, ok := s.Players[ref.ID]
					if !ok || follower.FollowingID != id || !containsString(p.FollowerIDs, ref.ID) {
						return fmt.Errorf("mixed follower player edge is not reciprocal")
					}
				case "npc":
					if s.NPCs == nil {
						return fmt.Errorf("mixed NPC follower order without canonical NPCs")
					}
					npc, ok := s.NPCs[ref.ID]
					if !ok || npc.FollowingPlayerID != id || !containsString(p.NPCFollowerIDs, ref.ID) {
						return fmt.Errorf("mixed follower NPC edge is not reciprocal")
					}
				default:
					return fmt.Errorf("unknown mixed follower kind")
				}
				refs[ref] = true
			}
			if len(refs) != len(p.FollowerIDs)+len(p.NPCFollowerIDs) {
				return fmt.Errorf("mixed follower order is incomplete")
			}
			for _, followerID := range p.FollowerIDs {
				if !refs[EntityRef{Kind: "player", ID: followerID}] {
					return fmt.Errorf("mixed follower player omitted")
				}
			}
			for _, npcID := range p.NPCFollowerIDs {
				if !refs[EntityRef{Kind: "npc", ID: npcID}] {
					return fmt.Errorf("mixed follower NPC omitted")
				}
			}
		}
		for leaderID, seenLeader := id, map[string]bool{}; leaderID != ""; {
			if seenLeader[leaderID] {
				return fmt.Errorf("follower cycle")
			}
			seenLeader[leaderID] = true
			leader, ok := s.Players[leaderID]
			if !ok {
				return fmt.Errorf("missing following leader")
			}
			leaderID = leader.FollowingID
		}
		enemies := map[string]bool{}
		for _, enemy := range p.PlayerEnemies {
			if _, ok := s.Players[enemy]; !ok || enemy == "" || enemies[enemy] {
				return fmt.Errorf("invalid player enemy identity")
			}
			enemies[enemy] = true
		}
		if id == "" || p.Body.Name == "" || p.Body.Type != 0 {
			return fmt.Errorf("missing player identity")
		}
		if err := validatePlayerAliases(p.Aliases); err != nil {
			return fmt.Errorf("invalid aliases for player %q: %w", id, err)
		}
		if _, ok := s.Rooms[p.Body.RoomID]; !ok {
			return fmt.Errorf("missing player room")
		}
		if p.Online != seen[id] {
			return fmt.Errorf("player occupancy mismatch")
		}
		if p.Items != nil {
			if len(p.Body.Inventory) != 0 {
				return fmt.Errorf("duplicate legacy and canonical inventory")
			}
			if err := p.Items.Validate(); err != nil {
				return err
			}
			for itemID := range p.Items.Items {
				if itemOwners[itemID] {
					return fmt.Errorf("item owned by multiple entities")
				}
				itemOwners[itemID] = true
			}
		}
	}
	return nil
}

// DecodeState fails closed on unknown fields/schema versions instead of
// silently discarding data written by a newer runtime.
func DecodeState(raw []byte) (State, error) {
	var s State
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return State{}, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return State{}, fmt.Errorf("trailing snapshot data")
	}
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	return s, nil
}

func (s State) RoomPlayers(id int16) ([]RoomPlayerView, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	room, ok := s.Rooms[id]
	if !ok {
		return nil, fmt.Errorf("room absent")
	}
	out := make([]RoomPlayerView, 0, len(room.PlayerIDs))
	for _, playerID := range room.PlayerIDs {
		p := s.Players[playerID].Body
		out = append(out, RoomPlayerView{ID: playerID, Name: p.Name, Description: p.Description, Flags: p.Flags})
	}
	return out, nil
}

// ApplyTransfer applies an internal proposal from this exact snapshot. Revision
// checks remain the executor/store's responsibility. No client may supply this
// proposal. NPC activation and ordered output events are not implemented here.
func (s State) ApplyTransfer(actorID string, p TransferProposal) (State, error) {
	return s.applyTransfer(actorID, p, false, nil, nil)
}

func (s State) applyTransfer(actorID string, p TransferProposal, idAware bool, destinationItems *ItemCollection, npcDelta *npcEntryDelta) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || p.Source.ID != actor.Body.RoomID || p.Movement.HP < -32768 || p.Movement.HP > 32767 {
		return State{}, fmt.Errorf("invalid transfer actor or numeric range")
	}
	if p.Movement.Moved != (p.Entry != nil) || (!p.Movement.Moved && p.Movement.RoomID != actor.Body.RoomID) {
		return State{}, fmt.Errorf("inconsistent transfer destination")
	}
	if p.Entry != nil {
		if _, ok := s.Rooms[p.Entry.Room.ID]; !ok || p.Entry.Room.ID != p.Movement.RoomID || p.Entry.Room.ID == actor.Body.RoomID {
			return State{}, fmt.Errorf("invalid entry room")
		}
	}
	// Legacy proposals carry nested object values, not authoritative item IDs.
	// Reject until the canonical refresh/entry adapter supplies ID-aware plans.
	if !idAware && (s.Rooms[p.Source.ID].Items != nil || (p.Entry != nil && s.Rooms[p.Entry.Room.ID].Items != nil)) {
		return State{}, fmt.Errorf("canonical floor requires ID-aware transfer")
	}
	next := s.clone()
	if npcDelta != nil {
		if next.NPCs == nil || p.Entry == nil {
			return State{}, fmt.Errorf("NPC entry delta without canonical arrival")
		}
		if err := next.installNPCSpawns(npcDelta); err != nil {
			return State{}, err
		}
		room := next.Rooms[p.Entry.Room.ID]
		room.NPCIDs = append([]string(nil), npcDelta.IDs...)
		next.Rooms[p.Entry.Room.ID] = room
	}
	actor = next.Players[actorID]
	actor.Body.HPCurrent = int16(p.Movement.HP)
	actor.Body.RoomID = p.Movement.RoomID
	actor.Body.Flags[0] &^= 2
	if p.Movement.Hidden {
		actor.Body.Flags[0] |= 2
	}
	next.Players[actorID] = actor
	applyRoom := func(resource LegacyRoom, views []RoomPlayerView) error {
		ids := make([]string, len(views))
		for i, view := range views {
			player, ok := next.Players[view.ID]
			body := player.Body
			if !ok || view.Name != body.Name || view.Description != body.Description || view.Flags != body.Flags {
				return fmt.Errorf("view differs from canonical player")
			}
			ids[i] = view.ID
		}
		room := next.Rooms[resource.ID]
		if next.NPCs != nil {
			// Ordered IDs, including explicit spawn events, are authoritative.
			// This comparison rejects unsanctioned mutations; it never discovers IDs.
			if len(resource.Monsters) != len(room.NPCIDs) {
				return fmt.Errorf("NPC refresh requires identity events")
			}
			for i, id := range room.NPCIDs {
				if !reflect.DeepEqual(resource.Monsters[i], next.NPCs[id].Body) {
					return fmt.Errorf("NPC projection changed without identity event")
				}
			}
			resource.Monsters = nil
		}
		room.Resource = cloneRoom(resource)
		room.PlayerIDs = ids
		if p.Entry != nil && resource.ID == p.Entry.Room.ID && destinationItems != nil {
			items := destinationItems.clone()
			room.Items = &items
		}
		next.Rooms[resource.ID] = room
		return nil
	}
	if err := applyRoom(p.Source, p.SourcePlayers); err != nil {
		return State{}, err
	}
	if p.Entry != nil {
		if err := applyRoom(p.Entry.Room, p.Entry.Players); err != nil {
			return State{}, err
		}
		if len(next.Rooms[p.Source.ID].PlayerIDs) == 0 {
			next.deactivateRoomNPCs(p.Source.ID)
		}
		next.activateEntryNPCs(p.Entry.Room.ID, len(s.Rooms[p.Entry.Room.ID].PlayerIDs) == 0, npcDelta)
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

func (s State) clone() State {
	next := State{Version: s.Version, Rooms: make(map[int16]RoomState, len(s.Rooms)), Players: make(map[string]PlayerState, len(s.Players))}
	if s.Mailboxes != nil {
		next.Mailboxes = make(map[string][]MailMessage, len(s.Mailboxes))
		for recipientID, messages := range s.Mailboxes {
			if messages == nil {
				next.Mailboxes[recipientID] = nil
				continue
			}
			next.Mailboxes[recipientID] = append([]MailMessage{}, messages...)
		}
	}
	if s.Memos != nil {
		next.Memos = make(map[string][]CharacterMemo, len(s.Memos))
		for recipientID, records := range s.Memos {
			next.Memos[recipientID] = copyMemos(records)
		}
	}
	if s.Notepad != nil {
		next.Notepad = append([]string{}, s.Notepad...)
	}
	if s.Boards != nil {
		boards := s.Boards.Clone()
		next.Boards = &boards
	}
	if s.BankAccounts != nil {
		next.BankAccounts = make(map[string]BankAccount, len(s.BankAccounts))
		for characterID, account := range s.BankAccounts {
			if account.Items != nil {
				items := account.Items.clone()
				account.Items = &items
			}
			next.BankAccounts[characterID] = account
		}
	}
	if s.ActiveNPCIDs != nil {
		next.ActiveNPCIDs = append([]string{}, s.ActiveNPCIDs...)
	}
	if s.NPCs != nil {
		next.NPCs = make(map[string]NPCState, len(s.NPCs))
		for id, npc := range s.NPCs {
			npc.Body.Inventory = cloneObjects(npc.Body.Inventory)
			npc.TradeOffers = cloneNPCTradeOffers(npc.TradeOffers)
			if npc.Items != nil {
				items := npc.Items.clone()
				npc.Items = &items
			}
			if npc.Enemies != nil {
				npc.Enemies = append([]NPCEnemy{}, npc.Enemies...)
			}
			if npc.PermanentOrigin != nil {
				origin := *npc.PermanentOrigin
				npc.PermanentOrigin = &origin
			}
			next.NPCs[id] = npc
		}
	}
	if s.Invitations != nil {
		next.Invitations = make(map[int16][]string, len(s.Invitations))
		for property, ids := range s.Invitations {
			next.Invitations[property] = append([]string(nil), ids...)
		}
	}
	if s.Votes != nil {
		votes := s.Votes.Clone()
		next.Votes = &votes
	}
	if s.War != nil {
		war := *s.War
		next.War = &war
	}
	if s.Family != nil {
		family := s.Family.Clone()
		next.Family = &family
	}
	if s.FamilyNews != nil {
		news := s.FamilyNews.Clone()
		next.FamilyNews = &news
	}
	for id, room := range s.Rooms {
		room.Resource = cloneRoom(room.Resource)
		room.PlayerIDs = append([]string(nil), room.PlayerIDs...)
		room.NPCIDs = append([]string(nil), room.NPCIDs...)
		if room.Items != nil {
			items := room.Items.clone()
			room.Items = &items
		}
		next.Rooms[id] = room
	}
	for id, player := range s.Players {
		player.Aliases = clonePlayerAliases(player.Aliases)
		player.PlayerEnemies = append([]string(nil), player.PlayerEnemies...)
		if player.CharmRefs != nil {
			player.CharmRefs = append([]EntityRef{}, player.CharmRefs...)
		}
		player.FollowerIDs = append([]string(nil), player.FollowerIDs...)
		if player.NPCFollowerIDs != nil {
			player.NPCFollowerIDs = append([]string{}, player.NPCFollowerIDs...)
		}
		if player.FollowerRefs != nil {
			player.FollowerRefs = append([]EntityRef{}, player.FollowerRefs...)
		}
		player.Body.Inventory = cloneObjects(player.Body.Inventory)
		if player.Items != nil {
			items := player.Items.clone()
			player.Items = &items
		}
		next.Players[id] = player
	}
	return next
}
