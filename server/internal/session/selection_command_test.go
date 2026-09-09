package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func selectionCommandFixture(t *testing.T) ([]byte, world.MerchantOffers) {
	t.Helper()
	var flags [8]byte
	flags[world.MerchantPurchaseFlag/8] |= 1 << (world.MerchantPurchaseFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "상점방"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"merchant"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"merchant": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: flags}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw, world.MerchantOffers{"merchant": {{Name: "검", Value: 7, Weight: 1}, {Name: "보석", Value: 20, Weight: 1}}}
}

func selectionCommandOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseSelectionLineUsesAliasExactTargetAndPositiveOccurrence(t *testing.T) {
	for _, line := range []string{"선택 상인", `선택 "상인" 2`, `선택 "긴 검"`} {
		command, ok := ParseSelectionLine(line)
		if !ok || command.NPCName == "" || command.Target != command.NPCName || command.Occurrence < 1 {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	command, ok := ParseSelectionLine(`선택 "상인" 2`)
	if !ok || command.NPCName != "상인" || command.Occurrence != 2 {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"선택", "선택 상인 0", "선택 상인 x", "선택 상인 1 2", "selection 상인", "선택\n상인"} {
		if _, ok := ParseSelectionLine(line); ok {
			t.Fatalf("unsupported selection line accepted: %q", line)
		}
	}
}

func TestExecuteSelectionLineIsReadOnlyDurableAndReplayStable(t *testing.T) {
	state, offers := selectionCommandFixture(t)
	store := &departureStore{state: state}
	owners, lease := selectionCommandOwner(t)
	before := append([]byte(nil), store.state...)
	first, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-1", lease, "선택 상인", offers)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.SelectionResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "selection" || result.NPCID != "merchant" || len(result.Items) != 2 || result.Items[0].Price != 10 || !strings.Contains(result.Response, "상인의 물건들:") {
		t.Fatalf("result=%+v", result)
	}
	if string(store.state) != string(before) {
		t.Fatal("selection changed world snapshot")
	}
	replay, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-1", lease, "선택 상인", offers)
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteSelectionLineFailsClosedBeforeReceiptForMissingNPCOrCatalog(t *testing.T) {
	state, offers := selectionCommandFixture(t)
	store := &departureStore{state: state}
	owners, lease := selectionCommandOwner(t)
	if _, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-missing-catalog", lease, "선택 상인"); !errors.Is(err, world.ErrSelectionMerchantOffersUnresolved) || store.commits != 0 {
		t.Fatalf("missing catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-invalid-line", lease, "선택 상인 0", offers); !errors.Is(err, ErrUnsupportedSelectionLine) || store.commits != 0 {
		t.Fatalf("invalid line err=%v commits=%d", err, store.commits)
	}

	var flags [8]byte
	flags[world.MerchantPurchaseFlag/8] |= 1 << (world.MerchantPurchaseFlag % 8)
	missingNPCState := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{200: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200}}, PlayerIDs: []string{"actor"}}},
		Players: map[string]world.PlayerState{"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true}},
		NPCs:    map[string]world.NPCState{"merchant": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: flags}}},
	}
	missingNPCState.NPCs = nil
	raw, err := json.Marshal(missingNPCState)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	if _, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-missing-npc", lease, "선택 상인", offers); !errors.Is(err, world.ErrSelectionNPCStateUnresolved) || store.commits != 0 {
		t.Fatalf("missing NPC err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteSelectionLineRejectsMultipleCatalogsBeforeReceipt(t *testing.T) {
	state, offers := selectionCommandFixture(t)
	store := &departureStore{state: state}
	owners, lease := selectionCommandOwner(t)
	if _, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-multiple", lease, "선택 상인", offers, offers); err == nil || store.commits != 0 {
		t.Fatalf("multiple catalogs err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteSelectionLineWithOptions(context.Background(), store, "w", "selection-options", lease, "선택 상인", SelectionOptions{Offers: offers}); err != nil {
		t.Fatal(err)
	}
}

func TestSelectionCommandUsesIndependentReceiptStoreContract(t *testing.T) {
	state, offers := selectionCommandFixture(t)
	store := &selectionReceiptStore{state: state}
	owners, lease := selectionCommandOwner(t)
	first, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-receipt", lease, "선택 상인", offers)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	replay, err := owners.ExecuteSelectionLine(context.Background(), store, "w", "selection-receipt", lease, "선택 상인", offers)
	if err != nil || !replay.Replayed || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

// selectionReceiptStore intentionally checks request identity. It keeps the
// test independent of departureStore's permissive payload handling while
// still using the same engine.CommandStore contract.
type selectionReceiptStore struct {
	state   []byte
	receipt *storage.WorldReceipt
	request []byte
	commits int
}

func (s *selectionReceiptStore) ReadWorldReceipt(_ context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	if s.receipt == nil || worldID != "w" || commandID != "selection-receipt" || !strings.EqualFold(string(request), string(s.request)) {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	receipt := *s.receipt
	receipt.Replayed = true
	return receipt, nil
}

func (s *selectionReceiptStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	return storage.WorldSnapshot{State: s.state}, nil
}

func (s *selectionReceiptStore) CommitWorldCommand(_ context.Context, worldID, commandID string, request json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.commits++
	s.state = append([]byte(nil), state...)
	s.request = append([]byte(nil), request...)
	s.receipt = &storage.WorldReceipt{Revision: revision + 1, Response: append([]byte(nil), response...)}
	return *s.receipt, nil
}
