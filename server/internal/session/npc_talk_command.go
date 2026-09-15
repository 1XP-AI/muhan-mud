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

var ErrUnsupportedNPCTalkLine = errors.New("line is not an implemented NPC talk command")

// NPCTalkCommand is the bounded command8.c:talk line. The optional numeric
// token mirrors the legacy parser's positive cmnd->val[1] occurrence:
//
//	대화 <NPC> [occurrence] [topic]
//
// A non-numeric third token is the topic directly, so the user-facing common
// form remains `대화 <NPC> [topic]`.
type NPCTalkCommand struct {
	NPCName       string
	NPCOccurrence int
	Topic         string
}

type npcTalkLineRequest struct {
	Kind          string `json:"kind"`
	Line          string `json:"line"`
	NPCName       string `json:"npc_name"`
	NPCOccurrence int    `json:"npc_occurrence"`
	Topic         string `json:"topic,omitempty"`
}

// NPCTalkOptions binds the server-owned, immutable talk catalog to one
// command execution. The pointer is optional so the original no-topic API can
// continue to run without a catalog; when a topic targets an MTALKS NPC, the
// world reducer fails closed if this dependency is absent.
//
// TalkCatalog exposes only read-only lookup methods. ExecuteNPCTalkLineWithOptions
// copies the catalog value before entering the reducer so a caller cannot swap
// the pointed-to value while a command is being planned/applied.
type NPCTalkOptions struct {
	Catalog *world.TalkCatalog
	// Now and Roll are used only by admitted catalog CAST/GIVE actions. Roll is
	// captured into the durable world receipt; it is never called on replay.
	Now           int32
	Roll          func(int, int) int
	ObjectCatalog world.SpawnCatalog
	Allocate      func() (string, error)
}

// ParseNPCTalkLine accepts only the exact global alias 대화. The parser keeps
// target occurrence separate from topic so numeric command parser tokens do
// not accidentally become a topic. Quoted tokens are handled by the existing
// legacy tokenizer; multi-token topics are still one quoted token and are
// rejected by the world loader boundary when MTALKS is present.
func ParseNPCTalkLine(line string) (NPCTalkCommand, bool) {
	if !utf8.ValidString(line) {
		return NPCTalkCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u001b' || r == '\u2028' || r == '\u2029' {
			return NPCTalkCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 4 || tokens[0] != "대화" || strings.TrimSpace(tokens[1]) == "" {
		return NPCTalkCommand{}, false
	}
	command := NPCTalkCommand{NPCName: tokens[1], NPCOccurrence: 1}
	if len(tokens) == 2 {
		return command, true
	}
	if occurrence, ok := parseNPCTalkOccurrence(tokens[2]); ok {
		command.NPCOccurrence = occurrence
		if len(tokens) == 4 {
			command.Topic = tokens[3]
		}
		return command, true
	}
	if len(tokens) == 3 {
		if looksLikeNPCTalkOccurrence(tokens[2]) {
			return NPCTalkCommand{}, false
		}
		command.Topic = tokens[2]
		return command, true
	}
	return NPCTalkCommand{}, false
}

func looksLikeNPCTalkOccurrence(token string) bool {
	if token == "" {
		return false
	}
	digits := token
	if digits[0] == '+' || digits[0] == '-' {
		digits = digits[1:]
	}
	if digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseNPCTalkOccurrence(token string) (int, bool) {
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

func IsNPCTalkLine(line string) bool {
	_, ok := ParseNPCTalkLine(line)
	return ok
}

// ExecuteNPCTalkLine connects the pure NPC talk proposal/apply boundary to a
// durable ExecuteGame receipt. The variadic value form keeps existing callers
// source-compatible while allowing the migration boundary to pass one
// server-owned catalog. A second catalog is rejected rather than silently
// selecting one.
func (o *Ownership) ExecuteNPCTalkLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.TalkCatalog) (storage.WorldReceipt, error) {
	if len(catalogs) > 1 {
		return storage.WorldReceipt{}, errors.New("multiple NPC talk catalogs")
	}
	var options NPCTalkOptions
	if len(catalogs) == 1 {
		catalog := catalogs[0]
		options.Catalog = &catalog
	}
	return o.ExecuteNPCTalkLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}

// ExecuteNPCTalkLineWithCatalog is the explicit value-copy spelling for
// callers that already own a loaded catalog but do not need pointer lifetime
// semantics at their call site.
func (o *Ownership) ExecuteNPCTalkLineWithCatalog(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.TalkCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteNPCTalkLineWithOptions(ctx, store, worldID, commandID, lease, line, NPCTalkOptions{Catalog: &catalog})
}

// ExecuteNPCTalkLineWithOptions is the host-injection form used by
// WorldConnector. The reducer stores the deterministic event in the response
// envelope; a replay therefore never re-selects an NPC or repeats the
// PHIDDN/enemy transition.
func (o *Ownership) ExecuteNPCTalkLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options NPCTalkOptions) (storage.WorldReceipt, error) {
	command, ok := ParseNPCTalkLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNPCTalkLine
	}
	// TalkCatalog is immutable by contract, but copying the value here makes
	// the command independent of a caller replacing the pointed-to struct after
	// this method starts. The internal map is never exposed for mutation and
	// all public file lookups return owned topic slices.
	var catalog *world.TalkCatalog
	if options.Catalog != nil {
		catalogValue := *options.Catalog
		catalog = &catalogValue
	}
	payload, err := json.Marshal(npcTalkLineRequest{
		Kind:          "npc-talk",
		Line:          line,
		NPCName:       command.NPCName,
		NPCOccurrence: command.NPCOccurrence,
		Topic:         command.Topic,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var proposal world.NPCTalkProposal
		if catalog == nil {
			proposal, err = s.PlanNPCTalkProposal(actorID, command.NPCName, command.NPCOccurrence, command.Topic)
		} else {
			proposal, err = s.PlanNPCTalkProposalWithEffectOptions(actorID, command.NPCName, command.NPCOccurrence, command.Topic, *catalog, world.NPCTalkEffectOptions{Now: options.Now, Roll: options.Roll, ObjectCatalog: options.ObjectCatalog, Allocate: options.Allocate})
		}
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.NPCTalkResult
		if catalog == nil {
			next, result, err = s.ApplyNPCTalk(proposal)
		} else {
			next, result, err = s.ApplyNPCTalk(proposal, *catalog)
		}
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return state, response, err
	})
}

// ExecuteTalkLine is a short compatibility alias for callers that use the
// legacy command name rather than the canonical NPCTalk spelling.
func (o *Ownership) ExecuteTalkLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.TalkCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteNPCTalkLine(ctx, store, worldID, commandID, lease, line, catalogs...)
}

// ExecuteTalkLineWithOptions preserves the same explicit dependency boundary
// for callers that use the legacy alias.
func (o *Ownership) ExecuteTalkLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options NPCTalkOptions) (storage.WorldReceipt, error) {
	return o.ExecuteNPCTalkLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
