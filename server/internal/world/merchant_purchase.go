package world

import (
	"fmt"
	"reflect"
	"strings"
)

const (
	merchantPurchaseFlag      = 36 // MPURIT
	merchantNPCInvisibleFlag  = 2  // MINVIS
	merchantDMInvisibleFlag   = 10 // PDMINV
	merchantDetectInvisible   = 21 // PDINVI
	merchantOfferInvisible    = 2  // OINVIS
	merchantCapacityLimit     = 150
	merchantOfferTreeLimit    = 100000
	merchantOfferTreeMaxDepth = 64
)

// MerchantPurchaseFlag is MPURIT. A merchant's legacy Body.Carry values are
// migration evidence only; runtime purchase requires a separately imported
// MerchantOffers catalog keyed by canonical NPC identity.
const MerchantPurchaseFlag = merchantPurchaseFlag

// MerchantOffer is an immutable object template loaded from the source object
// catalog. It is an alias so callers cannot confuse a template with a live
// canonical Item identity.
type MerchantOffer = LegacyObject

// MerchantOffers is the explicit canonical migration boundary for MPURIT
// merchants. A present key with an empty slice means a known merchant with no
// stock; a missing key means its legacy Carry data remains unresolved.
type MerchantOffers map[string][]MerchantOffer

// MerchantPurchaseItemIDAllocator mints canonical IDs for the purchased
// reward graph. The allocator is called in deterministic preorder only after
// all admission checks pass.
type MerchantPurchaseItemIDAllocator = ShopPurchaseItemIDAllocator

// MerchantPurchaseResult is the durable response and audit projection for a
// purchase. The NPC template remains unchanged; PurchasedIDs belong to the
// buyer's canonical item graph.
type MerchantPurchaseResult struct {
	Action         string   `json:"action"`
	RoomID         int16    `json:"room_id"`
	NPCID          string   `json:"npc_id,omitempty"`
	NPCName        string   `json:"npc_name,omitempty"`
	ItemName       string   `json:"item_name,omitempty"`
	ItemOccurrence int      `json:"item_occurrence,omitempty"`
	NPCOccurrence  int      `json:"npc_occurrence,omitempty"`
	Price          int64    `json:"price,omitempty"`
	GoldBefore     int64    `json:"gold_before,omitempty"`
	GoldAfter      int64    `json:"gold_after,omitempty"`
	RewardRootID   string   `json:"reward_root_id,omitempty"`
	PurchasedIDs   []string `json:"purchased_ids,omitempty"`
	Response       string   `json:"response"`
}

// MerchantPurchaseProposal is a state-bound candidate. Reward IDs and the
// copied graph are fixed during planning, so ApplyMerchantPurchase never
// allocates or consults external templates and can fail closed on stale state.
type MerchantPurchaseProposal struct {
	ActorID        string
	RoomID         int16
	NPCID          string
	NPCName        string
	ItemName       string
	ItemOccurrence int
	NPCOccurrence  int
	Price          int64
	Rejected       bool
	Response       string
	RewardRootID   string
	PurchasedIDs   []string

	rewardItems    ItemCollection
	expectedGold   int32
	expectedItems  ItemCollection
	expectedNPC    NPCState
	expectedNPCIDs []string
	expectedRoomID int16
	hasExpectedNPC bool
}

func validateMerchantOfferObject(object LegacyObject, label string) error {
	if object.Name == "" || strings.TrimSpace(object.Name) != object.Name {
		return fmt.Errorf("%s object name required", label)
	}
	count := 0
	var visit func(LegacyObject, int) error
	visit = func(current LegacyObject, depth int) error {
		if depth > merchantOfferTreeMaxDepth {
			return fmt.Errorf("%s object tree too deep", label)
		}
		count++
		if count > merchantOfferTreeLimit {
			return fmt.Errorf("%s object tree too large", label)
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

func validateMerchantOffers(s State, offers MerchantOffers) error {
	if offers == nil {
		return fmt.Errorf("canonical merchant offers required")
	}
	if s.NPCs == nil {
		return fmt.Errorf("canonical NPC merchant state required")
	}
	for npcID, stock := range offers {
		if npcID == "" {
			return fmt.Errorf("merchant offer NPC identity required")
		}
		npc, ok := s.NPCs[npcID]
		if !ok || !flag(npc.Body.Flags[:], merchantPurchaseFlag) {
			return fmt.Errorf("merchant offer NPC is not an MPURIT NPC")
		}
		for index, object := range stock {
			if err := validateMerchantOfferObject(object, fmt.Sprintf("merchant %s offer %d", npcID, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ImportNPCMerchantOffers resolves MPURIT Carry template IDs exactly once
// against a stable object catalog. It never writes the legacy IDs back into a
// live State; callers must retain the returned catalog and pass it explicitly
// to the reducer. This keeps unresolved Carry data fail-closed at runtime.
func (s State) ImportNPCMerchantOffers(catalog SpawnCatalog) (MerchantOffers, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.NPCs == nil || catalog == nil {
		return nil, fmt.Errorf("merchant offer import requires canonical NPCs and catalog")
	}
	offers := make(MerchantOffers)
	for npcID, npc := range s.NPCs {
		if !flag(npc.Body.Flags[:], merchantPurchaseFlag) {
			continue
		}
		stock := make([]MerchantOffer, 0, len(npc.Body.Carry))
		seen := map[int16]bool{}
		for _, objectID := range npc.Body.Carry {
			if objectID <= 0 || seen[objectID] {
				continue
			}
			seen[objectID] = true
			object, err := catalog.Object(objectID)
			if err != nil {
				return nil, fmt.Errorf("NPC %s merchant object %d: %w", npcID, objectID, err)
			}
			if err := validateMerchantOfferObject(object, "merchant template"); err != nil {
				return nil, fmt.Errorf("NPC %s: %w", npcID, err)
			}
			stock = append(stock, object)
		}
		offers[npcID] = stock
	}
	if err := validateMerchantOffers(s, offers); err != nil {
		return nil, err
	}
	return offers, nil
}

// ImportMerchantOffers is a descriptive alias for migration callers.
func (s State) ImportMerchantOffers(catalog SpawnCatalog) (MerchantOffers, error) {
	return s.ImportNPCMerchantOffers(catalog)
}

func merchantActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Items == nil || actor.Body.Name == "" {
		return PlayerState{}, RoomState{}, fmt.Errorf("online player with migrated merchant inventory required")
	}
	if actor.Body.Stats[0] > 63 || actor.Body.Gold < 0 {
		return PlayerState{}, RoomState{}, fmt.Errorf("merchant actor legacy stat or gold range invalid")
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, fmt.Errorf("merchant actor room absent")
	}
	return actor, room, nil
}

func merchantNPCVisible(actor LegacyMonster, npc LegacyMonster) bool {
	if npc.Class >= 10 && flag(npc.Flags[:], merchantDMInvisibleFlag) {
		return false
	}
	return !flag(npc.Flags[:], merchantNPCInvisibleFlag) || flag(actor.Flags[:], merchantDetectInvisible)
}

func selectMerchantNPC(s State, actorID, name string, occurrence int) (string, bool, error) {
	if occurrence < 1 {
		return "", false, fmt.Errorf("invalid merchant NPC occurrence")
	}
	actor, room, err := merchantActor(s, actorID)
	if err != nil {
		return "", false, err
	}
	if s.NPCs == nil {
		return "", false, fmt.Errorf("canonical merchant NPC state required")
	}
	if name == "" || strings.TrimSpace(name) != name {
		return "", false, fmt.Errorf("merchant NPC name required")
	}
	found := 0
	for _, npcID := range room.NPCIDs {
		npc, ok := s.NPCs[npcID]
		if !ok || npcID == "" || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID {
			return "", false, fmt.Errorf("unresolved merchant NPC identity")
		}
		if !merchantNPCVisible(actor.Body, npc.Body) || !strings.EqualFold(npc.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return npcID, true, nil
		}
	}
	return "", false, nil
}

func merchantObjectWeight(object LegacyObject) (int, error) {
	count := 0
	var visit func(LegacyObject, int) (int, error)
	visit = func(current LegacyObject, depth int) (int, error) {
		if depth > merchantOfferTreeMaxDepth {
			return 0, fmt.Errorf("merchant offer object tree too deep")
		}
		count++
		if count > merchantOfferTreeLimit || current.Weight < 0 {
			return 0, fmt.Errorf("invalid merchant offer weight")
		}
		total := int(current.Weight)
		if flag(current.Flags[:], itemWeightlessFlag) {
			return total, nil
		}
		for _, child := range current.Contents {
			childWeight, err := visit(child, depth+1)
			if err != nil {
				return 0, err
			}
			if total > int(^uint(0)>>1)-childWeight {
				return 0, fmt.Errorf("merchant offer weight overflow")
			}
			total += childWeight
		}
		return total, nil
	}
	return visit(object, 0)
}

func merchantPurchaseResponse(npcName, itemName string, price int64) string {
	return fmt.Sprintf("당신은 %s에게 %d냥을 줍니다.\r\n%s%s \"고맙습니다. 여기 %s%s 있습니다.\"라고 말합니다.\r\n", npcName, price, npcName, merchantSubjectParticle(npcName), itemName, merchantSubjectParticle(itemName))
}

func merchantSubjectParticle(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return "가"
	}
	last := runes[len(runes)-1]
	if last >= 0xAC00 && last <= 0xD7A3 && (last-0xAC00)%28 != 0 {
		return "이"
	}
	return "가"
}

func merchantMissingItemResponse(npcName string) string {
	return fmt.Sprintf("%s가 \"미안합니다. 그런 물건은 갖고 있지 않습니다.\"라고 말합니다.\r\n", npcName)
}

func merchantNoStockResponse(npcName string) string {
	return fmt.Sprintf("%s는 팔 물건을 갖고 있지 않습니다.\r\n", npcName)
}

func merchantInsufficientGoldResponse(npcName string, price int64) string {
	return fmt.Sprintf("%s가 \"%d냥입니다. 깎아줄순 없습니다.\"라고 말합니다.\r\n", npcName, price)
}

func merchantItemIDs(s State) map[string]struct{} {
	used := make(map[string]struct{})
	add := func(items *ItemCollection) {
		if items == nil {
			return
		}
		for id := range items.Items {
			used[id] = struct{}{}
		}
	}
	for _, room := range s.Rooms {
		add(room.Items)
	}
	for _, player := range s.Players {
		add(player.Items)
	}
	for _, account := range s.BankAccounts {
		add(account.Items)
	}
	for _, npc := range s.NPCs {
		add(npc.Items)
	}
	return used
}

func merchantGraphIDs(items ItemCollection) []string {
	ids := make([]string, 0, len(items.Items))
	var visit func(string)
	visit = func(id string) {
		ids = append(ids, id)
		for _, child := range items.Items[id].Contents {
			visit(child)
		}
	}
	for _, root := range items.Inventory {
		visit(root)
	}
	return ids
}

func copyMerchantNPC(npc NPCState) NPCState {
	out := npc
	out.Enemies = append([]NPCEnemy(nil), npc.Enemies...)
	out.TradeOffers = cloneNPCTradeOffers(npc.TradeOffers)
	if npc.Items != nil {
		items := npc.Items.clone()
		out.Items = &items
	}
	return out
}

func merchantExpectedProposal(actor PlayerState, room RoomState, p *MerchantPurchaseProposal) {
	p.expectedGold = actor.Body.Gold
	p.expectedItems = actor.Items.clone()
	p.expectedRoomID = room.Resource.ID
	p.expectedNPCIDs = append([]string(nil), room.NPCIDs...)
}

// PlanMerchantPurchase performs all source admission checks and allocates a
// deterministic buyer-owned reward graph only on the successful branch.
func (s State) PlanMerchantPurchase(actorID, npcName string, npcOccurrence int, itemName string, itemOccurrence int, offers MerchantOffers, allocate MerchantPurchaseItemIDAllocator) (MerchantPurchaseProposal, error) {
	actor, room, err := merchantActor(s, actorID)
	if err != nil {
		return MerchantPurchaseProposal{}, err
	}
	if itemOccurrence < 1 {
		return MerchantPurchaseProposal{}, fmt.Errorf("invalid merchant item occurrence")
	}
	p := MerchantPurchaseProposal{
		ActorID:        actorID,
		RoomID:         room.Resource.ID,
		ItemName:       itemName,
		ItemOccurrence: itemOccurrence,
		NPCOccurrence:  npcOccurrence,
	}
	if itemName == "" || strings.TrimSpace(itemName) != itemName {
		return MerchantPurchaseProposal{}, fmt.Errorf("merchant item name required")
	}
	merchantExpectedProposal(actor, room, &p)
	npcID, found, err := selectMerchantNPC(s, actorID, npcName, npcOccurrence)
	if err != nil {
		return MerchantPurchaseProposal{}, err
	}
	if !found {
		p.Rejected = true
		p.Response = "그것은 여기 없습니다.\r\n"
		return p, nil
	}
	npc := s.NPCs[npcID]
	p.NPCID, p.NPCName, p.hasExpectedNPC = npcID, npc.Body.Name, true
	p.expectedNPC = copyMerchantNPC(npc)
	if !flag(npc.Body.Flags[:], merchantPurchaseFlag) {
		p.Rejected = true
		p.Response = fmt.Sprintf("당신은 %s에게서 물건을 구입할 수 없습니다.\r\n", npc.Body.Name)
		return p, nil
	}
	if err := validateMerchantOffers(s, offers); err != nil {
		return MerchantPurchaseProposal{}, err
	}
	stock, ok := offers[npcID]
	if !ok {
		return MerchantPurchaseProposal{}, fmt.Errorf("merchant %s offer templates unresolved", npcID)
	}
	if len(stock) == 0 {
		p.Rejected = true
		p.Response = merchantNoStockResponse(npc.Body.Name)
		return p, nil
	}
	detectInvisible := flag(actor.Body.Flags[:], merchantDetectInvisible)
	foundItem := 0
	var object LegacyObject
	for _, candidate := range stock {
		if flag(candidate.Flags[:], merchantOfferInvisible) && !detectInvisible {
			continue
		}
		if !strings.EqualFold(candidate.Name, itemName) {
			continue
		}
		foundItem++
		if foundItem == itemOccurrence {
			object = candidate
			break
		}
	}
	if object.Name == "" {
		p.Rejected = true
		p.Response = merchantMissingItemResponse(npc.Body.Name)
		return p, nil
	}
	weight, err := merchantObjectWeight(object)
	if err != nil {
		return MerchantPurchaseProposal{}, err
	}
	carriedWeight, err := actor.Items.Weight()
	if err != nil {
		return MerchantPurchaseProposal{}, err
	}
	capacity, err := actor.Items.CapacityCount()
	if err != nil {
		return MerchantPurchaseProposal{}, err
	}
	if carriedWeight > maxPlayerWeight(actor.Body) || weight > maxPlayerWeight(actor.Body)-carriedWeight || capacity > merchantCapacityLimit {
		p.Rejected = true
		p.Response = "당신은 더이상 가질 수 없습니다.\r\n"
		return p, nil
	}
	price := int64(object.Value)
	if price < 10 {
		price = 10
	}
	p.Price = price
	if int64(actor.Body.Gold) < price {
		p.Rejected = true
		p.Response = merchantInsufficientGoldResponse(npc.Body.Name, price)
		return p, nil
	}
	if allocate == nil {
		return MerchantPurchaseProposal{}, fmt.Errorf("merchant purchase item ID allocator required")
	}
	used := merchantItemIDs(s)
	uniqueAllocate := func() (string, error) {
		id, err := allocate()
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", fmt.Errorf("empty merchant purchase item ID")
		}
		if _, exists := used[id]; exists {
			return "", fmt.Errorf("duplicate merchant purchase item ID")
		}
		used[id] = struct{}{}
		return id, nil
	}
	reward, err := ImportItems([]LegacyObject{object}, uniqueAllocate)
	if err != nil {
		return MerchantPurchaseProposal{}, err
	}
	p.rewardItems = reward
	p.RewardRootID = reward.Inventory[0]
	p.PurchasedIDs = merchantGraphIDs(reward)
	p.Response = merchantPurchaseResponse(npc.Body.Name, object.Name, price)
	return p, nil
}

func merchantProposalResult(p MerchantPurchaseProposal, goldBefore, goldAfter int64) MerchantPurchaseResult {
	result := MerchantPurchaseResult{
		Action:         "merchant-purchase",
		RoomID:         p.RoomID,
		NPCID:          p.NPCID,
		NPCName:        p.NPCName,
		ItemName:       p.ItemName,
		ItemOccurrence: p.ItemOccurrence,
		NPCOccurrence:  p.NPCOccurrence,
		Price:          p.Price,
		GoldBefore:     goldBefore,
		GoldAfter:      goldAfter,
		RewardRootID:   p.RewardRootID,
		PurchasedIDs:   append([]string(nil), p.PurchasedIDs...),
		Response:       p.Response,
	}
	if p.Rejected {
		result.Action = "merchant-purchase-rejected"
	}
	return result
}

// ApplyMerchantPurchase verifies the exact planning snapshot and atomically
// attaches the prepared nested graph plus the gold debit. Rejected semantic
// branches are durable no-op candidates; unresolved templates are errors.
func (s State) ApplyMerchantPurchase(p MerchantPurchaseProposal) (State, MerchantPurchaseResult, error) {
	actor, room, err := merchantActor(s, p.ActorID)
	if err != nil {
		return State{}, MerchantPurchaseResult{}, err
	}
	if p.ActorID == "" || p.RoomID != room.Resource.ID || p.ItemOccurrence < 1 || p.NPCOccurrence < 1 || p.Response == "" || p.Price < 0 || actor.Body.Gold != p.expectedGold || !reflect.DeepEqual(*actor.Items, p.expectedItems) || !reflect.DeepEqual(room.NPCIDs, p.expectedNPCIDs) {
		return State{}, MerchantPurchaseResult{}, fmt.Errorf("stale or invalid merchant purchase proposal")
	}
	if p.hasExpectedNPC {
		npc, ok := s.NPCs[p.NPCID]
		if !ok || !reflect.DeepEqual(npc, p.expectedNPC) || npc.Body.Name != p.NPCName {
			return State{}, MerchantPurchaseResult{}, fmt.Errorf("stale merchant NPC proposal")
		}
	}
	if p.Rejected {
		result := merchantProposalResult(p, int64(actor.Body.Gold), int64(actor.Body.Gold))
		return s.clone(), result, nil
	}
	if p.NPCID == "" || p.NPCName == "" || p.ItemName == "" || p.Price < 10 || p.RewardRootID == "" || len(p.PurchasedIDs) == 0 || p.rewardItems.Items == nil || len(p.rewardItems.Inventory) == 0 {
		return State{}, MerchantPurchaseResult{}, fmt.Errorf("invalid merchant purchase success proposal")
	}
	if err := p.rewardItems.Validate(); err != nil {
		return State{}, MerchantPurchaseResult{}, err
	}
	if len(p.rewardItems.Items) != len(p.PurchasedIDs) || p.rewardItems.Inventory[0] != p.RewardRootID || !reflect.DeepEqual(p.PurchasedIDs, merchantGraphIDs(p.rewardItems)) {
		return State{}, MerchantPurchaseResult{}, fmt.Errorf("invalid merchant purchased graph")
	}
	if int64(actor.Body.Gold) < p.Price {
		return State{}, MerchantPurchaseResult{}, fmt.Errorf("merchant purchase gold changed")
	}
	destination := actor.Items.clone()
	for id, item := range p.rewardItems.Items {
		if _, exists := destination.Items[id]; exists {
			return State{}, MerchantPurchaseResult{}, fmt.Errorf("merchant purchase item ID collision")
		}
		destination.Items[id] = item
	}
	if err := insertShopInventoryRoot(&destination, p.RewardRootID); err != nil {
		return State{}, MerchantPurchaseResult{}, err
	}
	if err := destination.Validate(); err != nil {
		return State{}, MerchantPurchaseResult{}, err
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	goldBefore := int64(nextActor.Body.Gold)
	nextActor.Body.Gold = int32(goldBefore - p.Price)
	nextActor.Items = &destination
	next.Players[p.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, MerchantPurchaseResult{}, err
	}
	return next, merchantProposalResult(p, goldBefore, int64(nextActor.Body.Gold)), nil
}

// PurchaseMerchantByName is the one-call candidate/apply boundary used by
// session commands.
func (s State) PurchaseMerchantByName(actorID, npcName string, npcOccurrence int, itemName string, itemOccurrence int, offers MerchantOffers, allocate MerchantPurchaseItemIDAllocator) (State, MerchantPurchaseResult, error) {
	proposal, err := s.PlanMerchantPurchase(actorID, npcName, npcOccurrence, itemName, itemOccurrence, offers, allocate)
	if err != nil {
		return State{}, MerchantPurchaseResult{}, err
	}
	return s.ApplyMerchantPurchase(proposal)
}

// BuyMerchantItem is a compatibility-oriented descriptive alias for callers
// that use the shop naming convention.
func (s State) BuyMerchantItem(actorID, npcName string, npcOccurrence int, itemName string, itemOccurrence int, offers MerchantOffers, allocate MerchantPurchaseItemIDAllocator) (State, MerchantPurchaseResult, error) {
	return s.PurchaseMerchantByName(actorID, npcName, npcOccurrence, itemName, itemOccurrence, offers, allocate)
}
