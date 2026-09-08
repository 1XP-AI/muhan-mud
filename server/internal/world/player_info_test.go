package world

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlayerInfoIsPureAndFailsClosedForUnmigratedFields(t *testing.T) {
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Body.Level = 2
	p.Body.Race = 5
	p.Body.Experience = 100
	p.Body.Timers[28].Interval = 60
	p.Body.Proficiency = [5]int32{0, 1024, 1440, 1910, 16000}
	p.Body.Realm = [4]int32{0, 1024, 40000, 80000}
	s.Players["a"] = p
	original := s
	text, err := s.PlayerInfo("a")
	if err != nil || !strings.Contains(text, "[이름] Alice") || !strings.Contains(text, "[ 검 ] 20%") {
		t.Fatalf("info=%q err=%v", text, err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("player info mutated state")
	}

	p.Items = nil
	s.Players["a"] = p
	if _, err := s.PlayerInfo("a"); err == nil {
		t.Fatal("unmigrated item state accepted")
	}
	p.Items = original.Players["a"].Items
	p.Body.Class = 0
	s.Players["a"] = p
	if _, err := s.PlayerInfo("a"); err == nil {
		t.Fatal("undefined legacy class accepted")
	}
}
