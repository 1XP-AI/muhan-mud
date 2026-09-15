package world

import (
	"reflect"
	"strings"
	"testing"
)

func TestTransferSceneUsesDestinationLightAndSavedBlindness(t *testing.T) {
	for _, blind := range []bool{false, true} {
		s, in := canonicalTransferFixture()
		p := s.Players[in.ActorID]
		p.Items = &ItemCollection{Items: map[string]Item{}}
		if blind {
			p.Body.Flags[5] |= 4
		}
		s.Players[in.ActorID] = p
		dst := s.Rooms[2]
		dst.Resource.Flags[1] |= 1 // permanent darkness
		dst.PlayerIDs = []string{"lamp-owner"}
		s.Rooms[2] = dst
		s.Players["lamp-owner"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 2, Flags: [8]byte{0, 0, 2}}, Online: true}
		in.View = SceneOptions{ViewOptions: ViewOptions{Hour: 12, Class: 12, HasLight: false, HideName: true}, ViewerID: "wrong"}
		next, result, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
		if err != nil || result.Entry == nil {
			t.Fatalf("%+v %v", result, err)
		}
		want, err := next.CurrentScene(in.ActorID, 12)
		if err != nil || result.Entry.Scene != want || strings.Contains(result.Entry.Scene, "Alice님") {
			t.Fatalf("stale arrival scene: %q want %q err %v", result.Entry.Scene, want, err)
		}
		if strings.Contains(result.Entry.Scene, "검") == blind {
			t.Fatal("destination light/blindness ignored")
		}
	}
}

func TestArrivalSceneFailureReturnsNoPartialMove(t *testing.T) {
	s, in := canonicalTransferFixture()
	p := s.Players[in.ActorID]
	p.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players[in.ActorID] = p
	dst := s.Rooms[2]
	dst.Resource.Flags[1] |= 1 // light is required to decide visibility
	dst.PlayerIDs = []string{"unmigrated"}
	s.Rooms[2] = dst
	s.Players["unmigrated"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 2}, Online: true}
	next, result, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, TransferProposal{}) || s.Players[in.ActorID].Body.RoomID != 1 {
		t.Fatal("unresolved lighting leaked a partial move")
	}
}

func TestRespawnSceneUsesPostDeathEquipment(t *testing.T) {
	s := playerDeathFixture()
	r := s.Rooms[1008]
	r.Resource.Name = "dark respawn"
	r.Resource.Flags[1] |= 1
	s.Rooms[1008] = r
	p := s.Players["a"]
	item := p.Items.Items["weapon"]
	item.Object.Flags[1] |= 8 // dropped glowing equipment cannot light respawn
	p.Items.Items["weapon"] = item
	s.Players["a"] = p
	next, result, err := s.PlanPlayerDeath("a", "a", 100, SceneOptions{ViewOptions: ViewOptions{HasLight: true}}, nil, func(lo, hi int) int { return lo }, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := next.CurrentScene("a", 0)
	if err != nil || result.Entry.Scene != want || strings.Contains(result.Entry.Scene, "dark respawn") {
		t.Fatalf("pre-death lighting retained: %q want %q %v", result.Entry.Scene, want, err)
	}
}

func TestLoginSceneUsesSavedViewerSettings(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Online = false
	p.Body.Flags[0] |= 64 // hide room name
	s.Players["a"] = p
	r := s.Rooms[1]
	r.PlayerIDs = nil
	r.Resource.Name = "hidden room title"
	s.Rooms[1] = r
	next, entry, err := s.EnterSavedPlayerWithIDs("a", SceneOptions{}, nil, 100, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := next.CurrentScene("a", 0)
	if err != nil || entry.Scene != want || strings.Contains(entry.Scene, "hidden room title") {
		t.Fatalf("login view: %q want %q %v", entry.Scene, want, err)
	}
}
