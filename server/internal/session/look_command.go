package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedLookLine = errors.New("line is not an implemented look command")

// LookCommand is the bounded command2.c:look form. An empty Target is the
// original bare room look. A Target is find_ext's prefix, then find_obj
// (inventory, ready[], room first_obj), exact 나, find_crt (first_mon),
// then upcased find_crt (first_ply). Occurrence is the one-based C val[1]
// default of 1. special objects remain fail-closed.
type LookCommand struct {
	Target     string
	Occurrence int
}

func validLookLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		switch r {
		case '\r', '\n', '\x00', '\x1b', '\u2028', '\u2029':
			return false
		}
	}
	return true
}

func isLookVerb(token string) bool {
	return token == "봐" || token == "보다" || token == "조사"
}

func parseLookRest(rest []string) (LookCommand, bool) {
	if len(rest) == 0 {
		return LookCommand{}, true
	}
	if rest[0] == "" || strings.TrimSpace(rest[0]) != rest[0] || strings.IndexFunc(rest[0], unicode.IsSpace) >= 0 {
		return LookCommand{}, false
	}
	if len(rest) == 1 {
		return LookCommand{Target: rest[0], Occurrence: 1}, true
	}
	if len(rest) != 2 {
		return LookCommand{}, false
	}
	occurrence, ok := parseLookAtOccurrence(rest[1])
	if !ok {
		return LookCommand{}, false
	}
	return LookCommand{Target: rest[0], Occurrence: occurrence}, true
}

// ParseLookLine admits exact 봐/보다/조사 as either the first token
// (`봐 동`) or the C parse() last token (`동 봐` / `늑대 봐`). Optionally one
// prefix and a positive occurrence. Last-token verbs win when both ends
// are look aliases. Extra tokens after a last-token verb peek the first
// remaining arg (`동 junk 봐` → 동), matching look()'s str[1]/val[1];
// prefix extras and controls stay fail-closed.
func ParseLookLine(line string) (LookCommand, bool) {
	if !validLookLine(line) {
		return LookCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 7 {
		return LookCommand{}, false
	}
	if isLookVerb(tokens[len(tokens)-1]) {
		return parseLookLastTokenRest(tokens[:len(tokens)-1])
	}
	if len(tokens) > 3 {
		return LookCommand{}, false
	}
	if isLookVerb(tokens[0]) {
		return parseLookRest(tokens[1:])
	}
	return LookCommand{}, false
}

func parseLookLastTokenRest(rest []string) (LookCommand, bool) {
	if len(rest) == 0 {
		return LookCommand{}, true
	}
	if len(rest) >= 2 {
		if occurrence, ok := parseLookAtOccurrence(rest[1]); ok {
			command, ok := parseLookRest(rest[:1])
			if !ok {
				return LookCommand{}, false
			}
			command.Occurrence = occurrence
			return command, true
		}
	}
	return parseLookRest(rest[:1])
}

func lastTokenIsLookVerb(line string) bool {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 {
		return false
	}
	return isLookVerb(tokens[len(tokens)-1])
}

func IsLookLine(line string) bool {
	_, ok := ParseLookLine(line)
	return ok
}

// ExecuteLookLine connects command2.c:look to a durable receipt. Bare aliases
// still render the actor room. An exit token (`봐 동` or C last-token `동 봐`)
// peeks the destination; inventory, equipped, or room object/creature
// (`봐 검` / `늑대 봐`) is find_obj/find_crt inspection; `봐 나` / `나 봐`
// is self-inspect then PLAYER PMARRI marriage + PLAYER standing
// description + PKNOWA glow + first_mon HP bands + is_enm_crt angry +
// first_enm combat + consider+equip_list
// and `봐 <player>` / `<player> 봐` is first_ply then PMARRI marriage +
// PLAYER standing description + PKNOWA glow (viewer PKNOWA &&
// alignment in [-100,100]; always 푸른 광채; MMALES 그/그녀) +
// hpcur < hpmax*3/10 가벼운 상처 (PMALES 그/그녀; not first_mon
// 9/10–2/10 bands) + equip_list without is_enm_crt/first_enm.
// Self/first_mon/first_ply inspect
// also derives C broadcast_rom for transport to fan out on first
// commit only, with per-occupant %M (PINVIS without PDINVI is 누군가;
// PDINVI+PINVIS may add (*)). The actor still receives inspect text.
// Actor RoomID does not change. Hour is server-owned. Replay does not
// re-render or re-fan-out. Missing or unmigrated targets, including nil
// Items /
// unmigrated ready[] / hpmax<=0 on self, first_mon, or first_ply /
// nil NPC Enemies on first_mon inspect / unmigrated first_enm names /
// PMARRI without a canonical spouse / empty or unmigrated self or
// first_ply description / unmigrated room occupants, create no receipt.
func (o *Ownership) ExecuteLookLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, hour int) (storage.WorldReceipt, error) {
	command, ok := ParseLookLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedLookLine
	}
	payload, err := json.Marshal(struct {
		Line       string
		Target     string
		Occurrence int
		Hour       int
	}{line, command.Target, command.Occurrence, hour})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanLook(actorID, command.Target, command.Occurrence, hour)
		if err != nil {
			if errors.Is(err, world.ErrLookExitUnresolved) || errors.Is(err, world.ErrLookDestinationUnresolved) || errors.Is(err, world.ErrLookObjectCreatureUnmigrated) || errors.Is(err, world.ErrRoomCombatUnmigrated) {
				return nil, nil, ErrUnsupportedLookLine
			}
			return nil, nil, err
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil {
			return nil, nil, err
		}
		state := raw
		if proposal.Changed {
			state, err = json.Marshal(next)
			if err != nil {
				return nil, nil, err
			}
		}
		response, err := json.Marshal(result.Response)
		return state, response, err
	})
}
