package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func tradeCommandFixture(t *testing.T) []byte {
	t.Helper()
	var npcFlags [8]byte
	npcFlags[37/8] |= 1 << (37 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "교역방"}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"npc"},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"offered": {Object: world.LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13, ShotsMax: 10, ShotsCurrent: 10}}}, Inventory: []string{"offered"}}},
		},
		NPCs: map[string]world.NPCState{
			"npc": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: npcFlags}, TradeOffers: []world.NPCTradeOffer{{Wanted: world.LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13}, Reward: &world.LegacyObject{Name: "보상검", Type: 13}}}},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseTradeLineUsesLegacySuffixAndExplicitOccurrences(t *testing.T) {
	command, ok := ParseTradeLine(`"사과" "상인" 2 3 교환`)
	if !ok || command.ItemName != "사과" || command.NPCName != "상인" || command.ItemOccurrence != 2 || command.NPCOccurrence != 3 {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	parsed, err := ParseCommand("사과 상인 교환")
	if err != nil || parsed.Kind != CommandTrade {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	for _, line := range []string{"교환 사과 상인", "사과 상인", "사과 상인 0 1 교환", "사과 상인 1 교환"} {
		if _, ok := ParseTradeLine(line); ok {
			t.Fatalf("unsupported trade syntax accepted: %q", line)
		}
	}
}

func TestExecuteTradeLinePersistsAndReplaysWithoutReallocatingReward(t *testing.T) {
	store := &departureStore{state: tradeCommandFixture(t)}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteTradeLine(context.Background(), store, "w", "trade-1", lease, "사과 상인 교환")
	if err != nil || first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "보상검") {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Players["a"].Items.Inventory) != 1 || saved.Players["a"].Items.Inventory[0] != "trade-trade-1-1" {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteTradeLine(context.Background(), store, "w", "trade-1", lease, "사과 상인 교환")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	rejected, err := owners.ExecuteTradeLine(context.Background(), store, "w", "trade-2", lease, "사과 다른사람 교환")
	if err != nil || !strings.Contains(string(rejected.Response), "그것은 여기 없습니다") {
		t.Fatalf("missing NPC result=%s err=%v", rejected.Response, err)
	}
}
