package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedHelpLine is returned before a receipt is created when the
// line is not a help invocation, has too many arguments, or the process has
// not been given the immutable legacy help document directory. Help is a
// document projection, not a guessed response, so missing source files fail
// closed instead of silently returning a partial manual.
var ErrUnsupportedHelpLine = errors.New("line is not an implemented help command")

type helpLineRequest struct {
	Kind       string
	Actor      string
	Generation uint64
	Line       string
	Topic      string
}

// The command numbers below are the legacy help.<n> files from src/global.c.
// This first slice covers every command which already has a durable Go
// handler, plus movement and speech aliases that users commonly ask about.
// The bare manual, spell index and policy document remain available for the
// complete legacy catalog. Unsupported command topics deliberately return the
// same deterministic no-help text as command4.c until their command handler
// is admitted to the Go feature ledger.
var helpTopicFiles = map[string]string{
	// Movement (command number 1).
	"나가는길": "help.1", "광장": "help.1", "향로": "help.1", "수련장": "help.1",
	"현감에게": "help.1", "면회허가": "help.1", "반하도인": "help.1", "월광반": "help.1",
	"반야바라밀": "help.1", "8": "help.1", "북": "help.1", "ㅂ": "help.1",
	"2": "help.1", "남": "help.1", "ㄴ": "help.1", "6": "help.1", "동": "help.1",
	"ㄷ": "help.1", "4": "help.1", "서": "help.1", "ㅅ": "help.1", "북동": "help.1",
	"북서": "help.1", "남서": "help.1", "남동": "help.1", "9": "help.1", "위": "help.1",
	"ㅇ": "help.1", "3": "help.1", "밑": "help.1", "ㅁ": "help.1", "밖": "help.1", "나가": "help.1",
	// Durable Go command projections.
	"봐": "help.2", "보다": "help.2", "조사": "help.2",
	"끝": "help.3", "말": "help.4", "\"": "help.4", "'": "help.4",
	"주워": "help.5", "주": "help.5", "가져": "help.5", "꺼내": "help.5",
	"소지품": "help.6", "버려": "help.7", "넣어": "help.7", "누구": "help.8",
	"입어": "help.9", "벗어": "help.10", "장비": "help.11", "장": "help.11",
	"쥐어": "help.12", "잡아": "help.12", "무장": "help.13", "건강": "help.15", "점수": "help.15",
	"정보": "help.16", "따라": "help.18", "내보내": "help.19", "그룹": "help.20", "무리": "help.20",
	"공격": "help.23", "공": "help.23", "쳐": "help.23", "때려": "help.23", "시간": "help.49",
	"보관물": "help.63", "잔액": "help.63", "입금": "help.63", "출금": "help.63", "받아": "help.63",
}

func helpDocumentName(tokens []string) (string, string, bool) {
	if len(tokens) == 1 {
		return "helpfile", "", true
	}
	if len(tokens) != 2 {
		return "", "", false
	}
	switch tokens[1] {
	case "주술":
		return "spellfile", tokens[1], true
	case "정책":
		return "policy", tokens[1], true
	default:
		if name, ok := helpTopicFiles[tokens[1]]; ok {
			return name, tokens[1], true
		}
		// command4.c emits this message without reading a file when neither
		// a command nor a spell matches. Keeping it a receipt makes retries
		// deterministic while preserving the fail-closed source boundary.
		return "", tokens[1], true
	}
}

func readHelpDocument(source fs.FS, name string) (string, error) {
	if source == nil {
		return "", ErrUnsupportedHelpLine
	}
	data, err := fs.ReadFile(source, name)
	if err != nil {
		return "", fmt.Errorf("read help document %q: %w", name, err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("help document %q is not UTF-8", name)
	}
	return string(data), nil
}

// ExecuteHelpLine projects the original document-backed help() command onto
// the durable command/receipt boundary. It never mutates canonical state. A
// document source is injected by the process boundary (the container's
// /home/muhan/help directory in production and a repository help directory in
// local tests), so this package does not read arbitrary paths from clients.
func (o *Ownership) ExecuteHelpLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, source fs.FS) (storage.WorldReceipt, error) {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || (tokens[0] != "도움말" && tokens[0] != "?") {
		return storage.WorldReceipt{}, ErrUnsupportedHelpLine
	}
	name, topic, ok := helpDocumentName(tokens)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedHelpLine
	}
	request, err := json.Marshal(helpLineRequest{
		Kind: "help", Actor: lease.ActorID, Generation: lease.Generation,
		Line: strings.TrimSpace(line), Topic: topic,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		player, ok := state.Players[actorID]
		if !ok || !player.Online {
			return nil, nil, fmt.Errorf("online help actor required")
		}
		text := "그 명령어에 대한 도움말은 없습니다.\n"
		if name != "" {
			text, err = readHelpDocument(source, name)
			if err != nil {
				return nil, nil, err
			}
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
