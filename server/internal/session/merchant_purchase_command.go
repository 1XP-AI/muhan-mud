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

// ErrUnsupportedMerchantPurchaseLine is returned before a receipt exists for
// lines outside command10.c's suffix form: <NPC> <item> 구입.
var ErrUnsupportedMerchantPurchaseLine = errors.New("line is not an implemented merchant purchase command")

// MerchantPurchaseCommand keeps only display-name input and positive
// occurrence selectors. Canonical NPC/item identities are resolved by the
// world reducer from its committed snapshot and the server-owned offer table.
type MerchantPurchaseCommand struct {
	NPCName        string
	ItemName       string
	NPCOccurrence  int
	ItemOccurrence int
}

type merchantPurchaseRequest struct {
	Kind           string `json:"kind"`
	Line           string `json:"line"`
	NPCName        string `json:"npc_name"`
	ItemName       string `json:"item_name"`
	NPCOccurrence  int    `json:"npc_occurrence"`
	ItemOccurrence int    `json:"item_occurrence"`
	OffersDigest   string `json:"offers_digest"`
}

// MerchantPurchaseOptions is the server-owned migration input. It is never
// read from terminal payloads; a nil/missing catalog makes an MPURIT NPC fail
// closed as unresolved legacy Carry state.
type MerchantPurchaseOptions struct {
	Offers world.MerchantOffers
}

// ParseMerchantPurchaseLine admits the source suffix order and an explicit
// occurrence extension: <NPC> <item> 구입, or <NPC> <item> <NPC-occurrence>
// <item-occurrence> 구입. Quoted display names remain one token.
func ParseMerchantPurchaseLine(line string) (MerchantPurchaseCommand, bool) {
	if !utf8.ValidString(line) {
		return MerchantPurchaseCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return MerchantPurchaseCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || (len(tokens) != 3 && len(tokens) != 5) || tokens[len(tokens)-1] != "구입" {
		return MerchantPurchaseCommand{}, false
	}
	if tokens[0] == "" || strings.TrimSpace(tokens[0]) != tokens[0] || tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return MerchantPurchaseCommand{}, false
	}
	command := MerchantPurchaseCommand{NPCName: tokens[0], ItemName: tokens[1], NPCOccurrence: 1, ItemOccurrence: 1}
	if len(tokens) == 5 {
		npcOccurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || npcOccurrence < 1 {
			return MerchantPurchaseCommand{}, false
		}
		itemOccurrence, err := strconv.ParseUint(tokens[3], 10, 31)
		if err != nil || itemOccurrence < 1 {
			return MerchantPurchaseCommand{}, false
		}
		command.NPCOccurrence = int(npcOccurrence)
		command.ItemOccurrence = int(itemOccurrence)
	}
	return command, true
}

// IsMerchantPurchaseLine is a descriptive parser helper for transport
// dispatchers. The central parser owns classification and is intentionally a
// separate file boundary.
func IsMerchantPurchaseLine(line string) bool {
	_, ok := ParseMerchantPurchaseLine(line)
	return ok
}

func merchantOffersDigest(offers world.MerchantOffers) (string, error) {
	raw, err := json.Marshal(offers)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:]), nil
}

// ExecuteMerchantPurchaseLine accepts an optional server-owned catalog. The
// variadic form permits callers that are still wiring the migration boundary
// to fail closed without inventing runtime Body.Carry behavior.
func (o *Ownership) ExecuteMerchantPurchaseLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.MerchantOffers) (storage.WorldReceipt, error) {
	if len(catalogs) > 1 {
		return storage.WorldReceipt{}, errors.New("multiple merchant offer catalogs")
	}
	var offers world.MerchantOffers
	if len(catalogs) == 1 {
		offers = catalogs[0]
	}
	return o.executeMerchantPurchaseLine(ctx, store, worldID, commandID, lease, line, offers)
}

// ExecuteMerchantPurchaseLineWithOptions is the explicit form for callers
// that want the migration dependency visible at the call site.
func (o *Ownership) ExecuteMerchantPurchaseLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options MerchantPurchaseOptions) (storage.WorldReceipt, error) {
	return o.executeMerchantPurchaseLine(ctx, store, worldID, commandID, lease, line, options.Offers)
}

func (o *Ownership) executeMerchantPurchaseLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, offers world.MerchantOffers) (storage.WorldReceipt, error) {
	command, ok := ParseMerchantPurchaseLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMerchantPurchaseLine
	}
	digest, err := merchantOffersDigest(offers)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	request, err := json.Marshal(merchantPurchaseRequest{
		Kind: "merchant-purchase", Line: line, NPCName: command.NPCName, ItemName: command.ItemName,
		NPCOccurrence: command.NPCOccurrence, ItemOccurrence: command.ItemOccurrence, OffersDigest: digest,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		allocation := 0
		next, result, err := state.PurchaseMerchantByName(actorID, command.NPCName, command.NPCOccurrence, command.ItemName, command.ItemOccurrence, offers, func() (string, error) {
			allocation++
			return fmt.Sprintf("merchant-%s-%d", commandID, allocation), nil
		})
		if err != nil {
			return nil, nil, err
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return nextRaw, response, err
	})
}
