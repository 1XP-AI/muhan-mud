// Package game contains deterministic game rules without network or database IO.
package game

import (
	"errors"
	"strconv"
	"strings"
)

// Stats order follows create_ply: strength, dexterity, constitution,
// intelligence, piety. Racial adjustments apply after the base budget check.
type Stats [5]int

var ErrCreation = errors.New("invalid character creation choices")

func validBase(stats Stats) bool {
	sum := 0
	for _, value := range stats {
		if value < 3 || value > 18 {
			return false
		}
		sum += value
	}
	return sum <= 54
}

// ParseCreationStats rejects extra fields and partial numbers rather than
// reproducing the legacy atoi/trailing-field acceptance bugs.
func ParseCreationStats(line string) (Stats, error) {
	var stats Stats
	fields := strings.Fields(line)
	if len(fields) != len(stats) {
		return Stats{}, ErrCreation
	}
	for i, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return Stats{}, ErrCreation
		}
		stats[i] = value
	}
	if !validBase(stats) {
		return Stats{}, ErrCreation
	}
	return stats, nil
}

type CreationChoices struct {
	Male       bool
	Class      int
	Stats      Stats
	Weapon     int
	Chaotic    bool
	RaceChoice int
}

// Creation is the validated pre-level draft, not a persisted/live character.
// Level initialization (up_level/init_ply) must run before admission to a world.
type Creation struct {
	Male        bool
	Class       int
	Stats       Stats
	Chaotic     bool
	RaceID      int
	Proficiency [5]int
	RoomID      int
	Gold        int
}

func BuildCreation(choice CreationChoices) (Creation, error) {
	if choice.Class < 1 || choice.Class > 8 || choice.Weapon < 1 || choice.Weapon > 5 ||
		choice.RaceChoice < 1 || choice.RaceChoice > 8 || !validBase(choice.Stats) {
		return Creation{}, ErrCreation
	}
	// Menu order is not the persisted ID order (src/mtype.h).
	raceIDs := [...]int{1, 2, 8, 3, 7, 4, 5, 6}
	adjustments := [...]Stats{
		{1, 0, 0, 0, -1}, {-1, 0, -1, 2, 0}, {-1, 0, 0, 0, 1}, {0, 0, -1, 1, 0},
		{2, 0, 0, -1, -1}, {-1, 1, 0, 0, 0}, {0, 0, 1, 0, 0}, {1, -1, 1, -1, 0},
	}
	result := Creation{
		Male: choice.Male, Class: choice.Class, Stats: choice.Stats, Chaotic: choice.Chaotic,
		RaceID: raceIDs[choice.RaceChoice-1], RoomID: 1, Gold: 500,
	}
	for i, delta := range adjustments[choice.RaceChoice-1] {
		result.Stats[i] += delta
	}
	result.Proficiency[choice.Weapon-1] = 1024
	return result, nil
}
