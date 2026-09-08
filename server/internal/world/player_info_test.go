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

func TestPlayerInfoContinuationRendersSortedSpellsEffectsAndQuestProgress(t *testing.T) {
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Body.Spells[0] |= 1<<0 | 1<<1 // 회복, 삭풍
	p.Body.Spells[6] |= 1 << 7      // 이혼대법
	for _, bit := range []uint{0, 2, 8, 17, 20, 21, 25, 30, 31, 32, 33, 36, 37, 38} {
		p.Body.Flags[bit/8] |= 1 << (bit % 8)
	}
	p.Body.Quests[0] = 0b00000111
	s.Players["a"] = p
	original := s

	text, err := s.PlayerInfoContinuation("a")
	if err != nil {
		t.Fatal(err)
	}
	want := "\n주문: 삭풍, 이혼대법, 회복.\n당신의 현주문: 성현진, 발광, 수호진, 은둔법, 은둔감지, 주문감지, 부양술, 방열진, 비상술, 보마진, 선악감지, 방한진, 수생술, 지방호.\n당신은 현재 임무 3까지 달성하였습니다."
	if text != want {
		t.Fatalf("continuation=%q want=%q", text, want)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("player info continuation mutated state")
	}
}

func TestPlayerInfoContinuationRendersEmptySpellsEffectsAndNoQuestProgress(t *testing.T) {
	s := npcAttackFixture(t)
	text, err := s.PlayerInfoContinuation("a")
	if err != nil {
		t.Fatal(err)
	}
	want := "\n주문: 없음.\n당신의 현주문: 없음.\n당신은 현재 달성한 임무가 없습니다."
	if text != want {
		t.Fatalf("continuation=%q want=%q", text, want)
	}
}

func TestPlayerInfoContinuationStopsAtFirstUncompletedQuest(t *testing.T) {
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Body.Quests[0] = 1 | 1<<2
	s.Players["a"] = p
	text, err := s.PlayerInfoContinuation("a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(text, "당신은 현재 임무 1까지 달성하였습니다.") {
		t.Fatalf("continuation=%q", text)
	}
}

func TestPlayerInfoContinuationRejectsInvalidActor(t *testing.T) {
	s := npcAttackFixture(t)
	if _, err := s.PlayerInfoContinuation("missing"); err == nil {
		t.Fatal("missing actor accepted")
	}
}
