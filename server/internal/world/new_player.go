package world

import (
	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// NewPlayerFromDraft is the checked conversion boundary for stored drafts.
// Call only for an identity that has not yet acquired a world character.
func NewPlayerFromDraft(name string, draft game.Creation) (PlayerState, error) {
	choices, err := draft.Choices()
	if err != nil {
		return PlayerState{}, err
	}
	return NewPlayer(name, choices)
}

// NewPlayer constructs an offline, level-one character from original creation
// choices. It never resets an existing character. World admission must still
// apply login timers/dailies and room entry, and persist the complete transition.
// Credentials belong to identity storage, never the world body.
func NewPlayer(name string, choices game.CreationChoices) (PlayerState, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return PlayerState{}, err
	}
	draft, err := game.BuildCreation(choices)
	if err != nil {
		return PlayerState{}, err
	}
	body := LegacyMonster{Name: canonical, Class: byte(draft.Class), Race: byte(draft.RaceID), RoomID: int16(draft.RoomID), Gold: int32(draft.Gold)}
	for i, value := range draft.Stats {
		body.Stats[i] = byte(value)
	}
	for i, value := range draft.Proficiency {
		body.Proficiency[i] = int32(value)
	}
	if draft.Male {
		body.Flags[12/8] |= 1 << (12 % 8)
	}
	if draft.Chaotic {
		body.Flags[28/8] |= 1 << (28 % 8)
	}
	body.Flags[18/8] |= 1 << (18 % 8) // PPROMP
	body.Flags[46/8] |= 1 << (46 % 8) // PLECHO
	body, err = RaisePlayerLevel(body)
	if err != nil {
		return PlayerState{}, err
	}
	items := &ItemCollection{Items: make(map[string]Item)}
	stats, err := items.CombatStats(body)
	if err != nil {
		return PlayerState{}, err
	}
	body.Armor, body.Thaco = byte(stats.Armor), byte(stats.Thaco)
	return PlayerState{Body: body, Items: items}, nil
}
