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

func TestPostgresThirdBatchDescriptionAndLookupReceipts(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b"}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Race: 5, Level: 3}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Race: 5, Level: 7}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-third-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "a")

	description, err := owners.ExecuteDescriptionLine(ctx, store, worldID, "description-pg-1", lease, "바르게 서다 묘사")
	if err != nil || description.Replayed || description.Revision != 1 {
		t.Fatalf("description=%+v err=%v", description, err)
	}
	replay, err := owners.ExecuteDescriptionLine(ctx, store, worldID, "description-pg-1", lease, "바르게 서다 묘사")
	if err != nil || !replay.Replayed || string(replay.Response) != string(description.Response) {
		t.Fatalf("description replay=%+v err=%v", replay, err)
	}
	search, err := owners.ExecutePlayerSearchLine(ctx, store, worldID, "lookup-pg-1", lease, "사용자검색 Bob")
	if err != nil || search.Replayed || search.Revision != 2 {
		t.Fatalf("search=%+v err=%v", search, err)
	}
	var searchText string
	if err := json.Unmarshal(search.Response, &searchText); err != nil || !strings.Contains(searchText, "사용자: Bob") {
		t.Fatalf("search response=%q err=%v", searchText, err)
	}
	searchReplay, err := owners.ExecutePlayerSearchLine(ctx, store, worldID, "lookup-pg-1", lease, "사용자검색 Bob")
	if err != nil || !searchReplay.Replayed || string(searchReplay.Response) != string(search.Response) {
		t.Fatalf("search replay=%+v err=%v", searchReplay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || saved.Players["a"].Body.Description != "바르게 서다 " {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}

func TestPostgresThirdBatchReturnSquareReceiptReplays(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			7:    {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 7, Name: "사냥터"}}, PlayerIDs: []string{"a"}},
			1001: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1001, Name: "광장"}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 7, Class: 4, Level: 21, MPCurrent: 9}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-return-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "a")
	first, err := owners.ExecuteReturnSquareLine(ctx, store, worldID, "return-pg-1", lease, "귀환")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.ReturnSquareResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Moved || result.DestinationRoomID != 1001 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteReturnSquareLine(ctx, store, worldID, "return-pg-1", lease, "귀환")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || saved.Players["a"].Body.RoomID != 1001 || saved.Players["a"].Body.MPCurrent != 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}
