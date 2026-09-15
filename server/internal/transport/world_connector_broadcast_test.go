package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesGlobalBroadcastWithOptOuts(t *testing.T) {
	store := connectorExpressionLookFixture(t)
	s, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	for id := range s.Players {
		player := s.Players[id]
		player.Body.Class = 4
		player.Body.Level = 20
		player.Body.HPMax = 100
		player.Body.HPCurrent = 100
		player.Body.Daily[0] = world.LegacyDaily{Max: 2, Current: 2, LastTime: 1000}
		player.Items = &world.ItemCollection{Items: map[string]world.Item{}}
		s.Players[id] = player
	}
	actor := s.Players["a"]
	actor.Body.Flags[49/8] |= 1 << (49 % 8) // PBRSND
	s.Players["a"] = actor
	target := s.Players["b"]
	target.Body.Flags[3/8] |= 1 << (3 % 8) // PNOBRD
	s.Players["b"] = target
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw

	_, broadcaster, optedOut, observer := connectorThreeWorldConnections(t, store, "broadcast-world")
	text, err := broadcaster.Submit(context.Background(), "잡담 안녕하세요")
	if err != nil || !strings.Contains(text, "Alice> 안녕하세요") {
		t.Fatalf("actor response=%q err=%v", text, err)
	}
	select {
	case event := <-observer.events:
		if !strings.Contains(event, "Alice> 안녕하세요") {
			t.Fatalf("global event=%q", event)
		}
	default:
		t.Fatal("global event missing for opted-in observer")
	}
	select {
	case event := <-optedOut.events:
		t.Fatalf("opted-out player received event=%q", event)
	default:
	}
	select {
	case event := <-broadcaster.events:
		t.Fatalf("broadcaster received duplicate event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
