package session

import (
	"context"
	"crypto/sha256"
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

// ErrUnsupportedSelectionLine is returned before a durable receipt for line
// forms outside command10.c's exact `선택 <NPC> [occurrence]` boundary.
var ErrUnsupportedSelectionLine = errors.New("line is not an implemented selection command")

// ErrUnsupportedSelectionCommandLine is a descriptive compatibility alias
// for adapters that name commands rather than terminal lines.
var ErrUnsupportedSelectionCommandLine = ErrUnsupportedSelectionLine

// SelectionCommand carries only the display-name selector.  Target is kept
// as a compatibility spelling for command adapters; both fields are filled
// from the same parsed token and neither is an NPC identity authority.
type SelectionCommand struct {
	NPCName    string
	Target     string
	Occurrence int
}

// SelectionOptions supplies the server-owned immutable MerchantOffers
// migration boundary.  A nil/missing catalog is allowed through parsing but
// fails closed in the world reducer for a selected MPURIT NPC.
type SelectionOptions struct {
	Offers world.MerchantOffers
}

type selectionLineRequest struct {
	Kind         string `json:"kind"`
	Line         string `json:"line"`
	NPCName      string `json:"npc_name"`
	Occurrence   int    `json:"occurrence"`
	OffersDigest string `json:"offers_digest"`
}

func validSelectionLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func parseSelectionOccurrence(token string) (int, bool) {
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

// ParseSelectionLine accepts the global.c alias and one exact display-name
// selector.  An explicit one-based occurrence is admitted because the legacy
// command passes cmnd->val[1] to find_crt; prefix/key matching remains outside
// this canonical identity slice.
func ParseSelectionLine(line string) (SelectionCommand, bool) {
	if !validSelectionLine(line) {
		return SelectionCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || (len(tokens) != 2 && len(tokens) != 3) || tokens[0] != "선택" {
		return SelectionCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return SelectionCommand{}, false
	}
	command := SelectionCommand{NPCName: tokens[1], Target: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		occurrence, ok := parseSelectionOccurrence(tokens[2])
		if !ok {
			return SelectionCommand{}, false
		}
		command.Occurrence = occurrence
	}
	return command, true
}

func IsSelectionLine(line string) bool {
	_, ok := ParseSelectionLine(line)
	return ok
}

func ParseSelectionCommandLine(line string) (SelectionCommand, bool) { return ParseSelectionLine(line) }
func IsSelectionCommandLine(line string) bool                        { return IsSelectionLine(line) }

func selectionOffersDigest(offers world.MerchantOffers) (string, error) {
	raw, err := json.Marshal(offers)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:]), nil
}

// ExecuteSelectionLine accepts an optional server-owned catalog.  The
// variadic form lets an unwired migration boundary fail closed without
// inventing Body.Carry behavior; more than one catalog is rejected before a
// receipt exists.
func (o *Ownership) ExecuteSelectionLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.MerchantOffers) (storage.WorldReceipt, error) {
	if len(catalogs) > 1 {
		return storage.WorldReceipt{}, errors.New("multiple selection offer catalogs")
	}
	var offers world.MerchantOffers
	if len(catalogs) == 1 {
		offers = catalogs[0]
	}
	return o.executeSelectionLine(ctx, store, worldID, commandID, lease, line, offers)
}

// ExecuteSelectionLineWithOptions is the explicit dependency-visible form
// used by callers wiring the canonical MerchantOffers migration.
func (o *Ownership) ExecuteSelectionLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options SelectionOptions) (storage.WorldReceipt, error) {
	return o.executeSelectionLine(ctx, store, worldID, commandID, lease, line, options.Offers)
}

func (o *Ownership) executeSelectionLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, offers world.MerchantOffers) (storage.WorldReceipt, error) {
	command, ok := ParseSelectionLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSelectionLine
	}
	digest, err := selectionOffersDigest(offers)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	request, err := json.Marshal(selectionLineRequest{
		Kind: "selection", Line: line, NPCName: command.NPCName, Occurrence: command.Occurrence, OffersDigest: digest,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanSelection(actorID, command.NPCName, command.Occurrence, offers)
		if err != nil {
			return nil, nil, err
		}
		_, result, err := state.ApplySelection(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		// The selection reducer is read-only. Return the exact loaded bytes so a
		// no-state receipt cannot rewrite unrelated map/slice encoding.
		return raw, response, err
	})
}

func (o *Ownership) ExecuteSelectionCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.MerchantOffers) (storage.WorldReceipt, error) {
	return o.ExecuteSelectionLine(ctx, store, worldID, commandID, lease, line, catalogs...)
}

func (o *Ownership) ExecuteSelectionCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options SelectionOptions) (storage.WorldReceipt, error) {
	return o.ExecuteSelectionLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}

func (o *Ownership) ExecuteSelection(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.MerchantOffers) (storage.WorldReceipt, error) {
	return o.ExecuteSelectionLine(ctx, store, worldID, commandID, lease, line, catalogs...)
}
