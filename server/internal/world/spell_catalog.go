package world

// This file is the bounded, read-only projection of the legacy spell tables:
// src/global.c:spllist (the 56 spell names/levels) and ospell (the offensive
// spell realm, cost and dice metadata).  It intentionally does not dispatch a
// spell function.  Offensive, targeted, map and otherwise unresolved effects
// remain fail-closed until their canonical Go state transitions exist.

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// SpellCatalogSize is spllist's active count.  Spell bits are zero-based,
	// while a scroll's MagicPower stores the same entry as one-based.
	SpellCatalogSize = 56

	// Legacy spell realms are the values of EARTH/WIND/FIRE/WATER in mtype.h.
	SpellRealmNone  SpellRealm = 0
	SpellRealmEarth SpellRealm = 1
	SpellRealmWind  SpellRealm = 2
	SpellRealmFire  SpellRealm = 3
	SpellRealmWater SpellRealm = 4
)

// SpellRealm is the source ospell.realm value.  Utility entries that have no
// ospell row use SpellRealmNone; that does not infer a realm or a cast rule.
type SpellRealm uint8

// SpellExecution describes the current Go migration boundary, not a new game
// permission.  A self-effect entry is backed by the existing potion/scroll
// reducer.  The other values are metadata-only and must not be executed.
type SpellExecution string

const (
	SpellExecutionSelfEffect       SpellExecution = "self-effect"
	SpellExecutionOffensivePending SpellExecution = "offensive-pending"
	SpellExecutionUnresolved       SpellExecution = "unresolved"
)

var (
	ErrSpellCatalogUnavailable = errors.New("spell catalog unavailable")
	ErrSpellCatalogInvalid     = errors.New("invalid spell catalog")
	ErrSpellListActorAbsent    = errors.New("online spell-list actor absent")
	ErrSpellListStaleProposal  = errors.New("stale spell-list proposal")
	ErrSpellListTampered       = errors.New("tampered spell-list proposal")
)

// Compatibility spellings make the migration boundary explicit to adapters
// without exposing the mutable legacy table.
var (
	ErrSpellCatalogMissing = ErrSpellCatalogUnavailable
	ErrSpellCatalogCorrupt = ErrSpellCatalogInvalid
	ErrSpellListStale      = ErrSpellListStaleProposal
)

// SpellCatalogEntry is one source-backed spllist row plus an optional ospell
// row. Index is the zero-based spell bit/splno; MagicPower is the one-based
// object field used by scrolls, potions and wands. DicePlus maps ospell.pdice.
// A nil/unknown execution is never silently promoted to a cast implementation.
type SpellCatalogEntry struct {
	Index      int            `json:"index"`
	MagicPower byte           `json:"magic_power"`
	Name       string         `json:"name"`
	Level      byte           `json:"level"`
	Execution  SpellExecution `json:"execution"`
	Realm      SpellRealm     `json:"realm,omitempty"`
	MPCost     int16          `json:"mp_cost,omitempty"`
	DiceCount  int16          `json:"dice_count,omitempty"`
	DiceSides  int16          `json:"dice_sides,omitempty"`
	DicePlus   int16          `json:"dice_plus,omitempty"`
	BonusType  byte           `json:"bonus_type,omitempty"`
}

func validSpellCatalogName(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func (entry SpellCatalogEntry) validate() error {
	if entry.Index < 0 || entry.Index >= SpellCatalogSize || int(entry.MagicPower) != entry.Index+1 || !validSpellCatalogName(entry.Name) || entry.Level < 1 || entry.Level > 5 {
		return fmt.Errorf("%w: invalid entry identity/index=%d", ErrSpellCatalogInvalid, entry.Index)
	}
	if entry.Execution != SpellExecutionSelfEffect && entry.Execution != SpellExecutionOffensivePending && entry.Execution != SpellExecutionUnresolved {
		return fmt.Errorf("%w: unsupported execution index=%d", ErrSpellCatalogInvalid, entry.Index)
	}
	if entry.Realm > SpellRealmWater || entry.MPCost < 0 || entry.DiceCount < 0 || entry.DiceSides < 0 || entry.DicePlus < 0 || entry.BonusType > 3 {
		return fmt.Errorf("%w: invalid ospell metadata index=%d", ErrSpellCatalogInvalid, entry.Index)
	}
	if entry.Execution == SpellExecutionOffensivePending {
		if entry.Realm == SpellRealmNone || entry.MPCost < 1 || entry.DiceCount < 1 || entry.DiceSides < 1 || entry.BonusType < 1 {
			return fmt.Errorf("%w: incomplete offensive row index=%d", ErrSpellCatalogInvalid, entry.Index)
		}
	} else if entry.Realm != SpellRealmNone || entry.MPCost != 0 || entry.DiceCount != 0 || entry.DiceSides != 0 || entry.DicePlus != 0 || entry.BonusType != 0 {
		return fmt.Errorf("%w: non-offensive row has ospell metadata index=%d", ErrSpellCatalogInvalid, entry.Index)
	}
	return nil
}

// spellOffensiveMetadata is a direct, ordered projection of the active rows
// in src/global.c:ospell. The source has no row for the commented SNAHAN spell.
type spellOffensiveMetadata struct {
	present   bool
	realm     SpellRealm
	mp        int16
	diceCount int16
	diceSides int16
	dicePlus  int16
	bonusType byte
}

// The sparse keyed literal keeps each source splno visible beside its row and
// makes an accidental index shift fail the catalog tests rather than changing
// a different spell's cost or realm.
var legacyOffensiveSpellMetadata = [SpellCatalogSize]spellOffensiveMetadata{
	1:  {present: true, realm: SpellRealmWind, mp: 3, diceCount: 1, diceSides: 8, dicePlus: 0, bonusType: 1},    // SHURTS
	6:  {present: true, realm: SpellRealmFire, mp: 7, diceCount: 2, diceSides: 5, dicePlus: 8, bonusType: 2},    // SFIREB
	13: {present: true, realm: SpellRealmWind, mp: 15, diceCount: 3, diceSides: 4, dicePlus: 18, bonusType: 3},  // SLGHTN
	14: {present: true, realm: SpellRealmWater, mp: 25, diceCount: 4, diceSides: 5, dicePlus: 30, bonusType: 3}, // SICEBL
	25: {present: true, realm: SpellRealmWind, mp: 10, diceCount: 2, diceSides: 5, dicePlus: 13, bonusType: 2},  // SSHOCK
	26: {present: true, realm: SpellRealmEarth, mp: 3, diceCount: 1, diceSides: 8, dicePlus: 0, bonusType: 1},   // SRUMBL
	27: {present: true, realm: SpellRealmFire, mp: 3, diceCount: 1, diceSides: 7, dicePlus: 1, bonusType: 1},    // SBURNS
	28: {present: true, realm: SpellRealmWater, mp: 3, diceCount: 1, diceSides: 8, dicePlus: 0, bonusType: 1},   // SBLIST
	29: {present: true, realm: SpellRealmWind, mp: 7, diceCount: 2, diceSides: 5, dicePlus: 7, bonusType: 2},    // SDUSTG
	30: {present: true, realm: SpellRealmWater, mp: 7, diceCount: 2, diceSides: 5, dicePlus: 8, bonusType: 2},   // SWBOLT
	31: {present: true, realm: SpellRealmEarth, mp: 7, diceCount: 2, diceSides: 5, dicePlus: 7, bonusType: 2},   // SCRUSH
	32: {present: true, realm: SpellRealmEarth, mp: 10, diceCount: 2, diceSides: 5, dicePlus: 13, bonusType: 2}, // SENGUL
	33: {present: true, realm: SpellRealmFire, mp: 10, diceCount: 2, diceSides: 5, dicePlus: 13, bonusType: 2},  // SBURST
	34: {present: true, realm: SpellRealmWater, mp: 10, diceCount: 2, diceSides: 5, dicePlus: 13, bonusType: 2}, // SSTEAM
	35: {present: true, realm: SpellRealmEarth, mp: 15, diceCount: 3, diceSides: 4, dicePlus: 19, bonusType: 3}, // SSHATT
	36: {present: true, realm: SpellRealmFire, mp: 15, diceCount: 3, diceSides: 4, dicePlus: 18, bonusType: 3},  // SIMMOL
	37: {present: true, realm: SpellRealmWater, mp: 15, diceCount: 3, diceSides: 4, dicePlus: 18, bonusType: 3}, // SBLOOD
	38: {present: true, realm: SpellRealmWind, mp: 25, diceCount: 4, diceSides: 5, dicePlus: 30, bonusType: 3},  // STHUND
	39: {present: true, realm: SpellRealmEarth, mp: 25, diceCount: 4, diceSides: 5, dicePlus: 30, bonusType: 3}, // SEQUAK
	40: {present: true, realm: SpellRealmFire, mp: 25, diceCount: 4, diceSides: 5, dicePlus: 30, bonusType: 3},  // SFLFIL
}

func spellCatalogExecution(index int) SpellExecution {
	if index >= 0 && index < len(legacyOffensiveSpellMetadata) && legacyOffensiveSpellMetadata[index].present {
		return SpellExecutionOffensivePending
	}
	if _, ok := drinkSpells[index]; ok {
		return SpellExecutionSelfEffect
	}
	return SpellExecutionUnresolved
}

// SpellCatalog returns a fresh, source-validated snapshot. It checks the
// existing canonical spllist name table and teach level projection on every
// call, so a missing/invalid migration resource fails closed instead of
// granting a guessed spell bit. The returned slice can be safely mutated by a
// caller without changing the server catalog.
func SpellCatalog() ([]SpellCatalogEntry, error) {
	if len(legacyInfoSpellNames) != SpellCatalogSize || len(teachSpellLevels) != SpellCatalogSize || len(legacyOffensiveSpellMetadata) != SpellCatalogSize {
		return nil, ErrSpellCatalogUnavailable
	}
	entries := make([]SpellCatalogEntry, 0, SpellCatalogSize)
	seen := make(map[string]bool, SpellCatalogSize)
	for index, name := range legacyInfoSpellNames {
		if !validSpellCatalogName(name) || seen[name] {
			return nil, fmt.Errorf("%w: name index=%d", ErrSpellCatalogInvalid, index)
		}
		seen[name] = true
		entry := SpellCatalogEntry{
			Index:      index,
			MagicPower: byte(index + 1),
			Name:       name,
			Level:      teachSpellLevels[index],
			Execution:  spellCatalogExecution(index),
		}
		if metadata := legacyOffensiveSpellMetadata[index]; metadata.present {
			entry.Realm = metadata.realm
			entry.MPCost = metadata.mp
			entry.DiceCount = metadata.diceCount
			entry.DiceSides = metadata.diceSides
			entry.DicePlus = metadata.dicePlus
			entry.BonusType = metadata.bonusType
		}
		if err := entry.validate(); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// LegacySpellCatalog is an explicit source-named alias for SpellCatalog.
func LegacySpellCatalog() ([]SpellCatalogEntry, error) { return SpellCatalog() }

// SpellCatalogEntryAt resolves the canonical zero-based spell bit. It never
// accepts a one-based scroll field, avoiding an off-by-one permission grant.
func SpellCatalogEntryAt(index int) (SpellCatalogEntry, error) {
	entries, err := SpellCatalog()
	if err != nil {
		return SpellCatalogEntry{}, err
	}
	if index < 0 || index >= len(entries) {
		return SpellCatalogEntry{}, fmt.Errorf("%w: index=%d", ErrSpellCatalogUnavailable, index)
	}
	return entries[index], nil
}

// SpellCatalogEntryForMagicPower resolves an object's one-based MagicPower.
func SpellCatalogEntryForMagicPower(magicPower byte) (SpellCatalogEntry, error) {
	if magicPower < 1 || int(magicPower) > SpellCatalogSize {
		return SpellCatalogEntry{}, fmt.Errorf("%w: magicpower=%d", ErrSpellCatalogUnavailable, magicPower)
	}
	return SpellCatalogEntryAt(int(magicPower) - 1)
}

// SpellListResult is the durable, read-only projection of command4.c:info_2's
// known-spell line. Entries are sorted by source strcmp-compatible Go string
// order, just as info_2 sorts the selected spell names with qsort(strcmp).
// Changed is always false; no world state or resource is consumed.
type SpellListResult struct {
	Action    string              `json:"action"`
	ActorID   string              `json:"actor_id"`
	ActorName string              `json:"actor_name"`
	RoomID    int16               `json:"room_id"`
	Changed   bool                `json:"changed"`
	Spells    []SpellCatalogEntry `json:"spells"`
	Response  string              `json:"response"`
}

// SpellListProjection is a descriptive alias for callers that treat this as
// a pure query rather than a command receipt.
type SpellListProjection = SpellListResult

// SpellListProposal is bound to the complete canonical State and current
// catalog snapshot. Its private before field prevents a read receipt from
// being applied to a different actor identity or spell-bit state.
type SpellListProposal struct {
	Action    string
	ActorID   string
	ActorName string
	RoomID    int16
	Spells    []SpellCatalogEntry
	Response  string

	before State
}

func spellListActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validSpellCatalogName(actor.Body.Name) {
		return PlayerState{}, ErrSpellListActorAbsent
	}
	return actor, nil
}

func cloneSpellCatalogEntries(entries []SpellCatalogEntry) []SpellCatalogEntry {
	if entries == nil {
		return nil
	}
	cloned := make([]SpellCatalogEntry, len(entries))
	copy(cloned, entries)
	return cloned
}

func knownSpellEntries(body LegacyMonster, catalog []SpellCatalogEntry) []SpellCatalogEntry {
	known := make([]SpellCatalogEntry, 0, len(catalog))
	for _, entry := range catalog {
		if legacyInfoFlag(body.Spells[:], uint(entry.Index)) {
			known = append(known, entry)
		}
	}
	sort.SliceStable(known, func(i, j int) bool {
		if known[i].Name == known[j].Name {
			return known[i].Index < known[j].Index
		}
		return known[i].Name < known[j].Name
	})
	return known
}

func spellListResponse(entries []SpellCatalogEntry) string {
	var out strings.Builder
	out.WriteString("\n주문: ")
	if len(entries) == 0 {
		out.WriteString("없음.")
	} else {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name)
		}
		out.WriteString(strings.Join(names, ", "))
		out.WriteByte('.')
	}
	out.WriteByte('\n')
	return out.String()
}

func spellListResult(p SpellListProposal, actor PlayerState) SpellListResult {
	return SpellListResult{
		Action:    p.Action,
		ActorID:   p.ActorID,
		ActorName: actor.Body.Name,
		RoomID:    p.RoomID,
		Changed:   false,
		Spells:    cloneSpellCatalogEntries(p.Spells),
		Response:  p.Response,
	}
}

// PlanSpellList ports the read-only known-spell half of command4.c:info_2.
// The source info_2 continuation does not check blindness, class, level, MP
// or realm proficiency: it reports learned bits only. Those execution gates
// remain owned by their individual spell reducers and are not guessed here.
func (s State) PlanSpellList(actorID string) (SpellListProposal, error) {
	actor, err := spellListActor(s, actorID)
	if err != nil {
		return SpellListProposal{}, err
	}
	catalog, err := SpellCatalog()
	if err != nil {
		return SpellListProposal{}, err
	}
	spells := knownSpellEntries(actor.Body, catalog)
	return SpellListProposal{
		Action:    "spell_list",
		ActorID:   actorID,
		ActorName: actor.Body.Name,
		RoomID:    actor.Body.RoomID,
		Spells:    spells,
		Response:  spellListResponse(spells),
		before:    s.clone(),
	}, nil
}

// PlanKnownSpells is a source-neutral spelling alias for PlanSpellList.
func (s State) PlanKnownSpells(actorID string) (SpellListProposal, error) {
	return s.PlanSpellList(actorID)
}

// ProjectSpellList is the response-only form for callers that do not need to
// retain a proposal. It still uses the same source validation and canonical
// identity checks as the receipt path.
func (s State) ProjectSpellList(actorID string) (SpellListProjection, error) {
	proposal, err := s.PlanSpellList(actorID)
	if err != nil {
		return SpellListProjection{}, err
	}
	actor, err := spellListActor(s, actorID)
	if err != nil {
		return SpellListProjection{}, err
	}
	return spellListResult(proposal, actor), nil
}

// ApplySpellList validates the full pre-state and current source catalog, then
// returns an independent unchanged State. It acts as the pure receipt/replay
// boundary for a future Supabase command record; no RNG, timer or allocator is
// consulted and no effect is re-executed on retry.
func (s State) ApplySpellList(p SpellListProposal) (State, SpellListResult, error) {
	if p.Action != "spell_list" || p.ActorID == "" || p.RoomID == 0 && p.before.Version == 0 || p.Response == "" || p.before.Version != s.Version || !reflect.DeepEqual(s, p.before) {
		return State{}, SpellListResult{}, ErrSpellListStaleProposal
	}
	actor, err := spellListActor(s, p.ActorID)
	if err != nil || actor.Body.RoomID != p.RoomID || actor.Body.Name != p.ActorName {
		return State{}, SpellListResult{}, ErrSpellListStaleProposal
	}
	catalog, err := SpellCatalog()
	if err != nil {
		return State{}, SpellListResult{}, err
	}
	expected := knownSpellEntries(actor.Body, catalog)
	if !reflect.DeepEqual(p.Spells, expected) || p.Response != spellListResponse(expected) {
		return State{}, SpellListResult{}, ErrSpellListTampered
	}
	return s.clone(), spellListResult(p, actor), nil
}

// SpellList is the one-call reducer-shaped read-only API.
func (s State) SpellList(actorID string) (State, SpellListResult, error) {
	p, err := s.PlanSpellList(actorID)
	if err != nil {
		return State{}, SpellListResult{}, err
	}
	return s.ApplySpellList(p)
}

// ListKnownSpells is a descriptive alias for SpellList.
func (s State) ListKnownSpells(actorID string) (State, SpellListResult, error) {
	return s.SpellList(actorID)
}
