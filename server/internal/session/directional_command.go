package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// command2.c:move missing dest after load_rom returns the same room pointer.
const DirectionalMapMissingResponse = "그쪽으로 지도가 없습니다. 신에게 연락해 주세요."

var (
	ErrUnsupportedDirectionalLine       = errors.New("line is not a direct movement command")
	ErrDirectionalDestinationUnresolved = errors.New(DirectionalMapMissingResponse)
)

// ExecuteDirectionalLine is the durable command boundary for direct movement.
// The client supplies only a line; room, destination, permissions, occupants,
// NPC relations, equipment, RNG and identity allocation come from the command
// snapshot/config. The response is produced from the committed candidate and
// is safe to replay from its receipt.
func (o *Ownership) ExecuteDirectionalLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, hour int, view world.SceneOptions, catalog world.SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (storage.WorldReceipt, error) {
	if _, ok := ParseLookLine(line); ok || lastTokenIsLookVerb(line) || lineContainsLookVerb(line) {
		return storage.WorldReceipt{}, ErrUnsupportedDirectionalLine
	}
	token, ok := world.ParseDirectionalToken(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDirectionalLine
	}
	payload, err := json.Marshal(struct {
		Line string
		Now  int32
		Hour int
	}{line, now, hour})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		player, exists := s.Players[actorID]
		if !exists || !player.Online {
			return nil, nil, errors.New("online movement actor absent")
		}
		room, exists := s.Rooms[player.Body.RoomID]
		if !exists {
			return nil, nil, errors.New("movement source room absent")
		}
		var destination *world.LegacyRoom
		index := world.SelectDirectionalExit(room.Resource.Exits, token)
		destExists := false
		if index >= 0 {
			if target, ok := s.Rooms[room.Resource.Exits[index].Destination]; ok {
				projected, projectErr := s.ProjectRoom(target.Resource.ID)
				if projectErr != nil {
					return nil, nil, projectErr
				}
				destination = &projected
				destExists = true
			}
		}
		input := world.TransferInput{
			ActorID: actorID,
			Movement: world.MovementInput{
				Prefix:      token,
				Destination: destination,
				Now:         int64(now),
				Traversal:   world.TraversalInput{PassageOptions: world.PassageOptions{Hour: hour}},
			},
			View: view,
		}
		input.View.ViewerID = actorID
		input.View.ViewOptions.Hour = hour
		next, step, reduceErr := s.DirectionalStep(input, catalog, roll, allocate)
		if reduceErr != nil {
			return nil, nil, reduceErr
		}
		if index >= 0 && !destExists && (step.Transfer.Movement.Moved || (!step.Transfer.Movement.Traversal.Stop && step.Death == nil)) {
			return nil, nil, ErrDirectionalDestinationUnresolved
		}
		state, reduceErr := json.Marshal(next)
		if reduceErr != nil {
			return nil, nil, reduceErr
		}
		responseText := strings.Join(step.Transfer.Movement.Messages, "")
		if step.Death != nil {
			responseText += step.Death.Entry.Scene
		} else if step.Transfer.Entry != nil {
			responseText += step.Transfer.Entry.Scene
		}
		if responseText == "" {
			responseText, reduceErr = next.CurrentScene(actorID, hour)
			if reduceErr != nil {
				return nil, nil, reduceErr
			}
		}
		response, reduceErr := json.Marshal(responseText)
		return state, response, reduceErr
	})
}
