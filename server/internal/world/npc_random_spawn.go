package world

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
)

const (
	npcRandomAttackTimer   = 3  // LT_ATTCK
	npcRandomScavengeTimer = 4  // LT_MSCAV
	npcRandomWanderTimer   = 6  // LT_MWAND
	npcRandomGroupFlag     = 23 // RPLWAN
	npcRandomEnchantFlag   = 21 // ORENCH
	npcRandomFixedGoldFlag = 25 // MNRGLD
)

// ErrNPCRandomCatalogAbsent is the explicit catalog result for a random-room
// monster entry that is not present.  update_random treats load_crt failure as
// a no-op for that room; other catalog failures reject the whole proposal.
var ErrNPCRandomCatalogAbsent = errors.New("NPC random catalog entry absent")

// NPCRandomProducerInput is the small dependency seam for the pure producer.
// Catalog is read only, Roll is the source-compatible mrand(lo, hi) seam, and
// Allocate issues a fresh immutable runtime NPC identity.  No scheduler or
// durable receipt is implied by this input.
type NPCRandomProducerInput struct {
	Now      int32
	Catalog  SpawnCatalog
	Roll     func(int, int) int
	Allocate func() (string, error)
}

// NPCRandomSpawn is one newly admitted NPC in source admission order.  Body is
// a value projection; identity ownership belongs to the map key ID.
type NPCRandomSpawn struct {
	ID         string
	RoomID     int16
	TemplateID int16
	Body       LegacyMonster
}

// NPCRandomRoomDecision records every occupied-room decision, including a
// traffic failure or an empty random slot.  RandomIndex is -1 when traffic
// failed, which distinguishes that no-op from a successful roll selecting
// room.Random[0].
type NPCRandomRoomDecision struct {
	RoomID           int16
	PlayerCount      int
	TrafficRoll      int
	TrafficPassed    bool
	RandomIndex      int
	RandomRolled     bool
	TemplateID       int16
	TemplatePresent  bool
	TemplateWander   byte
	GroupWander      bool
	SpawnCount       int
	SpawnCountRoll   int
	SpawnCountRolled bool
	BeforeNPCIDs     []string
	AfterNPCIDs      []string
	Spawns           []NPCRandomSpawn
}

// NPCRandomProducerProposal is a complete snapshot-bound candidate for the
// legacy update_random producer. Before and the ordered room decisions make
// the apply side independent of catalog/RNG/allocator calls. The private hash
// and one-shot token additionally reject same-snapshot replay and in-package
// proposal tampering without adding a shared State revision API.
type NPCRandomProducerProposal struct {
	Now               int32
	Before            State
	Rooms             []NPCRandomRoomDecision
	AfterActiveNPCIDs []string

	beforeDigest   [32]byte
	proposalDigest [32]byte
	applyToken     *npcRandomApplyToken
}

// NPCRandomSpawnProposal is a descriptive alias for callers that name this
// reducer after its admission output rather than its source producer.
type NPCRandomSpawnProposal = NPCRandomProducerProposal

// NPCRandomSpawnInput is the matching descriptive alias for the input seam.
type NPCRandomSpawnInput = NPCRandomProducerInput

type npcRandomApplyToken struct{ used uint32 }

func (t *npcRandomApplyToken) valid() bool {
	return t != nil && atomic.LoadUint32(&t.used) == 0
}

func (t *npcRandomApplyToken) consume() bool {
	return t != nil && atomic.CompareAndSwapUint32(&t.used, 0, 1)
}

// PlanNPCRandomProducer ports src/update.c:146-230.  Rooms are visited once
// in sorted room/ordered PlayerIDs traversal, matching the server's canonical
// replacement for C's first descriptor encounter.  The operation is pure:
// all world writes happen only in ApplyNPCRandomProducer.
func (s State) PlanNPCRandomProducer(in NPCRandomProducerInput) (NPCRandomProducerProposal, error) {
	roomIDs, err := validateNPCRandomSource(s)
	if err != nil {
		return NPCRandomProducerProposal{}, err
	}

	before, beforeDigest, err := snapshotNPCRandomState(s)
	if err != nil {
		return NPCRandomProducerProposal{}, fmt.Errorf("NPC random snapshot: %w", err)
	}
	proposal := NPCRandomProducerProposal{
		Now:               in.Now,
		Before:            before,
		AfterActiveNPCIDs: cloneNPCRandomIDs(s.ActiveNPCIDs),
		applyToken:        &npcRandomApplyToken{},
		beforeDigest:      beforeDigest,
	}

	if len(roomIDs) != 0 && in.Roll == nil {
		return NPCRandomProducerProposal{}, fmt.Errorf("NPC random producer requires RNG")
	}
	seenIDs := make(map[string]bool, len(s.NPCs))
	for id := range s.NPCs {
		seenIDs[id] = true
	}

	for _, roomID := range roomIDs {
		room := s.Rooms[roomID]
		decision := NPCRandomRoomDecision{
			RoomID:       roomID,
			PlayerCount:  len(room.PlayerIDs),
			RandomIndex:  -1,
			BeforeNPCIDs: cloneNPCRandomIDs(room.NPCIDs),
			AfterNPCIDs:  cloneNPCRandomIDs(room.NPCIDs),
		}

		traffic, err := npcRandomRoll(in.Roll, 1, 100)
		if err != nil {
			return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d traffic: %w", roomID, err)
		}
		decision.TrafficRoll = traffic
		if traffic > int(room.Resource.Traffic) {
			proposal.Rooms = append(proposal.Rooms, decision)
			continue
		}
		decision.TrafficPassed = true

		index, err := npcRandomRoll(in.Roll, 0, 9)
		if err != nil {
			return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d slot: %w", roomID, err)
		}
		decision.RandomIndex = index
		decision.RandomRolled = true
		templateID := room.Resource.Random[index]
		decision.TemplateID = templateID
		if templateID == 0 {
			proposal.Rooms = append(proposal.Rooms, decision)
			continue
		}
		if in.Catalog == nil {
			return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d requires spawn catalog", roomID)
		}
		template, present, err := npcRandomLoadMonster(in.Catalog, templateID)
		if err != nil {
			return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d template %d: %w", roomID, templateID, err)
		}
		if !present {
			proposal.Rooms = append(proposal.Rooms, decision)
			continue
		}
		if err := validateNPCRandomTemplate(template, templateID); err != nil {
			return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d template %d: %w", roomID, templateID, err)
		}
		decision.TemplatePresent = true
		decision.TemplateWander = template.Wander
		decision.GroupWander = flag(room.Resource.Flags[:], npcRandomGroupFlag)
		maxCount := 1
		if decision.GroupWander {
			maxCount = len(room.PlayerIDs)
		} else if wander := int(int8(template.Wander)); wander > 1 {
			maxCount = wander
		}
		if maxCount < 1 {
			return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d invalid spawn count bound", roomID)
		}
		decision.SpawnCount = 1
		if maxCount > 1 {
			count, err := npcRandomRoll(in.Roll, 1, maxCount)
			if err != nil {
				return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d count: %w", roomID, err)
			}
			decision.SpawnCount = count
			decision.SpawnCountRoll = count
			decision.SpawnCountRolled = true
		}

		staged := make(map[string]NPCState, decision.SpawnCount)
		roomIDsAfter := cloneNPCRandomIDs(room.NPCIDs)
		for instance := 0; instance < decision.SpawnCount; instance++ {
			if instance != 0 {
				// update_random reloads the catalog template between instances;
				// never clone an already randomized body for the next NPC.
				template, present, err = npcRandomLoadMonster(in.Catalog, templateID)
				if err != nil {
					return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d template %d reload: %w", roomID, templateID, err)
				}
				if !present {
					return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d template %d reload absent", roomID, templateID)
				}
				if err := validateNPCRandomTemplate(template, templateID); err != nil {
					return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d template %d reload: %w", roomID, templateID, err)
				}
			}
			body, err := spawnNPCRandomMonster(template, in.Now, in.Catalog, in.Roll)
			if err != nil {
				return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d instance %d: %w", roomID, instance, err)
			}
			body.RoomID = roomID
			if err := validateNPCRandomBody(body, roomID); err != nil {
				return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d instance %d: %w", roomID, instance, err)
			}
			id, err := npcRandomAllocate(in.Allocate)
			if err != nil {
				return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d instance %d allocate: %w", roomID, instance, err)
			}
			if id == "" || seenIDs[id] {
				return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d instance %d duplicate or empty identity %q", roomID, instance, id)
			}
			seenIDs[id] = true
			body = cloneNPCBody(body)
			spawn := NPCRandomSpawn{ID: id, RoomID: roomID, TemplateID: templateID, Body: body}
			decision.Spawns = append(decision.Spawns, spawn)
			staged[id] = NPCState{Body: cloneNPCBody(body), Enemies: []NPCEnemy{}}
			roomIDsAfter, err = insertNPCResourceID(roomIDsAfter, id, s.NPCs, staged)
			if err != nil {
				return NPCRandomProducerProposal{}, fmt.Errorf("NPC random room %d instance %d room admission: %w", roomID, instance, err)
			}
		}
		decision.AfterNPCIDs = roomIDsAfter
		proposal.Rooms = append(proposal.Rooms, decision)
	}

	for _, decision := range proposal.Rooms {
		for _, spawn := range decision.Spawns {
			proposal.AfterActiveNPCIDs = prependNPCRandomID(proposal.AfterActiveNPCIDs, spawn.ID)
		}
	}
	proposal.proposalDigest, err = digestNPCRandomProposal(proposal)
	if err != nil {
		return NPCRandomProducerProposal{}, fmt.Errorf("NPC random proposal digest: %w", err)
	}
	return proposal, nil
}

// PlanNPCRandomSpawn is the descriptive spawn/admission entry point.
func (s State) PlanNPCRandomSpawn(in NPCRandomSpawnInput) (NPCRandomSpawnProposal, error) {
	return s.PlanNPCRandomProducer(in)
}

// ApplyNPCRandomProducer validates and commits a planned candidate without
// invoking RNG, catalog, or allocator. All checks happen before the isolated
// state is published, and any failure returns a zero State.
func (s State) ApplyNPCRandomProducer(proposal NPCRandomProducerProposal) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if proposal.Before.Version != 1 || !proposal.applyToken.valid() {
		return State{}, fmt.Errorf("empty or replayed NPC random proposal")
	}
	currentDigest, err := digestNPCRandomState(s)
	if err != nil {
		return State{}, fmt.Errorf("NPC random current snapshot: %w", err)
	}
	proposalBeforeDigest, err := digestNPCRandomState(proposal.Before)
	if err != nil || currentDigest != proposal.beforeDigest || proposalBeforeDigest != proposal.beforeDigest || !reflect.DeepEqual(s, proposal.Before) {
		return State{}, fmt.Errorf("stale NPC random proposal")
	}
	proposalDigest, err := digestNPCRandomProposal(proposal)
	if err != nil || proposalDigest != proposal.proposalDigest {
		return State{}, fmt.Errorf("tampered NPC random proposal")
	}
	roomIDs, err := validateNPCRandomSource(s)
	if err != nil {
		return State{}, err
	}
	if len(roomIDs) != len(proposal.Rooms) {
		return State{}, fmt.Errorf("NPC random room decision count mismatch")
	}
	for i, roomID := range roomIDs {
		if proposal.Rooms[i].RoomID != roomID {
			return State{}, fmt.Errorf("NPC random room order mismatch")
		}
	}

	staged := make(map[string]NPCState, len(proposal.Spawns()))
	seenIDs := make(map[string]bool, len(s.NPCs)+len(proposal.Spawns()))
	for id := range s.NPCs {
		seenIDs[id] = true
	}
	computedActive := cloneNPCRandomIDs(s.ActiveNPCIDs)
	computedRoomIDs := make(map[int16][]string, len(proposal.Rooms))
	for _, decision := range proposal.Rooms {
		room := s.Rooms[decision.RoomID]
		if err := validateNPCRandomDecision(decision, room); err != nil {
			return State{}, err
		}
		roomIDsAfter := cloneNPCRandomIDs(room.NPCIDs)
		for instance, spawn := range decision.Spawns {
			if spawn.ID == "" || seenIDs[spawn.ID] || spawn.RoomID != decision.RoomID || spawn.TemplateID != decision.TemplateID || instance >= decision.SpawnCount {
				return State{}, fmt.Errorf("NPC random spawn identity mismatch in room %d", decision.RoomID)
			}
			if err := validateNPCRandomBody(spawn.Body, decision.RoomID); err != nil {
				return State{}, fmt.Errorf("NPC random spawn %q: %w", spawn.ID, err)
			}
			seenIDs[spawn.ID] = true
			staged[spawn.ID] = NPCState{Body: cloneNPCBody(spawn.Body), Enemies: []NPCEnemy{}}
			var insertErr error
			roomIDsAfter, insertErr = insertNPCResourceID(roomIDsAfter, spawn.ID, s.NPCs, staged)
			if insertErr != nil {
				return State{}, fmt.Errorf("NPC random room %d admission: %w", decision.RoomID, insertErr)
			}
			computedActive = prependNPCRandomID(computedActive, spawn.ID)
		}
		if !reflect.DeepEqual(roomIDsAfter, decision.AfterNPCIDs) {
			return State{}, fmt.Errorf("NPC random room %d order mismatch", decision.RoomID)
		}
		computedRoomIDs[decision.RoomID] = roomIDsAfter
	}
	if !reflect.DeepEqual(computedActive, proposal.AfterActiveNPCIDs) {
		return State{}, fmt.Errorf("NPC random active order mismatch")
	}

	next, err := cloneNPCRandomState(s)
	if err != nil {
		return State{}, fmt.Errorf("NPC random candidate snapshot: %w", err)
	}
	for id, npc := range staged {
		next.NPCs[id] = npc
	}
	for roomID, ids := range computedRoomIDs {
		room := next.Rooms[roomID]
		room.NPCIDs = cloneNPCRandomIDs(ids)
		next.Rooms[roomID] = room
	}
	next.ActiveNPCIDs = cloneNPCRandomIDs(computedActive)
	if err := next.Validate(); err != nil {
		return State{}, fmt.Errorf("NPC random result invalid: %w", err)
	}
	if !proposal.applyToken.consume() {
		return State{}, fmt.Errorf("replayed NPC random proposal")
	}
	return next, nil
}

// ApplyNPCRandomSpawn is the descriptive spawn/admission entry point.
func (s State) ApplyNPCRandomSpawn(proposal NPCRandomSpawnProposal) (State, error) {
	return s.ApplyNPCRandomProducer(proposal)
}

// Spawns returns the ordered concatenation of all room admissions. It is a
// convenience for callers/tests and returns a fresh slice of fresh bodies.
func (p NPCRandomProducerProposal) Spawns() []NPCRandomSpawn {
	var out []NPCRandomSpawn
	for _, decision := range p.Rooms {
		for _, spawn := range decision.Spawns {
			spawn.Body = cloneNPCBody(spawn.Body)
			out = append(out, spawn)
		}
	}
	return out
}

func validateNPCRandomSource(s State) ([]int16, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.NPCs == nil {
		return nil, fmt.Errorf("NPC random producer requires canonical NPC state")
	}
	roomIDs := make([]int, 0, len(s.Rooms))
	for roomID := range s.Rooms {
		roomIDs = append(roomIDs, int(roomID))
	}
	sort.Ints(roomIDs)
	ordered := make([]int16, 0, len(roomIDs))
	for _, key := range roomIDs {
		roomID := int16(key)
		room := s.Rooms[roomID]
		if len(room.PlayerIDs) == 0 {
			continue
		}
		if room.Items == nil || len(room.Resource.Objects) != 0 {
			return nil, fmt.Errorf("NPC random room %d canonical floor unresolved", roomID)
		}
		if s.ActiveNPCIDs == nil {
			return nil, fmt.Errorf("NPC random active order unresolved")
		}
		for _, npcID := range room.NPCIDs {
			npc, ok := s.NPCs[npcID]
			if !ok || npcID == "" || npc.Body.RoomID != roomID || npc.Body.Type != 1 {
				return nil, fmt.Errorf("NPC random room %d NPC identity unresolved", roomID)
			}
			if npc.Enemies == nil {
				return nil, fmt.Errorf("NPC random room %d enemy relations unresolved", roomID)
			}
		}
		ordered = append(ordered, roomID)
	}
	return ordered, nil
}

func validateNPCRandomTemplate(template LegacyMonster, templateID int16) error {
	if templateID == 0 || template.Type != 1 || template.Name == "" {
		return fmt.Errorf("invalid monster template")
	}
	if len(template.Inventory) != 0 {
		return fmt.Errorf("monster template has unexpected inventory")
	}
	return nil
}

func validateNPCRandomBody(body LegacyMonster, roomID int16) error {
	if body.Type != 1 || body.Name == "" || body.RoomID != roomID {
		return fmt.Errorf("invalid spawned monster body")
	}
	if body.Timers[npcRandomAttackTimer].LastTime != body.Timers[npcRandomScavengeTimer].LastTime || body.Timers[npcRandomAttackTimer].LastTime != body.Timers[npcRandomWanderTimer].LastTime {
		return fmt.Errorf("spawn timers are not synchronized")
	}
	wantInterval := int32(3)
	if int(int8(body.Stats[1])) >= 20 {
		wantInterval = 2
	}
	if body.Timers[npcRandomAttackTimer].Interval != wantInterval {
		return fmt.Errorf("spawn attack interval mismatch")
	}
	for i := 1; i < len(body.Inventory); i++ {
		prior, current := body.Inventory[i-1], body.Inventory[i]
		if prior.Name > current.Name || (prior.Name == current.Name && int8(prior.Adjustment) > int8(current.Adjustment)) {
			return fmt.Errorf("spawn inventory order mismatch")
		}
	}
	return nil
}

func validateNPCRandomDecision(decision NPCRandomRoomDecision, room RoomState) error {
	if decision.PlayerCount != len(room.PlayerIDs) || !reflect.DeepEqual(decision.BeforeNPCIDs, room.NPCIDs) {
		return fmt.Errorf("NPC random room %d source index mismatch", decision.RoomID)
	}
	if decision.TrafficRoll < 1 || decision.TrafficRoll > 100 {
		return fmt.Errorf("NPC random room %d traffic roll invalid", decision.RoomID)
	}
	if !decision.TrafficPassed {
		if decision.RandomRolled || decision.RandomIndex != -1 || decision.TemplateID != 0 || decision.TemplatePresent || decision.SpawnCount != 0 || len(decision.Spawns) != 0 || !reflect.DeepEqual(decision.AfterNPCIDs, room.NPCIDs) {
			return fmt.Errorf("NPC random room %d traffic no-op mismatch", decision.RoomID)
		}
		return nil
	}
	if !decision.RandomRolled || decision.RandomIndex < 0 || decision.RandomIndex > 9 || decision.TemplateID != room.Resource.Random[decision.RandomIndex] {
		return fmt.Errorf("NPC random room %d random slot mismatch", decision.RoomID)
	}
	if decision.TemplateID == 0 {
		if decision.TemplatePresent || decision.SpawnCount != 0 || len(decision.Spawns) != 0 || !reflect.DeepEqual(decision.AfterNPCIDs, room.NPCIDs) {
			return fmt.Errorf("NPC random room %d empty-template no-op mismatch", decision.RoomID)
		}
		return nil
	}
	if !decision.TemplatePresent {
		if decision.SpawnCount != 0 || len(decision.Spawns) != 0 || !reflect.DeepEqual(decision.AfterNPCIDs, room.NPCIDs) {
			return fmt.Errorf("NPC random room %d absent-template no-op mismatch", decision.RoomID)
		}
		return nil
	}
	if decision.GroupWander != flag(room.Resource.Flags[:], npcRandomGroupFlag) || decision.SpawnCount < 1 || decision.SpawnCount != len(decision.Spawns) {
		return fmt.Errorf("NPC random room %d count metadata mismatch", decision.RoomID)
	}
	maxCount := 1
	if decision.GroupWander {
		maxCount = len(room.PlayerIDs)
	} else if wander := int(int8(decision.TemplateWander)); wander > 1 {
		maxCount = wander
	}
	if decision.SpawnCount > maxCount {
		return fmt.Errorf("NPC random room %d spawn count outside template bound", decision.RoomID)
	}
	if maxCount > 1 {
		if !decision.SpawnCountRolled || decision.SpawnCountRoll != decision.SpawnCount || decision.SpawnCountRoll < 1 || decision.SpawnCountRoll > maxCount {
			return fmt.Errorf("NPC random room %d count roll mismatch", decision.RoomID)
		}
	} else if decision.SpawnCountRolled || decision.SpawnCountRoll != 0 || decision.SpawnCount != 1 {
		return fmt.Errorf("NPC random room %d unexpected count roll", decision.RoomID)
	}
	return nil
}

func spawnNPCRandomMonster(template LegacyMonster, now int32, catalog SpawnCatalog, roll func(int, int) int) (LegacyMonster, error) {
	if err := validateNPCRandomTemplate(template, 1); err != nil {
		return LegacyMonster{}, err
	}
	m := template
	m.Inventory = nil
	m.Timers[npcRandomAttackTimer].LastTime = now
	m.Timers[npcRandomScavengeTimer].LastTime = now
	m.Timers[npcRandomWanderTimer].LastTime = now
	if int(int8(m.Stats[1])) < 20 {
		m.Timers[npcRandomAttackTimer].Interval = 3
	} else {
		m.Timers[npcRandomAttackTimer].Interval = 2
	}
	itemRoll, err := npcRandomRoll(roll, 1, 100)
	if err != nil {
		return LegacyMonster{}, err
	}
	itemAttempts := 1
	if itemRoll >= 96 {
		itemAttempts = 3
	} else if itemRoll >= 90 {
		itemAttempts = 2
	}
	fixedGold := flag(m.Flags[:], npcRandomFixedGoldFlag)
	for attempt := 0; attempt < itemAttempts; attempt++ {
		upper := 9
		if fixedGold {
			upper = 49
		}
		slot, err := npcRandomRoll(roll, 0, upper)
		if err != nil {
			return LegacyMonster{}, err
		}
		if slot > 9 || m.Carry[slot] == 0 {
			continue
		}
		if catalog == nil {
			return LegacyMonster{}, fmt.Errorf("missing object catalog")
		}
		o, present, err := npcRandomLoadObject(catalog, m.Carry[slot])
		if err != nil {
			return LegacyMonster{}, err
		}
		if !present {
			continue
		}
		if len(o.Contents) != 0 {
			return LegacyMonster{}, fmt.Errorf("object template has unexpected contents")
		}
		if flag(o.Flags[:], npcRandomEnchantFlag) {
			o, err = npcRandomEnchant(o, roll)
			if err != nil {
				return LegacyMonster{}, err
			}
		}
		lo := int64(o.Value) * 9 / 10
		hi := int64(o.Value) * 11 / 10
		if lo < int64(-1<<31) || hi > int64(1<<31-1) || lo > hi {
			return LegacyMonster{}, fmt.Errorf("object value range invalid")
		}
		value, err := npcRandomRoll(roll, int(lo), int(hi))
		if err != nil {
			return LegacyMonster{}, err
		}
		o.Value = int32(value)
		m.Inventory = append(m.Inventory, o)
	}
	if !fixedGold && m.Gold != 0 {
		lo, hi := int64(m.Gold)/10, int64(m.Gold)
		if lo < int64(-1<<31) || hi > int64(1<<31-1) || lo > hi {
			return LegacyMonster{}, fmt.Errorf("gold range invalid")
		}
		gold, err := npcRandomRoll(roll, int(lo), int(hi))
		if err != nil {
			return LegacyMonster{}, err
		}
		m.Gold = int32(gold)
	}
	sort.SliceStable(m.Inventory, func(i, j int) bool {
		if m.Inventory[i].Name != m.Inventory[j].Name {
			return m.Inventory[i].Name < m.Inventory[j].Name
		}
		return int8(m.Inventory[i].Adjustment) < int8(m.Inventory[j].Adjustment)
	})
	return m, nil
}

func npcRandomEnchant(o LegacyObject, roll func(int, int) int) (LegacyObject, error) {
	// EnchantObject is the existing source-faithful rand_enchant helper. The
	// safe roll wrapper converts a panicking injected RNG into a range error.
	return EnchantObject(o, npcRandomSafeRoll(roll))
}

func npcRandomRoll(roll func(int, int) int, low, high int) (value int, err error) {
	if roll == nil {
		return 0, fmt.Errorf("random source unavailable")
	}
	return randomIn(npcRandomSafeRoll(roll), low, high)
}

func npcRandomSafeRoll(roll func(int, int) int) func(int, int) int {
	return func(low, high int) (value int) {
		defer func() {
			if recover() != nil {
				value = low - 1
			}
		}()
		return roll(low, high)
	}
}

func npcRandomLoadMonster(catalog SpawnCatalog, id int16) (monster LegacyMonster, present bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("monster loader panicked: %v", recovered)
			present = false
		}
	}()
	monster, err = catalog.Monster(id)
	if err != nil {
		if npcRandomCatalogAbsent(err, true) {
			return LegacyMonster{}, false, nil
		}
		return LegacyMonster{}, false, err
	}
	return monster, true, nil
}

func npcRandomLoadObject(catalog SpawnCatalog, id int16) (object LegacyObject, present bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("object loader panicked: %v", recovered)
			present = false
		}
	}()
	object, err = catalog.Object(id)
	if err != nil {
		if npcRandomCatalogAbsent(err, false) {
			return LegacyObject{}, false, nil
		}
		return LegacyObject{}, false, err
	}
	return object, true, nil
}

func npcRandomCatalogAbsent(err error, monster bool) bool {
	if errors.Is(err, ErrNPCRandomCatalogAbsent) || errors.Is(err, fs.ErrNotExist) {
		return true
	}
	// TemplateCatalog wraps fs.ErrNotExist, while small legacy test seams often
	// use a descriptive error. Keep the compatibility recognition narrow so a
	// generic loader failure still rejects the whole candidate.
	message := strings.ToLower(err.Error())
	if monster {
		return strings.Contains(message, "missing monster") || strings.Contains(message, "monster absent") || strings.Contains(message, "monster not found")
	}
	return strings.Contains(message, "missing object") || strings.Contains(message, "object absent") || strings.Contains(message, "object not found")
}

func npcRandomAllocate(allocate func() (string, error)) (id string, err error) {
	if allocate == nil {
		return "", fmt.Errorf("identity allocator unavailable")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("identity allocator panicked: %v", recovered)
			id = ""
		}
	}()
	return allocate()
}

func cloneNPCRandomIDs(ids []string) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}

func prependNPCRandomID(ids []string, id string) []string {
	out := make([]string, 1, len(ids)+1)
	out[0] = id
	for _, old := range ids {
		if old != id {
			out = append(out, old)
		}
	}
	return out
}

func snapshotNPCRandomState(s State) (State, [32]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return State{}, [32]byte{}, err
	}
	copyOfState, err := DecodeState(raw)
	if err != nil {
		return State{}, [32]byte{}, err
	}
	return copyOfState, sha256.Sum256(raw), nil
}

func cloneNPCRandomState(s State) (State, error) {
	copyOfState, _, err := snapshotNPCRandomState(s)
	return copyOfState, err
}

func digestNPCRandomState(s State) ([32]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func digestNPCRandomProposal(p NPCRandomProducerProposal) ([32]byte, error) {
	type content struct {
		Now               int32
		Before            State
		Rooms             []NPCRandomRoomDecision
		AfterActiveNPCIDs []string
	}
	raw, err := json.Marshal(content{Now: p.Now, Before: p.Before, Rooms: p.Rooms, AfterActiveNPCIDs: p.AfterActiveNPCIDs})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}
