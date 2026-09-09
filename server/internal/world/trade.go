package world

import (
	"fmt"
	"strings"
)

const (
	tradeNamedObjectFlag = 47 // ONAMED: named objects cannot be traded.
	tradeMiscObjectType  = 13 // MISC: damaged-shot guard does not apply.
)

// NPCTradeResult is the durable response for one MTRADE attempt. Canonical
// IDs are retained for audit/replay but are never accepted from a terminal
// client. Response is already rendered for the original command boundary.
type NPCTradeResult struct {
	Action          string `json:"action"`
	NPCID           string `json:"npc_id"`
	NPCName         string `json:"npc_name"`
	OfferedItemID   string `json:"offered_item_id"`
	OfferedItemName string `json:"offered_item_name"`
	RewardRootID    string `json:"reward_root_id,omitempty"`
	RewardItemName  string `json:"reward_item_name,omitempty"`
	Rewarded        bool   `json:"rewarded"`
	Quest           byte   `json:"quest,omitempty"`
	QuestExperience int32  `json:"quest_experience,omitempty"`
	Response        string `json:"response"`
}

func cloneNPCTradeOffers(in []NPCTradeOffer) []NPCTradeOffer {
	if in == nil {
		return nil
	}
	out := make([]NPCTradeOffer, len(in))
	for i, offer := range in {
		objects := cloneObjects([]LegacyObject{offer.Wanted})
		out[i].Wanted = objects[0]
		if offer.Reward != nil {
			reward := cloneObjects([]LegacyObject{*offer.Reward})[0]
			out[i].Reward = &reward
		}
	}
	return out
}

func validateTradeObject(object LegacyObject, label string) error {
	if object.Name == "" {
		return fmt.Errorf("%s object name required", label)
	}
	var visit func(LegacyObject, int) error
	visit = func(current LegacyObject, depth int) error {
		if depth > 64 {
			return fmt.Errorf("%s object tree too deep", label)
		}
		for _, child := range current.Contents {
			if child.Name == "" {
				return fmt.Errorf("%s object child name required", label)
			}
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(object, 0)
}

func validateNPCTradeOffers(npc NPCState) error {
	if npc.TradeOffers == nil {
		return nil
	}
	if !flag(npc.Body.Flags[:], npcTradeFlag) {
		return fmt.Errorf("trade offers on non-trade NPC")
	}
	for i, offer := range npc.TradeOffers {
		if err := validateTradeObject(offer.Wanted, fmt.Sprintf("trade offer %d wanted", i)); err != nil {
			return err
		}
		if offer.Reward != nil {
			if err := validateTradeObject(*offer.Reward, fmt.Sprintf("trade offer %d reward", i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ImportNPCTradeOffers is the explicit migration of C's numeric carry pairs.
// The raw Body.Carry values are not consulted by gameplay after this step:
// catalog resolution must succeed for every referenced object, and an
// unresolved offer table remains a fail-closed boundary.
func (s State) ImportNPCTradeOffers(catalog SpawnCatalog) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if s.NPCs == nil || catalog == nil {
		return State{}, fmt.Errorf("canonical NPC trade migration requires NPCs and catalog")
	}
	for id, npc := range s.NPCs {
		if flag(npc.Body.Flags[:], npcTradeFlag) && npc.TradeOffers != nil {
			return State{}, fmt.Errorf("NPC %s trade offers already migrated", id)
		}
	}
	next := s.clone()
	for id, npc := range next.NPCs {
		if !flag(npc.Body.Flags[:], npcTradeFlag) {
			continue
		}
		seenCarry := map[int16]bool{}
		offers := make([]NPCTradeOffer, 0, len(npc.Body.Carry))
		for i := 0; i < 5; i++ {
			wantedID := npc.Body.Carry[i]
			if wantedID <= 0 || seenCarry[wantedID] {
				continue
			}
			seenCarry[wantedID] = true
			wanted, err := catalog.Object(wantedID)
			if err != nil {
				return State{}, fmt.Errorf("NPC %s wanted object %d: %w", id, wantedID, err)
			}
			if err := validateTradeObject(wanted, "wanted"); err != nil {
				return State{}, fmt.Errorf("NPC %s: %w", id, err)
			}
			offer := NPCTradeOffer{Wanted: wanted}
			rewardID := npc.Body.Carry[i+5]
			if rewardID > 0 {
				reward, err := catalog.Object(rewardID)
				if err != nil {
					return State{}, fmt.Errorf("NPC %s reward object %d: %w", id, rewardID, err)
				}
				if err := validateTradeObject(reward, "reward"); err != nil {
					return State{}, fmt.Errorf("NPC %s: %w", id, err)
				}
				offer.Reward = &reward
			}
			offers = append(offers, offer)
		}
		npc.TradeOffers = offers
		next.NPCs[id] = npc
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

func (s State) selectTradeNPC(actorID, name string, occurrence int) (string, bool, error) {
	if occurrence < 1 {
		return "", false, fmt.Errorf("invalid NPC occurrence")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || s.NPCs == nil {
		return "", false, fmt.Errorf("canonical NPC trade context required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, fmt.Errorf("NPC name required")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return "", false, fmt.Errorf("actor room absent")
	}
	found := 0
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if !exists || id == "" || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID {
			return "", false, fmt.Errorf("unresolved NPC trade identity")
		}
		if !strings.EqualFold(npc.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, true, nil
		}
	}
	return "", false, nil
}

func tradeItemMatches(offered, wanted LegacyObject) bool {
	// command10.c compares key[0] and name with strcmp. Names are admitted
	// case-insensitively at the terminal boundary for the existing Go command
	// convention; key[0] remains an exact identity discriminator.
	return strings.EqualFold(offered.Name, wanted.Name) && offered.Keys[0] == wanted.Keys[0]
}

func tradeItemIDs(s State) map[string]struct{} {
	used := map[string]struct{}{}
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

func tradeRejected(s State, result NPCTradeResult) (State, NPCTradeResult, error) {
	return s.clone(), result, nil
}

// TradeNPCByName performs the canonical bounded trade transition. Only
// explicit TradeOffers are admitted; Body.Carry is migration evidence and is
// never guessed back into a live exchange. Semantic rejections return a
// durable no-op candidate, while malformed/unmigrated state returns an error.
func (s State) TradeNPCByName(actorID, itemName string, itemOccurrence int, npcName string, npcOccurrence int, allocate ShopPurchaseItemIDAllocator) (State, NPCTradeResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCTradeResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Items == nil {
		return State{}, NPCTradeResult{}, fmt.Errorf("online player with migrated items required")
	}
	npcID, found, err := s.selectTradeNPC(actorID, npcName, npcOccurrence)
	if err != nil {
		return State{}, NPCTradeResult{}, err
	}
	if !found {
		return tradeRejected(s, NPCTradeResult{Action: "trade-rejected", Response: "그것은 여기 없습니다.\r\n"})
	}
	npc := s.NPCs[npcID]
	base := NPCTradeResult{Action: "trade-rejected", NPCID: npcID, NPCName: npc.Body.Name}
	if !flag(npc.Body.Flags[:], npcTradeFlag) {
		base.Response = fmt.Sprintf("당신은 %s와 교역할 수 없습니다.\r\n", npc.Body.Name)
		return tradeRejected(s, base)
	}
	if npc.TradeOffers == nil {
		return State{}, NPCTradeResult{}, fmt.Errorf("canonical NPC trade offers required")
	}
	itemID, itemErr := selectInventoryRoot(*actor.Items, itemName, itemOccurrence, nil)
	if itemErr != nil {
		base.Response = "당신은 그런 물건을 갖고 있지 않습니다.\r\n"
		return tradeRejected(s, base)
	}
	offered := actor.Items.Items[itemID]
	base.OfferedItemID = itemID
	base.OfferedItemName = offered.Object.Name
	if flag(offered.Object.Flags[:], tradeNamedObjectFlag) {
		base.Response = "교역할 수 있는 물건이 아닙니다.\r\n"
		return tradeRejected(s, base)
	}
	if offered.Object.Type != tradeMiscObjectType && offered.Object.ShotsCurrent <= offered.Object.ShotsMax/10 {
		base.Response = fmt.Sprintf("%s가 \"난 그런거 필요없어요!\"라고 말합니다.\r\n", npc.Body.Name)
		return tradeRejected(s, base)
	}
	var selected *NPCTradeOffer
	for i := range npc.TradeOffers {
		if tradeItemMatches(offered.Object, npc.TradeOffers[i].Wanted) {
			selected = &npc.TradeOffers[i]
			break
		}
	}
	if selected == nil {
		base.Response = fmt.Sprintf("%s가 \"난 그런거 필요없어요!\"라고 말합니다.\r\n", npc.Body.Name)
		return tradeRejected(s, base)
	}
	if selected.Reward != nil && selected.Reward.Quest != 0 {
		quest := int(selected.Reward.Quest) - 1
		if quest < 0 || quest >= len(actor.Body.Quests)*8 {
			return State{}, NPCTradeResult{}, fmt.Errorf("unsupported trade quest %d", selected.Reward.Quest)
		}
		if _, ok := npcQuestExperience(selected.Reward.Quest); !ok {
			return State{}, NPCTradeResult{}, fmt.Errorf("unsupported trade quest %d", selected.Reward.Quest)
		}
		if flag(actor.Body.Quests[:], uint(quest)) {
			base.Response = "당신은 이미 임무를 완수했습니다.\r\n"
			return tradeRejected(s, base)
		}
	}

	// Remove the offered root and its complete canonical subtree first. A
	// reward is then materialized from its immutable template with fresh IDs;
	// both transitions are kept in the one returned candidate.
	empty := ItemCollection{Items: map[string]Item{}}
	plan, err := TransferItemRoots(*actor.Items, empty, []string{itemID})
	if err != nil {
		return State{}, NPCTradeResult{}, err
	}
	destination := plan.Source
	result := NPCTradeResult{Action: "trade-npc-item", NPCID: npcID, NPCName: npc.Body.Name, OfferedItemID: itemID, OfferedItemName: offered.Object.Name}
	if selected.Reward == nil {
		result.Response = fmt.Sprintf("%s가 \"고맙습니다! %s 필요했는데 잘됐군요. 그런데 당신에게 줄게 없는데..\"라고 말합니다.\r\n", npc.Body.Name, offered.Object.Name)
	} else {
		if allocate == nil {
			return State{}, NPCTradeResult{}, fmt.Errorf("trade reward ID allocator required")
		}
		used := tradeItemIDs(s)
		uniqueAllocate := func() (string, error) {
			id, err := allocate()
			if err != nil {
				return "", err
			}
			if id == "" {
				return "", fmt.Errorf("empty trade reward item ID")
			}
			if _, exists := used[id]; exists {
				return "", fmt.Errorf("duplicate trade reward item ID")
			}
			used[id] = struct{}{}
			return id, nil
		}
		rewardObjects := cloneObjects([]LegacyObject{*selected.Reward})
		reward, err := ImportItems(rewardObjects, uniqueAllocate)
		if err != nil {
			return State{}, NPCTradeResult{}, err
		}
		rewardRootID := reward.Inventory[0]
		rewardPlan, err := TransferItemRoots(reward, destination, reward.Inventory)
		if err != nil {
			return State{}, NPCTradeResult{}, err
		}
		destination = rewardPlan.Destination
		result.Rewarded = true
		result.RewardRootID = rewardRootID
		result.RewardItemName = selected.Reward.Name
		result.Response = fmt.Sprintf("%s가 당신에게 %s를 줍니다.\r\n", npc.Body.Name, selected.Reward.Name)
		if selected.Reward.Quest != 0 {
			experience, ok := npcQuestExperience(selected.Reward.Quest)
			if !ok { // validated before allocation; retain a defensive fence.
				return State{}, NPCTradeResult{}, fmt.Errorf("unsupported trade quest %d", selected.Reward.Quest)
			}
			result.Quest = selected.Reward.Quest
			result.QuestExperience = experience
			result.Response += fmt.Sprintf("임무를 완수했습니다! 버리지 마십시요.\r\n당신은 경험치 %d 를 얻었습니다.\r\n", experience)
		}
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &destination
	if result.Quest != 0 {
		quest := uint(result.Quest - 1)
		nextPlayer.Body.Quests[quest/8] |= 1 << (quest % 8)
		if int64(nextPlayer.Body.Experience)+int64(result.QuestExperience) > int64(^uint32(0)>>1) {
			return State{}, NPCTradeResult{}, fmt.Errorf("trade quest experience overflow")
		}
		nextPlayer.Body.Experience += result.QuestExperience
		if err := addUnassignedProficiency(&nextPlayer.Body, result.QuestExperience); err != nil {
			return State{}, NPCTradeResult{}, err
		}
	}
	next.Players[actorID] = nextPlayer
	if err := next.Validate(); err != nil {
		return State{}, NPCTradeResult{}, err
	}
	return next, result, nil
}
