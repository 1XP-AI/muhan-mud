package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedFollowLine = errors.New("line is not an implemented follow command")

type followAction struct {
	verb   string
	target string
}

func parseFollowLine(line string) (followAction, bool) {
	fields := strings.Fields(line)
	if len(fields) == 1 && fields[0] == "내보내" {
		return followAction{verb: fields[0]}, true
	}
	if len(fields) != 2 {
		return followAction{}, false
	}
	if fields[0] != "따라" && fields[0] != "내보내" {
		return followAction{}, false
	}
	return followAction{verb: fields[0], target: fields[1]}, true
}

// ExecuteFollowLine connects command4's player-follow state transition to the
// durable command loop. The target is resolved from the same committed room
// snapshot used by FollowPlayer; client-supplied IDs are never trusted.
func (o *Ownership) ExecuteFollowLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	action, ok := parseFollowLine(line)
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
		if action.verb == "따라" {
			leaderID, err := s.SelectPlayerInRoom(actorID, action.target)
			if err != nil {
				response, marshalErr := json.Marshal("그런 사람은 여기 없습니다.\r\n")
				return raw, response, marshalErr
			}
			next, err = s.FollowPlayer(actorID, leaderID)
			if err == nil {
				text = fmt.Sprintf("%s을(를) 따라갑니다.\r\n", next.Players[leaderID].Body.Name)
			}
		} else {
			if action.target == "" {
				if s.Players[actorID].FollowingID == "" {
					response, marshalErr := json.Marshal("당신은 누구를 따라다니고 있지 않습니다.\r\n")
					return raw, response, marshalErr
				}
				leaderID := s.Players[actorID].FollowingID
				next, err = s.UnfollowPlayer(actorID)
				if err == nil {
					text = fmt.Sprintf("당신은 %s을(를) 그만 따라다니기로 하였습니다.\r\n", next.Players[leaderID].Body.Name)
				}
			} else {
				followerID, lookupErr := s.SelectPlayerInRoom(actorID, action.target)
				if lookupErr != nil {
					response, marshalErr := json.Marshal("그런 사람은 여기 없습니다.\r\n")
					return raw, response, marshalErr
				}
				if s.Players[followerID].FollowingID != actorID {
					response, marshalErr := json.Marshal("그 사람은 당신을 따라다니고 있지 않습니다.\r\n")
					return raw, response, marshalErr
				}
				next, err = s.UnfollowPlayer(followerID)
				if err == nil {
					text = fmt.Sprintf("당신은 %s을(를) 못 따라오도록 하였습니다.\r\n", next.Players[followerID].Body.Name)
				}
			}
		}
		if err != nil {
			response, marshalErr := json.Marshal(fmt.Sprintf("따라갈 수 없습니다: %s\r\n", err))
			return raw, response, marshalErr
		}
		state, marshalErr := json.Marshal(next)
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		response, marshalErr := json.Marshal(text)
		return state, response, marshalErr
	})
}
