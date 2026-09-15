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

var ErrUnsupportedSocialLine = errors.New("line is not an implemented social status command")

// SocialCommand is the bounded command5.c social projection. Long is only
// meaningful for the explicit `누구 l` source form.
type SocialCommand struct {
	Kind string
	Long bool
}

type socialLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Long bool   `json:"long,omitempty"`
}

func validSocialLine(line string) bool {
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

// ParseSocialLine admits the existing bare social aliases and command5.c's
// exact optional long token. It deliberately does not accept prefixes,
// uppercase variants, or any extra argument.
func ParseSocialLine(line string) (SocialCommand, bool) {
	if !validSocialLine(line) {
		return SocialCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil {
		return SocialCommand{}, false
	}
	switch {
	case len(tokens) == 1 && tokens[0] == "누구":
		return SocialCommand{Kind: "who"}, true
	case len(tokens) == 2 && tokens[0] == "누구" && tokens[1] == "l":
		return SocialCommand{Kind: "who", Long: true}, true
	case len(tokens) == 1 && (tokens[0] == "그룹" || tokens[0] == "무리"):
		return SocialCommand{Kind: "group"}, true
	default:
		return SocialCommand{}, false
	}
}

func IsSocialLine(line string) bool {
	_, ok := ParseSocialLine(line)
	return ok
}

func socialCommand(line string) (SocialCommand, bool) {
	return ParseSocialLine(line)
}

func (o *Ownership) ExecuteSocialLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, familyCatalog ...world.FamilyCatalog) (storage.WorldReceipt, error) {
	if len(familyCatalog) > 1 {
		return storage.WorldReceipt{}, ErrUnsupportedSocialLine
	}
	command, ok := socialCommand(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSocialLine
	}
	payload, err := json.Marshal(socialLineRequest{Kind: command.Kind, Line: line, Long: command.Long})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var text string
		if command.Kind == "who" {
			options := world.PlayerWhoOptions{Long: command.Long}
			if len(familyCatalog) == 1 {
				options.FamilyCatalog = familyCatalog[0]
			}
			text, err = s.PlayerWhoWithOptions(actorID, options)
		} else {
			text, err = s.PlayerGroup(actorID)
		}
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
