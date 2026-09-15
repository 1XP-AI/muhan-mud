package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedSuicideLine = errors.New("line is not an implemented suicide command")

// SuicideCommand is the parser-facing form of command5.c:ply_suicide.
// C parse() takes the last token as the verb; this bounded adapter admits
// only the exact cmdlist-153 alias `목매달기`. The following password line
// is ExecuteSuicidePasswordLine, not ParseCommand.
type SuicideCommand struct {
	Alias string
}

type suicideLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
}

type suicidePasswordRequest struct {
	Kind    string `json:"kind"`
	Matched bool   `json:"matched"`
}

// SuicidePasswordOptions binds the account identity used by suicide case 2.
// The password line is authenticated here and never copied into the durable
// receipt payload.
type SuicidePasswordOptions struct {
	Accounts      Accounts
	PasswordStore PasswordChangeStore
	Name          string
}

func validSuicideLine(line string) bool {
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

// ParseSuicideLine admits the original Korean alias. Outer terminal
// whitespace is harmless; arguments, prefix forms, and English suicide
// remain outside this slice.
func ParseSuicideLine(line string) (SuicideCommand, bool) {
	if !validSuicideLine(line) || strings.TrimSpace(line) != "목매달기" {
		return SuicideCommand{}, false
	}
	return SuicideCommand{Alias: "목매달기"}, true
}

func IsSuicideLine(line string) bool {
	_, ok := ParseSuicideLine(line)
	return ok
}

// ExecuteSuicideLine starts the password-prompt continuation through the
// durable receipt boundary. C case 1 commits PREADI and RETURN param 2.
// Replay of the same command ID does not re-commit or re-prompt. Player
// files are not deleted.
func (o *Ownership) ExecuteSuicideLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseSuicideLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSuicideLine
	}
	payload, err := json.Marshal(suicideLineRequest{Kind: "suicide", Line: line, Alias: command.Alias})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanSuicide(actorID)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplySuicide(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

func matchSuicidePassword(ctx context.Context, opts SuicidePasswordOptions, line string) (bool, error) {
	password := []byte(line)
	defer clear(password)
	if opts.Accounts != nil {
		_, err := opts.Accounts.Authenticate(ctx, opts.Name, password)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, storage.ErrCredentials) || errors.Is(err, identity.ErrPassword) || errors.Is(err, identity.ErrName) {
			return false, nil
		}
		return false, err
	}
	if opts.PasswordStore != nil {
		_, hash, err := opts.PasswordStore.LookupCredential(ctx, opts.Name)
		defer clear(hash)
		if err != nil {
			if errors.Is(err, storage.ErrCredentials) || errors.Is(err, identity.ErrName) {
				return false, nil
			}
			return false, err
		}
		return identity.CheckPassword(hash, password), nil
	}
	return false, nil
}

// ExecuteSuicidePasswordLine is command5.c:suicide case 2. Authenticate
// compares the submitted line; a mismatch commits F_CLR(PREADI) and the
// typed reject without deleting files. A match stays fail-closed before
// param 3. The password is not stored in the receipt. Replay of the same
// command ID does not re-commit or re-prompt.
func (o *Ownership) ExecuteSuicidePasswordLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, opts SuicidePasswordOptions) (storage.WorldReceipt, error) {
	matched, err := matchSuicidePassword(ctx, opts, line)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	payload, err := json.Marshal(suicidePasswordRequest{Kind: "suicide-password", Matched: matched})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanSuicidePassword(actorID, matched)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplySuicidePassword(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}
