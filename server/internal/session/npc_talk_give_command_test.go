package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type sessionNPCTalkGiveCatalog struct{ object world.LegacyObject }

func (sessionNPCTalkGiveCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("monster lookup not used")
}

func (c sessionNPCTalkGiveCatalog) Object(id int16) (world.LegacyObject, error) {
	if id != 107 {
		return world.LegacyObject{}, errors.New("object not found")
	}
	return c.object, nil
}

func TestExecuteNPCTalkGiveUsesInjectedObjectCatalogAndAllocatorOnce(t *testing.T) {
	s, err := world.DecodeState(npcTalkCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	actor.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	s.Players["a"] = actor
	npc := s.NPCs["guide-one"]
	npc.Body.Flags[23/8] |= 1 << (23 % 8) // MTALKS
	npc.Body.Level = 0
	s.NPCs["guide-one"] = npc
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	talk := npcTalkSessionCatalog(t, "quest GIVE 107\n응답\n")
	objects := sessionNPCTalkGiveCatalog{object: world.LegacyObject{Name: "선물", Weight: 1}}
	allocations := 0
	options := NPCTalkOptions{
		Catalog:       &talk,
		ObjectCatalog: objects,
		Allocate: func() (string, error) {
			allocations++
			return "session-gift", nil
		},
	}
	first, err := owners.ExecuteNPCTalkLineWithOptions(context.Background(), store, "w", "npc-give-1", lease, "대화 Guide quest", options)
	if err != nil || first.Replayed || store.commits != 1 || allocations != 1 {
		t.Fatalf("first=%+v err=%v commits=%d allocations=%d", first, err, store.commits, allocations)
	}
	var result world.NPCTalkResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.GiveGranted || result.GiveItemID != "session-gift" || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteNPCTalkLineWithOptions(context.Background(), store, "w", "npc-give-1", lease, "대화 Guide quest", options)
	if err != nil || !replay.Replayed || store.commits != 1 || allocations != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d allocations=%d", replay, err, store.commits, allocations)
	}
}
