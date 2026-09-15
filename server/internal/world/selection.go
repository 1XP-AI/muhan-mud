package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Selection is the bounded, read-only port of command10.c:selection (global
// alias “선택”).  It deliberately does not read LegacyMonster.Carry: an
// MPURIT merchant's stock is resolved once into the server-owned
// MerchantOffers catalog and remains an immutable object-template snapshot.

var (
	// ErrSelectionNPCStateUnresolved is returned when the command cannot prove
	// an NPC identity from the canonical same-room index.  Legacy room monster
	// slices are not a fallback identity source.
	ErrSelectionNPCStateUnresolved = errors.New("selection canonical NPC state unresolved")
	// ErrSelectionMerchantOffersUnresolved is returned for an MPURIT NPC whose
	// server-owned catalog is absent or has no entry for that canonical NPC ID.
	ErrSelectionMerchantOffersUnresolved = errors.New("selection merchant offers unresolved")
	// ErrSelectionMerchantOffersInvalid is returned when the supplied catalog
	// is not a valid canonical MerchantOffers snapshot.
	ErrSelectionMerchantOffersInvalid = errors.New("selection merchant offers invalid")
	// ErrSelectionStaleProposal prevents a read projection planned against one
	// room/NPC snapshot from being applied to a different snapshot.
	ErrSelectionStaleProposal = errors.New("stale selection proposal")
)

// Descriptive aliases keep callers from matching error strings and make the
// migration boundary explicit at call sites.
var (
	ErrSelectionNPCUnresolved          = ErrSelectionNPCStateUnresolved
	ErrSelectionCanonicalNPCUnresolved = ErrSelectionNPCStateUnresolved
	ErrSelectionCatalogMissing         = ErrSelectionMerchantOffersUnresolved
	ErrSelectionOffersUnresolved       = ErrSelectionMerchantOffersUnresolved
)

// SelectionItem is one deterministic display row.  Number is one-based and
// follows the order in the server-owned MerchantOffers slice.  Item IDs are
// intentionally absent: command10.c exposes names/prices, while a later
// purchase command resolves its own canonical item choice.
type SelectionItem struct {
	Number   int    `json:"number"`
	ItemName string `json:"item_name"`
	Price    int64  `json:"price"`
}

// SelectionListing is a descriptive spelling for callers that use the list
// vocabulary.  It is an alias so the receipt cannot drift into two schemas.
type SelectionListing = SelectionItem

// SelectionResult is the durable, read-only projection.  Changed is always
// false: a selection receipt records the observation but never mutates the
// world snapshot.  NoOp distinguishes source branches that only print a
// message (missing target, non-merchant, or empty stock).
type SelectionResult struct {
	Action        string          `json:"action"`
	RoomID        int16           `json:"room_id"`
	NPCID         string          `json:"npc_id,omitempty"`
	NPCName       string          `json:"npc_name,omitempty"`
	NPCOccurrence int             `json:"npc_occurrence,omitempty"`
	TargetFound   bool            `json:"target_found"`
	Found         bool            `json:"found"`
	Merchant      bool            `json:"merchant"`
	CatalogReady  bool            `json:"catalog_ready"`
	NoOp          bool            `json:"no_op"`
	Changed       bool            `json:"changed"`
	Items         []SelectionItem `json:"items"`
	Response      string          `json:"response"`
}

// SelectionProposal is a state-bound read candidate.  The private before
// snapshot and copied offer templates prevent a caller from applying a name
// lookup or a mutable catalog against a different committed world.
type SelectionProposal struct {
	ActorID       string
	RoomID        int16
	QueryName     string
	TargetName    string
	NPCID         string
	NPCName       string
	NPCOccurrence int
	TargetFound   bool
	Found         bool
	Merchant      bool
	CatalogReady  bool
	NoOp          bool
	Items         []SelectionItem
	Response      string

	before State
	offers []MerchantOffer
}

type SelectionCandidate = SelectionProposal
type SelectionOutcome = SelectionResult

func validSelectionName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func selectionActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		if s.NPCs == nil {
			return PlayerState{}, RoomState{}, fmt.Errorf("%w: %v", ErrSelectionNPCStateUnresolved, err)
		}
		return PlayerState{}, RoomState{}, err
	}
	if s.NPCs == nil {
		return PlayerState{}, RoomState{}, ErrSelectionNPCStateUnresolved
	}
	if actorID == "" {
		return PlayerState{}, RoomState{}, fmt.Errorf("selection actor required")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validSelectionName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online selection actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("selection actor room absent")
	}
	return actor, room, nil
}

// selectionNPCVisible follows find_crt's visibility order.  The room's
// canonical NPCIDs slice remains authoritative; legacy room monsters are not
// consulted.  The existing helper carries the same MPURIT-era source bits and
// is kept as the single visibility implementation for merchant commands.
func selectionNPCVisible(actor LegacyMonster, npc LegacyMonster) bool {
	return merchantNPCVisible(actor, npc)
}

func selectSelectionNPC(s State, actorID, name string, occurrence int) (string, bool, error) {
	if occurrence < 1 {
		return "", false, fmt.Errorf("invalid selection NPC occurrence")
	}
	if !validSelectionName(name) {
		return "", false, fmt.Errorf("selection NPC name required")
	}
	actor, room, err := selectionActor(s, actorID)
	if err != nil {
		return "", false, err
	}
	found := 0
	for _, npcID := range room.NPCIDs {
		npc, ok := s.NPCs[npcID]
		if !ok || npcID == "" || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validSelectionName(npc.Body.Name) {
			return "", false, fmt.Errorf("%w: unresolved NPC %q", ErrSelectionNPCStateUnresolved, npcID)
		}
		// This bounded port intentionally admits exact display-name matching;
		// prefix/key matching would make a read-only list depend on legacy keys
		// that are not canonical identity fields in State.
		if !selectionNPCVisible(actor.Body, npc.Body) || !strings.EqualFold(npc.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return npcID, true, nil
		}
	}
	return "", false, nil
}

func selectionOfferNamesSafe(object LegacyObject, label string) error {
	if err := validateMerchantOfferObject(object, label); err != nil {
		return err
	}
	count := 0
	var visit func(LegacyObject, int) error
	visit = func(current LegacyObject, depth int) error {
		count++
		if depth > merchantOfferTreeMaxDepth || count > merchantOfferTreeLimit {
			return fmt.Errorf("%s object tree limits exceeded", label)
		}
		if current.Name != "" && !validSelectionName(current.Name) {
			return fmt.Errorf("%s object name contains unsafe text", label)
		}
		for _, child := range current.Contents {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(object, 0)
}

func validateSelectionOffers(s State, offers MerchantOffers) error {
	if offers == nil {
		return fmt.Errorf("%w: canonical merchant offer catalog required", ErrSelectionMerchantOffersUnresolved)
	}
	if err := validateMerchantOffers(s, offers); err != nil {
		return fmt.Errorf("%w: %v", ErrSelectionMerchantOffersInvalid, err)
	}
	for npcID, stock := range offers {
		for index, object := range stock {
			if err := selectionOfferNamesSafe(object, fmt.Sprintf("merchant %s offer %d", npcID, index)); err != nil {
				return fmt.Errorf("%w: %v", ErrSelectionMerchantOffersInvalid, err)
			}
		}
	}
	return nil
}

func selectionRender(npcName string, merchant bool, items []SelectionItem) string {
	if !merchant {
		return fmt.Sprintf("%s는 아무것도 없습니다.\n", npcName)
	}
	if len(items) == 0 {
		return fmt.Sprintf("%s은 팔 물건이 없습니다.\n", npcName)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s의 물건들:\n", npcName)
	for _, item := range items {
		fmt.Fprintf(&out, "%d) %-22s    %d냥\n", item.Number, item.ItemName, item.Price)
	}
	out.WriteByte('\n')
	return out.String()
}

func selectionItems(stock []MerchantOffer) []SelectionItem {
	items := make([]SelectionItem, 0, len(stock))
	for index, object := range stock {
		price := int64(object.Value)
		if price < 10 {
			price = 10
		}
		items = append(items, SelectionItem{Number: index + 1, ItemName: object.Name, Price: price})
	}
	return items
}

func selectionCopyItems(items []SelectionItem) []SelectionItem {
	if items == nil {
		return nil
	}
	out := make([]SelectionItem, len(items))
	copy(out, items)
	return out
}

func selectionResult(p SelectionProposal) SelectionResult {
	return SelectionResult{
		Action:        "selection",
		RoomID:        p.RoomID,
		NPCID:         p.NPCID,
		NPCName:       p.NPCName,
		NPCOccurrence: p.NPCOccurrence,
		TargetFound:   p.TargetFound,
		Found:         p.Found,
		Merchant:      p.Merchant,
		CatalogReady:  p.CatalogReady,
		NoOp:          p.NoOp,
		Changed:       false,
		Items:         selectionCopyItems(p.Items),
		Response:      p.Response,
	}
}

// PlanSelection resolves one visible, canonical same-room NPC and projects
// its immutable MerchantOffers stock. npcOccurrence is one-based and follows
// the canonical room NPC order; the compact ByName helper defaults to one.
func (s State) PlanSelection(actorID, npcName string, npcOccurrence int, offers MerchantOffers) (SelectionProposal, error) {
	_, room, err := selectionActor(s, actorID)
	if err != nil {
		return SelectionProposal{}, err
	}
	if npcOccurrence < 1 {
		return SelectionProposal{}, fmt.Errorf("invalid selection NPC occurrence")
	}
	if !validSelectionName(npcName) {
		return SelectionProposal{}, fmt.Errorf("selection NPC name required")
	}
	p := SelectionProposal{
		ActorID:       actorID,
		RoomID:        room.Resource.ID,
		QueryName:     npcName,
		NPCOccurrence: npcOccurrence,
		Response:      "",
		before:        s.clone(),
	}
	npcID, found, err := selectSelectionNPC(s, actorID, npcName, npcOccurrence)
	if err != nil {
		return SelectionProposal{}, err
	}
	p.TargetFound, p.Found = found, found
	if !found {
		p.NoOp = true
		p.Response = "그런 사람은 없습니다.\n"
		return p, nil
	}
	npc := s.NPCs[npcID]
	p.NPCID, p.NPCName = npcID, npc.Body.Name
	p.Merchant = flag(npc.Body.Flags[:], merchantPurchaseFlag)
	if !p.Merchant {
		p.NoOp = true
		p.Response = selectionRender(p.NPCName, false, nil)
		return p, nil
	}
	if err := validateSelectionOffers(s, offers); err != nil {
		return SelectionProposal{}, err
	}
	stock, ok := offers[npcID]
	if !ok {
		return SelectionProposal{}, fmt.Errorf("%w: merchant %s catalog entry required", ErrSelectionMerchantOffersUnresolved, npcID)
	}
	p.CatalogReady = true
	// MerchantOffers is server-owned, but its nested slices are still mutable
	// Go values. Copy the complete selected template graph before rendering so
	// Apply and receipt output cannot observe a later caller mutation.
	p.offers = cloneObjects(stock)
	p.Items = selectionItems(p.offers)
	p.Response = selectionRender(p.NPCName, true, p.Items)
	if len(p.Items) == 0 {
		p.NoOp = true
	}
	return p, nil
}

// PlanSelectionByName is the concise one-occurrence API used by command
// adapters that do not expose the optional source occurrence slot.
func (s State) PlanSelectionByName(actorID, npcName string, offers MerchantOffers) (SelectionProposal, error) {
	return s.PlanSelection(actorID, npcName, 1, offers)
}

// PlanMerchantSelection is a descriptive alias that keeps this list command
// separate from the existing purchase reducer.
func (s State) PlanMerchantSelection(actorID, npcName string, npcOccurrence int, offers MerchantOffers) (SelectionProposal, error) {
	return s.PlanSelection(actorID, npcName, npcOccurrence, offers)
}

// ApplySelection verifies the exact planning snapshot and returns a cloned,
// semantically unchanged State. It never reloads Carry, reads a catalog map,
// allocates IDs, or performs a purchase side effect.
func (s State) ApplySelection(p SelectionProposal) (State, SelectionResult, error) {
	if p.ActorID == "" || p.RoomID == 0 && p.before.Version == 0 || p.NPCOccurrence < 1 || p.Response == "" || p.before.Version != s.Version || !reflect.DeepEqual(s, p.before) {
		return State{}, SelectionResult{}, ErrSelectionStaleProposal
	}
	if p.TargetFound != p.Found || (p.Found && (p.NPCID == "" || p.NPCName == "")) || (!p.Found && (p.NPCID != "" || p.NPCName != "")) {
		return State{}, SelectionResult{}, fmt.Errorf("%w: invalid selection identity", ErrSelectionStaleProposal)
	}
	if !p.Merchant && (p.CatalogReady || len(p.Items) != 0 || len(p.offers) != 0) {
		return State{}, SelectionResult{}, fmt.Errorf("%w: non-merchant selection has stock", ErrSelectionStaleProposal)
	}
	if p.Merchant && !p.CatalogReady {
		return State{}, SelectionResult{}, fmt.Errorf("%w: merchant catalog not captured", ErrSelectionStaleProposal)
	}
	if p.Merchant {
		items := selectionItems(p.offers)
		if !reflect.DeepEqual(items, p.Items) || p.Response != selectionRender(p.NPCName, true, items) {
			return State{}, SelectionResult{}, fmt.Errorf("%w: selection templates changed", ErrSelectionStaleProposal)
		}
	} else {
		if p.Response != selectionRender(p.NPCName, false, nil) && p.Found {
			return State{}, SelectionResult{}, fmt.Errorf("%w: selection response changed", ErrSelectionStaleProposal)
		}
		if !p.Found && p.Response != "그런 사람은 없습니다.\n" {
			return State{}, SelectionResult{}, fmt.Errorf("%w: selection response changed", ErrSelectionStaleProposal)
		}
	}
	return s.clone(), selectionResult(p), nil
}

// ListMerchantOffers is the one-call read-only reducer boundary.
func (s State) ListMerchantOffers(actorID, npcName string, npcOccurrence int, offers MerchantOffers) (State, SelectionResult, error) {
	p, err := s.PlanSelection(actorID, npcName, npcOccurrence, offers)
	if err != nil {
		return State{}, SelectionResult{}, err
	}
	return s.ApplySelection(p)
}

// SelectMerchantOffers is the compact one-occurrence spelling used by
// callers that do not need to expose the source occurrence slot.
func (s State) SelectMerchantOffers(actorID, npcName string, offers MerchantOffers) (State, SelectionResult, error) {
	return s.ListMerchantOffers(actorID, npcName, 1, offers)
}
