package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedDMFollowLine = errors.New("line is not an implemented DM follow command")

// DMFollowCommand is the parser-facing form of dm6.c:dm_follow.
// C parse() takes the last token as the verb: `<괴물> [*따르기|*cfollow]`.
type DMFollowCommand struct {
	Verb       string
	NPCName    string
	Occurrence int
}

type dmFollowLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Verb       string `json:"verb"`
	NPCName    string `json:"npc_name,omitempty"`
	Occurrence int    `json:"occurrence,omitempty"`
}

func validDMFollowLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

func looksLikeDMFollowOccurrence(token string) bool {
	if token == "" {
		return false
	}
	for i, r := range token {
		if (r == '+' || r == '-') && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ParseDMFollowLine(line string) (DMFollowCommand, bool) {
	if !validDMFollowLine(line) {
		return DMFollowCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 {
		return DMFollowCommand{}, false
	}
	verb := tokens[len(tokens)-1]
	if verb != "*따르기" && verb != "*cfollow" {
		return DMFollowCommand{}, false
	}
	command := DMFollowCommand{Verb: verb, Occurrence: 1}
	rest := tokens[:len(tokens)-1]
	if len(rest) >= 1 && looksLikeDMFollowOccurrence(rest[len(rest)-1]) {
		candidate := rest[len(rest)-1]
		if strings.HasPrefix(candidate, "+") {
			return DMFollowCommand{}, false
		}
		occurrence, parseErr := strconv.ParseInt(candidate, 10, 31)
		if parseErr != nil || occurrence < 1 {
			return DMFollowCommand{}, false
		}
		command.Occurrence = int(occurrence)
		rest = rest[:len(rest)-1]
	}
	switch len(rest) {
	case 0:
		return command, true
	case 1:
		if rest[0] == "" || strings.TrimSpace(rest[0]) != rest[0] {
			return DMFollowCommand{}, false
		}
		command.NPCName = rest[0]
		return command, true
	default:
		return DMFollowCommand{}, false
	}
}

func IsDMFollowLine(line string) bool {
	_, ok := ParseDMFollowLine(line)
	return ok
}

func (o *Ownership) ExecuteDMFollowLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseDMFollowLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDMFollowLine
	}
	payload, err := json.Marshal(dmFollowLineRequest{
		Kind: "dm-follow", Line: line, Verb: command.Verb, NPCName: command.NPCName, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanDMFollow(actorID, command.Verb, command.NPCName, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.DMFollowResult
		if !proposal.Changed {
			result = world.DMFollowResult{
				Action: proposal.Action, ActorID: proposal.ActorID, NPCID: proposal.NPCID,
				NPCName: proposal.NPCName, Verb: proposal.Verb, Occurrence: proposal.Occurrence,
				Response: proposal.Response, Changed: false,
			}
			next = state
		} else {
			next, result, err = state.ApplyDMFollow(proposal)
			if err != nil {
				return nil, nil, err
			}
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return nextRaw, response, err
	})
}
