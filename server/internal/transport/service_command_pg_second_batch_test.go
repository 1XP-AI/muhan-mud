package transport

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresMerchantPurchaseCommandPersistsRewardAndReplays(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	var merchantFlags [8]byte
	merchantFlags[world.MerchantPurchaseFlag/8] |= 1 << (world.MerchantPurchaseFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "상점방"}},
			PlayerIDs: []string{"actor"},
			NPCIDs:    []string{"merchant"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100},
			Online: true,
			Items:  &world.ItemCollection{Items: map[string]world.Item{"old": {Object: world.LegacyObject{Name: "낡은 물건"}}}, Inventory: []string{"old"}},
		}},
		NPCs: map[string]world.NPCState{"merchant": {
			Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: merchantFlags},
		}},
	}
	offers := world.MerchantOffers{"merchant": {{Name: "검", Value: 7, Weight: 2}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-merchant-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "actor")
	first, err := owners.ExecuteMerchantPurchaseLineWithOptions(ctx, store, worldID, "merchant-pg-1", lease, "상인 검 구입", session.MerchantPurchaseOptions{Offers: offers})
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.MerchantPurchaseResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Price != 10 || !strings.Contains(result.Response, "검") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteMerchantPurchaseLineWithOptions(ctx, store, worldID, "merchant-pg-1", lease, "상인 검 구입", session.MerchantPurchaseOptions{Offers: offers})
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || saved.Players["actor"].Body.Gold != 90 || len(saved.Players["actor"].Items.Inventory) != 2 {
		t.Fatalf("saved=%+v err=%v", saved.Players["actor"], err)
	}
}

func TestPostgresNPCTalkCommandPersistsEventAndReplays(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{200: serviceRoomState(200, "광장", [8]byte{}, "actor", "observer")},
		Players: map[string]world.PlayerState{
			"actor":    {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 200}, Online: true},
		},
		NPCs: map[string]world.NPCState{"guide": {
			Body: world.LegacyMonster{Name: "Guide", Type: 1, RoomID: 200, Talk: "어서 오세요"},
		}},
	}
	room := state.Rooms[200]
	room.NPCIDs = []string{"guide"}
	state.Rooms[200] = room
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-npc-talk-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "actor")
	first, err := owners.ExecuteNPCTalkLine(ctx, store, worldID, "npc-talk-pg-1", lease, "대화 Guide")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.NPCTalkResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Event == nil || !strings.Contains(result.Response, "어서 오세요") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteNPCTalkLine(ctx, store, worldID, "npc-talk-pg-1", lease, "대화 Guide")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestPostgresGroupTalkCommandPersistsOrderedEventsAndReplays(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{200: serviceRoomState(200, "광장", [8]byte{}, "leader", "follower")},
		Players: map[string]world.PlayerState{
			"leader": {
				Body:         world.LegacyMonster{Name: "Leader", Type: 0, RoomID: 200},
				Online:       true,
				FollowerIDs:  []string{"follower"},
				FollowerRefs: []world.EntityRef{{Kind: "player", ID: "follower"}},
			},
			"follower": {Body: world.LegacyMonster{Name: "Follower", Type: 0, RoomID: 200}, Online: true, FollowingID: "leader"},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-group-talk-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "leader")
	first, err := owners.ExecuteGroupTalkLine(ctx, store, worldID, "group-talk-pg-1", lease, "그룹말 hello")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.GroupTalkResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Broadcast || len(result.Events) != 2 || result.Events[0].RecipientID != "follower" || result.Events[1].RecipientID != "leader" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteGroupTalkLine(ctx, store, worldID, "group-talk-pg-1", lease, "그룹말 hello")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
