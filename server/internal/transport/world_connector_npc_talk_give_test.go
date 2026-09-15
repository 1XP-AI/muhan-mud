package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type transportNPCTalkGiveCatalog struct{ object world.LegacyObject }

func (transportNPCTalkGiveCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("monster lookup not used")
}

func (c transportNPCTalkGiveCatalog) Object(id int16) (world.LegacyObject, error) {
	if id != 107 {
		return world.LegacyObject{}, errors.New("object not found")
	}
	return c.object, nil
}

func TestWorldConnectorSubmitDispatchesNPCTalkGiveAndReplaysWithoutAllocator(t *testing.T) {
	store := npcTalkTopicConnectorStore(t)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["a"]
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	actor.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	state.Players["a"] = actor
	store.state, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	talk := npcTalkTransportCatalog(t, "quest GIVE 107\n응답\n")
	connector, actorConn, observer := npcTalkConnectorConnectionsWithCatalog(t, store, &talk)
	connector.config.Catalog = transportNPCTalkGiveCatalog{object: world.LegacyObject{Name: "선물", Weight: 1}}
	allocations := 0
	connector.config.Allocate = func() (string, error) {
		allocations++
		return "transport-gift", nil
	}

	output, err := actorConn.Submit(context.Background(), "대화 Guide quest")
	if err != nil || allocations != 1 || store.commits != 1 || output == "" {
		t.Fatalf("output=%q err=%v allocations=%d commits=%d", output, err, allocations, store.commits)
	}
	if !strings.Contains(output, "선물을 줍니다") {
		t.Fatalf("actor output=%q", output)
	}
	foundGift := false
	for i := 0; i < 3; i++ {
		got := <-observer.events
		if strings.Contains(got, "Guide가 Alice에게 선물을 줍니다.") {
			foundGift = true
		}
	}
	if !foundGift {
		t.Fatal("observer did not receive NPC gift event")
	}
	if _, err := actorConn.Submit(context.Background(), "대화 Guide quest"); err != nil || allocations != 1 || store.commits != 1 {
		t.Fatalf("replay err=%v allocations=%d commits=%d", err, allocations, store.commits)
	}
	select {
	case got := <-observer.events:
		t.Fatalf("replay fanned out event=%q", got)
	default:
	}
}
