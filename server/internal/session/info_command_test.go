package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestExecuteInfoLinePersistsCanonicalProjectionAndReplays(t *testing.T) {
	state, err := decodeInfoFixture()
	if err != nil {
		t.Fatal(err)
	}
	p := state.Players["a"]
	p.Body.Level = 3
	p.Body.Race = 5
	p.Body.Class = 4
	p.Body.Alignment = 101
	p.Body.Stats = [5]byte{11, 12, 13, 14, 15}
	p.Body.HPCurrent, p.Body.HPMax = 33, 44
	p.Body.MPCurrent, p.Body.MPMax = 7, 8
	p.Body.Armor = 65
	p.Body.Experience = 300
	p.Body.Gold = 42
	p.Body.Timers[28].Interval = 90061
	p.Body.Proficiency = [5]int32{0, 1024, 1440, 1910, 16000}
	p.Body.Realm = [4]int32{0, 1024, 40000, 80000}
	p.Items = &world.ItemCollection{Items: map[string]world.Item{
		"sword": {Object: world.LegacyObject{Name: "검", Weight: 3}},
	}, Inventory: []string{"sword"}}
	state.Players["a"] = p
	raw, err := json.Marshal(state)
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

	first, err := owners.ExecuteInfoLine(context.Background(), store, "w", "info-1", lease, "정보")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[이름] Alice",
		"[레벨] 3",
		"[종족] 인간족",
		"[직업] 검사",
		"[힘] 11",
		"[민첩] 12",
		"[맷집] 13",
		"[지식] 14",
		"[신앙심] 15",
		"[체력] 33",
		"/44",
		"[도력] 7",
		"/8",
		"[경험치] 300 ( 84의 경험치가 필요합니다. )",
		"[방어력] 35",
		"[소지품 무게] 3 근 (총 1개).",
		"[ 도 ]  0%",
		"[ 검 ] 20%",
		"[ 땅 ]  0%",
		"[바람] 10%",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("info response missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "엔터") || strings.Contains(text, "주문:") {
		t.Fatalf("info response crossed unsupported continuation boundary: %q", text)
	}
	if string(store.state) != string(raw) {
		t.Fatal("info command mutated world state")
	}

	replay, err := owners.ExecuteInfoLine(context.Background(), store, "w", "info-1", lease, "정보")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteInfoLineRejectsContinuationAndArguments(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"정보 주문", "정보 extra", ".", "도움말"} {
		if _, err := owners.ExecuteInfoLine(context.Background(), store, "w", "info-bad-"+line, lease, line); err == nil {
			t.Fatalf("unsupported info input accepted: %q", line)
		}
		if store.commits != 0 {
			t.Fatalf("unsupported info input created receipt: %q", line)
		}
	}
}

func decodeInfoFixture() (world.State, error) {
	return world.DecodeState(attackCommandFixture())
}
