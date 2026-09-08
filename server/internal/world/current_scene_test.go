package world

import (
	"strings"
	"testing"
)

func TestCurrentSceneUsesCanonicalItemsAndViewer(t *testing.T) {
	s := playerDeathFixture()
	r := s.Rooms[1]
	r.Resource.Name = "광장"
	r.Items = &ItemCollection{Items: map[string]Item{"floor": {Object: LegacyObject{Name: "검"}}}, Inventory: []string{"floor"}}
	s.Rooms[1] = r
	text, err := s.CurrentScene("a", 12)
	if err != nil || !strings.Contains(text, "광장") || !strings.Contains(text, "검") || strings.Contains(text, "Alice님") {
		t.Fatalf("%q %v", text, err)
	}
	p := s.Players["a"]
	p.Body.Flags[5] |= 4
	s.Players["a"] = p
	text, err = s.CurrentScene("a", 12)
	if err != nil || strings.Contains(text, "검") {
		t.Fatalf("blind %q %v", text, err)
	}
}

func TestCurrentSceneResolvesOnlyNecessaryLighting(t *testing.T) {
	for _, mode := range []string{"daylight", "blind", "own-light", "other-light", "unknown-dark"} {
		t.Run(mode, func(t *testing.T) {
			s := playerDeathFixture()
			room := s.Rooms[1]
			room.PlayerIDs = append(room.PlayerIDs, "unknown", "lit")
			if mode != "daylight" {
				room.Resource.Flags[1] |= 1
			}
			s.Players["unknown"] = PlayerState{Body: LegacyMonster{Name: "Unknown", RoomID: 1}, Online: true}
			s.Players["lit"] = PlayerState{Body: LegacyMonster{Name: "Lit", RoomID: 1}, Online: true}
			p := s.Players["a"]
			switch mode {
			case "blind":
				p.Body.Flags[5] |= 4
			case "own-light":
				p.Body.Flags[2] |= 2
			case "other-light":
				other := s.Players["lit"]
				other.Body.Flags[2] |= 2
				s.Players["lit"] = other
			}
			s.Players["a"], s.Rooms[1] = p, room
			_, err := s.CurrentScene("a", 12)
			if (err != nil) != (mode == "unknown-dark") {
				t.Fatalf("%s lighting: %v", mode, err)
			}
		})
	}
}
