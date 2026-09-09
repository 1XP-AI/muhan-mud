package world

import (
	"reflect"
	"strings"
	"testing"
)

func titleTestState(title string) State {
	return State{Version: 1, Rooms: map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor"}}}, Players: map[string]PlayerState{"actor": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true, Title: title}}}
}

func TestTitleSetClearAndViewAreCanonicalAndAtomic(t *testing.T) {
	s := titleTestState("")
	view, err := s.ViewTitle("actor")
	if err != nil || view.Action != TitleView || view.Title != "" || !strings.Contains(view.Response, "설정된 칭호") {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	proposal, err := s.PlanSetTitle("actor", "무명의 수호자")
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyTitle(proposal)
	if err != nil || !result.Changed || next.Players["actor"].Title != "무명의 수호자" {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["actor"], result, err)
	}
	if s.Players["actor"].Title != "" {
		t.Fatal("plan mutated source")
	}
	clear, err := next.PlanClearTitle("actor")
	if err != nil {
		t.Fatal(err)
	}
	cleared, clearResult, err := next.ApplyTitle(clear)
	if err != nil || !clearResult.Changed || cleared.Players["actor"].Title != "" {
		t.Fatalf("cleared=%+v result=%+v err=%v", cleared.Players["actor"], clearResult, err)
	}
	if _, _, err := s.ApplyTitle(proposal); err != nil {
		t.Fatal("original proposal should still apply to original snapshot")
	}
	stale := titleTestState("")
	sp, err := stale.PlanSetTitle("actor", "one")
	if err != nil {
		t.Fatal(err)
	}
	changed := stale.Players["actor"]
	changed.Body.Level = 2
	stale.Players["actor"] = changed
	if _, _, err := stale.ApplyTitle(sp); err == nil {
		t.Fatal("stale proposal accepted")
	}
}

func TestValidatePlayerTitleBoundaries(t *testing.T) {
	if err := ValidatePlayerTitle(strings.Repeat("가", 26)); err != nil { // 78 UTF-8 bytes
		t.Fatal(err)
	}
	for _, title := range []string{"", strings.Repeat("가", 27), " leading", "trailing ", "line\nfeed"} {
		if err := ValidatePlayerTitle(title); err == nil {
			t.Fatalf("accepted invalid title %q", title)
		}
	}
	s := titleTestState("old")
	p, err := s.PlanSetTitle("actor", "new")
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(p, TitleProposal{}) {
		t.Fatal("empty proposal")
	}
}
