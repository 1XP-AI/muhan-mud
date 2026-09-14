package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func zapCommandFixture(t *testing.T) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
				PlayerIDs: []string{"actor", "observer"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Level: 10, HPMax: 30, HPCurrent: 10},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"wand-1": {Object: world.LegacyObject{Name: "회복봉", Type: world.ZapWandType, ShotsCurrent: 2, ShotsMax: 2, MagicPower: 1}},
						"wand-2": {Object: world.LegacyObject{Name: "회복봉", Type: world.ZapWandType, ShotsCurrent: 1, ShotsMax: 1, MagicPower: 1}},
					},
					Inventory: []string{"wand-1", "wand-2"},
				},
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Level: 8, HPMax: 20, HPCurrent: 5},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}, Inventory: []string{}},
			},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 16, HPCurrent: 8}},
		},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseZapLineAdmitsSuffixForms(t *testing.T) {
	bare, ok := ParseZapLine("  zap  ")
	if !ok || bare.ItemName != "" || bare.Occurrence != 1 || !IsZapLine("zap") {
		t.Fatalf("bare=%+v ok=%t", bare, ok)
	}
	named, ok := ParseZapLine("회복봉 zap")
	if !ok || named.ItemName != "회복봉" || named.Occurrence != 1 || named.TargetName != "" {
		t.Fatalf("named=%+v ok=%t", named, ok)
	}
	occ, ok := ParseZapLine(`"회복봉" 2 zap`)
	if !ok || occ.ItemName != "회복봉" || occ.Occurrence != 2 {
		t.Fatalf("occurrence=%+v ok=%t", occ, ok)
	}
	target, ok := ParseZapLine("회복봉 늑대 zap")
	if !ok || target.ItemName != "회복봉" || target.TargetName != "늑대" || target.TargetOccurrence != 1 {
		t.Fatalf("target=%+v ok=%t", target, ok)
	}
	itemOccTarget, ok := ParseZapLine("회복봉 2 늑대 zap")
	if !ok || itemOccTarget.ItemName != "회복봉" || itemOccTarget.Occurrence != 2 || itemOccTarget.TargetName != "늑대" {
		t.Fatalf("item occ target=%+v ok=%t", itemOccTarget, ok)
	}
	targetOcc, ok := ParseZapLine("회복봉 늑대 2 zap")
	if !ok || targetOcc.TargetName != "늑대" || targetOcc.TargetOccurrence != 2 || targetOcc.Occurrence != 1 {
		t.Fatalf("target occ=%+v ok=%t", targetOcc, ok)
	}
	both, ok := ParseZapLine("회복봉 2 늑대 2 zap")
	if !ok || both.Occurrence != 2 || both.TargetOccurrence != 2 {
		t.Fatalf("both=%+v ok=%t", both, ok)
	}
	for _, line := range []string{
		"zap 회복봉", "회복봉 0 zap", "회복봉 -1 zap", "zap\n", string([]byte{0xff}), "지팡이",
	} {
		if _, ok := ParseZapLine(line); ok || IsZapLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesZap(t *testing.T) {
	for _, line := range []string{"zap", "회복봉 zap", "회복봉 늑대 zap"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandZap {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	parsed, err := ParseCommand("zap 회복봉")
	if err != nil || parsed.Kind != CommandUnknown {
		t.Fatalf("prefix=%+v err=%v", parsed, err)
	}
}

func TestExecuteZapLineVigorAndReplaysWithoutRandom(t *testing.T) {
	store := &departureStore{state: zapCommandFixture(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteZapLineWithOptions(context.Background(), store, "w", "zap-1", lease, "회복봉 2 zap", ZapOptions{Now: 10, Roll: func(low, high int) int {
		calls++
		if calls == 1 {
			if low != 1 || high != 100 {
				t.Fatalf("fail range=%d..%d", low, high)
			}
			return 1
		}
		if low != 1 || high != 6 {
			t.Fatalf("heal range=%d..%d", low, high)
		}
		return 5
	}})
	if err != nil || first.Replayed || store.commits != 1 || calls != 2 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.ZapResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ZapVigorSelf || result.ItemID != "wand-2" || !result.Changed || result.HPDelta != 5 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.HPCurrent != 15 || saved.Players["actor"].Items.Items["wand-2"].Object.ShotsCurrent != 0 {
		t.Fatalf("saved=%+v", saved.Players["actor"])
	}
	replay, err := owners.ExecuteZapLineWithOptions(context.Background(), store, "w", "zap-1", lease, "회복봉 2 zap", ZapOptions{Now: 10, Roll: func(int, int) int {
		t.Fatal("replay consumed random")
		return 1
	}})
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if _, err := owners.ExecuteZapLineWithOptions(context.Background(), store, "w", "zap-bad", lease, "zap 회복봉", ZapOptions{Now: 10}); !errors.Is(err, ErrUnsupportedZapLine) {
		t.Fatalf("unsupported err=%v", err)
	}
}

func TestExecuteZapLineUsageDoesNotMutate(t *testing.T) {
	store := &departureStore{state: zapCommandFixture(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteZapLine(context.Background(), store, "w", "zap-usage", lease, "zap", 10, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("usage=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ZapResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != world.ZapUsage || result.Response != world.ZapUsageResponse {
		t.Fatalf("result=%+v", result)
	}
}
