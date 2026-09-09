package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type infoContinuationRetryStore struct {
	base     *connectorCommandStore
	failOnce bool
	commands []string
}

func (s *infoContinuationRetryStore) ReadWorldReceipt(ctx context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	return s.base.ReadWorldReceipt(ctx, worldID, commandID, request)
}

func (s *infoContinuationRetryStore) LoadWorld(ctx context.Context, worldID string) (storage.WorldSnapshot, error) {
	return s.base.LoadWorld(ctx, worldID)
}

func (s *infoContinuationRetryStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.commands = append(s.commands, commandID)
	if s.failOnce {
		s.failOnce = false
		return storage.WorldReceipt{}, errors.New("transient continuation commit failure")
	}
	return s.base.CommitWorldCommand(ctx, worldID, commandID, request, revision, state, response)
}

func TestWorldConnectorSubmitDispatchesInfoWithoutMutatingWorld(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", RoomID: 1, Level: 3, Class: 4, Race: 5,
					Stats: [5]byte{11, 12, 13, 14, 15}, HPCurrent: 33, HPMax: 44,
					MPCurrent: 7, MPMax: 8, Experience: 300, Gold: 42,
				},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword": {Object: world.LegacyObject{Name: "검", Weight: 3}},
				}, Inventory: []string{"sword"}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "info-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	text, err := connection.Submit(context.Background(), "정보")
	if err != nil || !strings.Contains(text, "[이름] Alice") || !strings.Contains(text, "[직업] 검사") || !strings.Contains(text, "[엔터]를 누르세요. 그만보시려면 [.]을 치세요: ") {
		t.Fatalf("info text=%q err=%v", text, err)
	}
	saved, commits := store.snapshot()
	if commits != 1 || string(saved) != string(raw) {
		t.Fatalf("info command changed world commits=%d", commits)
	}
}

func TestWorldConnectorInfoDotCancelsPendingContinuationWithoutWorldMutation(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", RoomID: 1, Level: 3, Class: 4, Race: 5,
					Stats: [5]byte{11, 12, 13, 14, 15}, HPCurrent: 33, HPMax: 44,
					MPCurrent: 7, MPMax: 8, Experience: 300, Gold: 42,
				},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "info-cancel-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	first, err := connection.Submit(context.Background(), "정보")
	if err != nil || !strings.Contains(first, "[엔터]를 누르세요. 그만보시려면 [.]을 치세요: ") {
		t.Fatalf("first info=%q err=%v", first, err)
	}
	cancelled, err := connection.Submit(context.Background(), ".")
	if err != nil || cancelled != session.InfoContinuationCancelResponse {
		t.Fatalf("cancelled=%q err=%v", cancelled, err)
	}
	saved, commits := store.snapshot()
	if commits != 1 || string(saved) != string(raw) {
		t.Fatalf("continuation changed durable world commits=%d", commits)
	}
	// The pending continuation is per connection and consumed by the dot;
	// another dot is an ordinary unsupported command, not a second cancel.
	again, err := connection.Submit(context.Background(), ".")
	if err != nil || again != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("second dot=%q err=%v", again, err)
	}
	_, commits = store.snapshot()
	if commits != 1 {
		t.Fatalf("unsupported continuation created receipt commits=%d", commits)
	}
}

func TestWorldConnectorInfoContinuationBindsSnapshotAndCreatesReceipt(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", RoomID: 1, Level: 3, Class: 4, Race: 5,
				},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: &store, WorldID: "info-empty-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	if _, err := connection.Submit(context.Background(), "정보"); err != nil {
		t.Fatal(err)
	}
	updated := initial
	p := updated.Players["a"]
	p.Body.Spells[0] |= 1<<0 | 1<<1 // 회복, 삭풍
	p.Body.Flags[0] |= 1 << 0       // 성현진
	p.Body.Flags[2] |= 1 << 1       // 발광 (flag 17)
	p.Body.Quests[0] = 1 | 1<<1
	updated.Players["a"] = p
	updatedRaw, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.state = updatedRaw
	store.mu.Unlock()

	text, err := connection.Submit(context.Background(), "")
	if err != nil {
		t.Fatalf("empty continuation err=%v", err)
	}
	// The continuation reads the current canonical snapshot once and binds its
	// response to a durable receipt. A later retry can therefore replay this
	// exact page without observing another snapshot.
	want := "\n주문: 삭풍, 회복.\n당신의 현주문: 성현진, 발광.\n당신은 현재 임무 2까지 달성하였습니다."
	if text != want {
		t.Fatalf("empty continuation text=%q want=%q", text, want)
	}
	_, commits := store.snapshot()
	if commits != 2 {
		t.Fatalf("continuation receipt count=%d want=2", commits)
	}
}

func TestWorldConnectorInfoContinuationRetriesWithStableReceiptID(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", RoomID: 1, Level: 3, Class: 4, Race: 5},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &infoContinuationRetryStore{base: base}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "info-retry-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	if _, err := connection.Submit(context.Background(), "정보"); err != nil {
		t.Fatal(err)
	}
	store.failOnce = true
	if output, err := connection.Submit(context.Background(), ""); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !connection.infoPending || connection.infoContinuationCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := connection.infoContinuationCommandID
	output, err := connection.Submit(context.Background(), "")
	if err != nil || !strings.Contains(output, "주문:") {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if connection.infoPending || connection.infoContinuationCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if len(store.commands) != 3 || store.commands[1] != wantID || store.commands[2] != wantID {
		t.Fatalf("continuation command IDs=%v want repeated %q", store.commands, wantID)
	}
	if _, commits := base.snapshot(); commits != 2 {
		t.Fatalf("commits=%d want first info plus one continuation", commits)
	}
}

func TestWorldConnectorInfoContinuationFailsClosedOnFreshSnapshotError(t *testing.T) {
	store := connectorCommandStore{state: []byte(`{"Version":1,"Rooms":{"1":{"Resource":{"ID":1},"Items":{"Items":{},"Inventory":[]},"PlayerIDs":["a"]}},"Players":{"a":{"Body":{"Name":"Alice","RoomID":1,"Level":1,"Class":4,"Race":5},"Online":true,"Items":{"Items":{},"Inventory":[]}}}}`)}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: &store, WorldID: "info-error-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	if _, err := connection.Submit(context.Background(), "정보"); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.state = []byte(`{}`)
	store.mu.Unlock()
	text, err := connection.Submit(context.Background(), "")
	if err == nil || text != "" {
		t.Fatalf("invalid continuation text=%q err=%v", text, err)
	}
	_, commits := store.snapshot()
	if commits != 1 {
		t.Fatalf("invalid continuation created receipt commits=%d", commits)
	}
}
