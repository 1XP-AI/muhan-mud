package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedFollowLine = errors.New("line is not an implemented follow command")

var ErrUnsupportedFollowCommandLine = ErrUnsupportedFollowLine

type followAction struct {
	verb       string
	target     string
	occurrence int
}

// FollowCommand is the parser-facing form of command4.c:follow/lose.  The
// optional occurrence is the legacy cmnd->val[1] slot and is always
// one-based.
type FollowCommand struct {
	Verb       string
	Target     string
	Occurrence int
}

func validFollowLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func parseFollowOccurrence(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(token, 10, 31)
	if err != nil || value < 1 {
		return 0, false
	}
	return int(value), true
}

// parseFollowLine accepts the existing one-token 내보내 form, a named target,
// and the optional positive target occurrence. It intentionally keeps the old
// whitespace-token boundary; quoted or multi-token names are not inferred.
func parseFollowLine(line string) (followAction, bool) {
	if !validFollowLine(line) {
		return followAction{}, false
	}
	fields := strings.Fields(line)
	if len(fields) == 1 && fields[0] == "내보내" {
		return followAction{verb: fields[0], occurrence: 1}, true
	}
	if len(fields) != 2 && len(fields) != 3 {
		return followAction{}, false
	}
	if fields[0] != "따라" && fields[0] != "내보내" {
		return followAction{}, false
	}
	command := followAction{verb: fields[0], target: fields[1], occurrence: 1}
	if len(fields) == 3 {
		occurrence, ok := parseFollowOccurrence(fields[2])
		if !ok {
			return followAction{}, false
		}
		command.occurrence = occurrence
	}
	return command, true
}

// ParseFollowLine returns the parser-facing command without exposing a client
// identity; the world reducer performs the authoritative target selection.
func ParseFollowLine(line string) (FollowCommand, bool) {
	action, ok := parseFollowLine(line)
	if !ok {
		return FollowCommand{}, false
	}
	return FollowCommand{Verb: action.verb, Target: action.target, Occurrence: action.occurrence}, true
}

func IsFollowLine(line string) bool {
	_, ok := ParseFollowLine(line)
	return ok
}

func ParseFollowCommandLine(line string) (FollowCommand, bool) { return ParseFollowLine(line) }
func IsFollowCommandLine(line string) bool                     { return IsFollowLine(line) }

// ExecuteFollowLine connects command4's player-follow state transition to the
// durable command loop. The target is resolved from the same committed room
// snapshot used by FollowPlayer; client-supplied IDs are never trusted.
func (o *Ownership) ExecuteFollowLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	action, ok := ParseFollowLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFollowLine
	}
	payload, err := json.Marshal(struct{ Line string }{line})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var text string
		if action.Verb == "따라" {
			if action.Target == "나" {
				// command4.c treats 나 specially: it first resolves the actor,
				// then leaves the current leader, without ever creating a self
				// edge.  A self target while unattached is rejected before any
				// candidate state is built.
				if s.Players[actorID].FollowingID == "" {
					return nil, nil, fmt.Errorf("cannot follow self")
				}
				leaderID := s.Players[actorID].FollowingID
				next, err = s.UnfollowPlayer(actorID)
				if err != nil {
					return nil, nil, err
				}
				text = fmt.Sprintf("당신은 %s을(를) 그만 따라다니기로 하였습니다.\r\n", next.Players[leaderID].Body.Name)
			} else {
				leaderID, err := s.SelectFollowTarget(actorID, action.Target, action.Occurrence)
				if err != nil {
					return nil, nil, err
				}
				next, err = s.FollowPlayer(actorID, leaderID)
				if err != nil {
					return nil, nil, err
				}
				text = fmt.Sprintf("%s을(를) 따라갑니다.\r\n", next.Players[leaderID].Body.Name)
			}
		} else {
			if action.Target == "" {
				if s.Players[actorID].FollowingID == "" {
					response, marshalErr := json.Marshal("당신은 누구를 따라다니고 있지 않습니다.\r\n")
					return raw, response, marshalErr
				}
				leaderID := s.Players[actorID].FollowingID
				next, err = s.UnfollowPlayer(actorID)
				if err != nil {
					return nil, nil, err
				}
				text = fmt.Sprintf("당신은 %s을(를) 그만 따라다니기로 하였습니다.\r\n", next.Players[leaderID].Body.Name)
			} else {
				followerID, lookupErr := s.SelectFollowerInOrder(actorID, action.Target, action.Occurrence)
				if lookupErr != nil {
					return nil, nil, lookupErr
				}
				if s.Players[followerID].FollowingID != actorID {
					return nil, nil, fmt.Errorf("follower edge is not reciprocal")
				}
				next, err = s.UnfollowPlayer(followerID)
				if err != nil {
					return nil, nil, err
				}
				text = fmt.Sprintf("당신은 %s을(를) 못 따라오도록 하였습니다.\r\n", next.Players[followerID].Body.Name)
			}
		}
		if err != nil {
			return nil, nil, err
		}
		state, marshalErr := json.Marshal(next)
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		response, marshalErr := json.Marshal(text)
		return state, response, marshalErr
	})
}
