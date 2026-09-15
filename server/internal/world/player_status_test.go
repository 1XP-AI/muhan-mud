package world

import (
	"strings"
	"testing"
)

func TestPlayerStatusUsesAuthoritativeBodyAndEquipment(t *testing.T) {
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Body.Description = "광장"
	p.Body.Experience = 10
	s.Players["a"] = p
	text, err := s.PlayerStatus("a")
	if err != nil || !strings.Contains(text, "Alice") || !strings.Contains(text, "40/100") || !strings.Contains(text, "[방어력] 0") || !strings.Contains(text, "목표치") || !strings.Contains(text, "광장서") {
		t.Fatalf("status=%q err=%v", text, err)
	}
	blind := s.Players["a"]
	blind.Body.Flags[5] |= 1 << (42 - 40)
	s.Players["a"] = blind
	text, err = s.PlayerStatus("a")
	if err != nil || text != "당신은 눈이 멀어 있습니다!\r\n" {
		t.Fatalf("blind status=%q err=%v", text, err)
	}
}

func TestExperienceThresholdMatchesLegacyHighLevelFormula(t *testing.T) {
	if got := ExperienceThreshold(127); got != neededExperience[126] || ExperienceThreshold(128) != neededExperience[126]+5000000 {
		t.Fatalf("threshold127=%d threshold128=%d", got, ExperienceThreshold(128))
	}
}

func TestSelectPlayerInRoomUsesExactNameAndSkipsActor(t *testing.T) {
	s := npcAttackFixture(t)
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1, Type: 0}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	if got, err := s.SelectPlayerInRoom("a", "Bob"); err != nil || got != "b" {
		t.Fatalf("target=%q err=%v", got, err)
	}
	if _, err := s.SelectPlayerInRoom("a", "Ali"); err == nil {
		t.Fatal("prefix player target accepted")
	}
}
