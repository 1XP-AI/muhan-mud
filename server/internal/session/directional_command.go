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
	// C parse() last-token 주워/버려/꺼내/넣어 is get/drop, not move. Live
	// Submit classifies those as CommandItemMutation first; a direct
	// ExecuteDirectionalLine bypass still has to refuse them or
	// ParseDirectionalToken(fields[0]) would walk east on `동 버려`.
	// A mutation verb in the middle (`동 버려 extra`) is fail-closed
	// CommandUnknown on ParseCommand; this deny-list still has to
	// refuse the same bypass or fields[0] would walk.
	if lastTokenIsItemMutationVerb(line) || lineContainsItemMutationVerb(line) {
		return storage.WorldReceipt{}, ErrUnsupportedDirectionalLine
	}
	// C parse() 소지품/장비/장 is inventory/equipment, not move. Live Submit
	// classifies last-token forms as CommandItems first and rejects a
	// mid-verb form such as `동 소지품 extra`; a direct
	// ExecuteDirectionalLine bypass must refuse both or
	// ParseDirectionalToken(fields[0]) would walk east.
	if lastTokenIsItemsVerb(line) || lineContainsItemsVerb(line) {
		return storage.WorldReceipt{}, ErrUnsupportedDirectionalLine
	}
	// C parse() bank aliases are not movement. Live Submit classifies valid
	// forms as CommandBank and rejects a mid-verb form such as
	// `동 입금 extra`; a direct ExecuteDirectionalLine bypass must refuse any exact
	// bank alias token before ParseDirectionalToken(fields[0]) can walk.
	if lineContainsBankVerb(line) {
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

func lastTokenIsItemMutationVerb(line string) bool {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 {
		return false
	}
	return isItemMutationVerb(tokens[len(tokens)-1])
}

func lastTokenIsItemsVerb(line string) bool {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 {
		return false
	}
	return isItemsVerb(tokens[len(tokens)-1])
}

func lineContainsItemMutationVerb(line string) bool {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil {
		return false
	}
	return tokensContainItemMutationVerb(tokens)
}

func lineContainsItemsVerb(line string) bool {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil {
		return false
	}
	return tokensContainItemsVerb(tokens)
}

func lineContainsBankVerb(line string) bool {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil {
		return false
	}
	return tokensContainBankVerb(tokens)
}
