package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// memoryRegistrationWorld is deliberately test-only. It implements the same
// command/receipt boundaries as the production stores so this regression can
// run without PostgreSQL while still exercising WorldAccounts and
// WorldConnector together.
type memoryRegistrationWorld struct {
	mu         sync.Mutex
	worldID    string
	revision   int64
	state      json.RawMessage
	accounts   map[string]memoryAccount
	receipts   map[string]memoryWorldReceipt
	registers  int
	registrars int
}

type memoryAccount struct {
	id          string
	hash        []byte
	draft       game.Creation
	worldID     string
	worldPlayer string
}

type memoryWorldReceipt struct {
	requestHash [32]byte
	receipt     storage.WorldReceipt
}

func newMemoryRegistrationWorld(t *testing.T, worldID string) *memoryRegistrationWorld {
	t.Helper()
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				Items:    &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	return &memoryRegistrationWorld{
		worldID:  worldID,
		state:    raw,
		accounts: map[string]memoryAccount{},
		receipts: map[string]memoryWorldReceipt{},
	}
}

func (m *memoryRegistrationWorld) Exists(_ context.Context, name string) (bool, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.accounts[canonical]
	return ok, nil
}

func (m *memoryRegistrationWorld) Register(context.Context, string, []byte, game.Creation) (string, error) {
	m.mu.Lock()
	m.registrars++
	m.mu.Unlock()
	return "", errors.New("draft-only registration path used")
}

func (m *memoryRegistrationWorld) Authenticate(_ context.Context, name string, password []byte) (storage.Character, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return storage.Character{}, storage.ErrCredentials
	}
	m.mu.Lock()
	account, ok := m.accounts[canonical]
	m.mu.Unlock()
	if !ok || !identity.CheckPassword(account.hash, password) {
		return storage.Character{}, storage.ErrCredentials
	}
	character := storage.Character{ID: account.id, Draft: account.draft}
	if account.worldID != "" && account.worldPlayer != "" {
		character.ID = account.worldPlayer
		character.Linked = true
		character.WorldID = account.worldID
		character.WorldPlayerID = account.worldPlayer
	}
	return character, nil
}

func (m *memoryRegistrationWorld) LoadWorld(_ context.Context, worldID string) (storage.WorldSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if worldID != m.worldID {
		return storage.WorldSnapshot{}, errors.New("unknown memory world")
	}
	return storage.WorldSnapshot{Revision: m.revision, State: bytes.Clone(m.state)}, nil
}

func (m *memoryRegistrationWorld) CreateInWorld(_ context.Context, worldID, commandID string, expected int64, name string, hash []byte, draft game.Creation) (string, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", err
	}
	player, err := world.NewPlayerFromDraft(canonical, draft)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(hash)
	request, err := json.Marshal(struct {
		Name   string
		Draft  game.Creation
		Digest [32]byte
	}{canonical, draft, digest})
	if err != nil {
		return "", err
	}
	requestHash := sha256.Sum256(request)

	m.mu.Lock()
	defer m.mu.Unlock()
	if worldID != m.worldID {
		return "", errors.New("unknown memory world")
	}
	if saved, ok := m.receipts[commandID]; ok {
		if saved.requestHash != requestHash {
			return "", storage.ErrCommandConflict
		}
		var id string
		if err := json.Unmarshal(saved.receipt.Response, &id); err != nil {
			return "", err
		}
		return id, nil
	}
	if m.revision != expected {
		return "", storage.ErrWorldConflict
	}
	if _, exists := m.accounts[canonical]; exists {
		return "", errors.New("account already exists")
	}
	state, err := world.DecodeState(m.state)
	if err != nil {
		return "", err
	}
	for _, existing := range state.Players {
		if existing.Body.Name == player.Body.Name {
			return "", errors.New("world character already exists")
		}
	}
	characterID := "character-1"
	if _, exists := state.Players[characterID]; exists {
		characterID = "character-2"
	}
	state.Players[characterID] = player
	if err := state.Validate(); err != nil {
		return "", err
	}
	next, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	response, err := json.Marshal(characterID)
	if err != nil {
		return "", err
	}
	m.state = next
	m.revision++
	m.accounts[canonical] = memoryAccount{
		id:          characterID,
		hash:        bytes.Clone(hash),
		draft:       draft,
		worldID:     worldID,
		worldPlayer: characterID,
	}
	m.receipts[commandID] = memoryWorldReceipt{
		requestHash: requestHash,
		receipt: storage.WorldReceipt{
			Revision: m.revision,
			Response: bytes.Clone(response),
		},
	}
	m.registers++
	return characterID, nil
}

func (m *memoryRegistrationWorld) ReadWorldReceipt(_ context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	requestHash := sha256.Sum256(request)
	m.mu.Lock()
	defer m.mu.Unlock()
	if worldID != m.worldID {
		return storage.WorldReceipt{}, errors.New("unknown memory world")
	}
	saved, ok := m.receipts[commandID]
	if !ok {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	if saved.requestHash != requestHash {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	receipt := saved.receipt
	receipt.Response = bytes.Clone(receipt.Response)
	receipt.Replayed = true
	return receipt, nil
}

func (m *memoryRegistrationWorld) CommitWorldCommand(_ context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	requestHash := sha256.Sum256(request)
	m.mu.Lock()
	defer m.mu.Unlock()
	if worldID != m.worldID {
		return storage.WorldReceipt{}, errors.New("unknown memory world")
	}
	if saved, ok := m.receipts[commandID]; ok {
		if saved.requestHash != requestHash {
			return storage.WorldReceipt{}, storage.ErrCommandConflict
		}
		receipt := saved.receipt
		receipt.Response = bytes.Clone(receipt.Response)
		receipt.Replayed = true
		return receipt, nil
	}
	if m.revision != expected {
		return storage.WorldReceipt{}, storage.ErrWorldConflict
	}
	m.state = bytes.Clone(state)
	m.revision++
	receipt := storage.WorldReceipt{Revision: m.revision, Response: bytes.Clone(response)}
	m.receipts[commandID] = memoryWorldReceipt{requestHash: requestHash, receipt: receipt}
	return receipt, nil
}

func (m *memoryRegistrationWorld) snapshot(t *testing.T) (storage.WorldSnapshot, world.State, memoryAccount) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := storage.WorldSnapshot{Revision: m.revision, State: bytes.Clone(m.state)}
	state, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, state, m.accounts["Alice"]
}

func g2ReadOutput(t *testing.T, ctx context.Context, conn *websocket.Conn) Output {
	t.Helper()
	var output Output
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func g2SendLine(t *testing.T, ctx context.Context, conn *websocket.Conn, line string) Output {
	t.Helper()
	if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: line}); err != nil {
		t.Fatal(err)
	}
	return g2ReadOutput(t, ctx, conn)
}

func g2WaitOffline(t *testing.T, ctx context.Context, connector *WorldConnector, store *memoryRegistrationWorld, actorID string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		_, state, _ := store.snapshot(t)
		if player, ok := state.Players[actorID]; ok && !player.Online && len(connector.PendingCleanup()) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatalf("socket departure not durable: pending=%d state=%+v", len(connector.PendingCleanup()), state.Players[actorID])
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestWebSocketMemoryRegistrationWorldFirstCommandAndRelogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store := newMemoryRegistrationWorld(t, "memory-g2")
	accounts := session.NewWorldAccounts(store, store, "memory-g2")
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "memory-g2",
		Clock:       func() (int32, int) { return 100, 12 },
		MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewGameHandler(ctx, accounts, []string{"https://mud.test"}, connector))
	defer server.Close()

	open := func() (*websocket.Conn, Output) {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return conn, g2ReadOutput(t, ctx, conn)
	}

	conn, output := open()
	if output.Type != "view" || output.Closed || output.Secret || !strings.Contains(output.Text, "이름") {
		t.Fatalf("unexpected signup prompt: %+v", output)
	}
	for _, line := range []string{"Alice", "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7"} {
		output = g2SendLine(t, ctx, conn, line)
		if output.Closed || strings.Contains(output.Text, "pw1234") {
			t.Fatalf("signup stopped before password: line=%q output=%+v", line, output)
		}
	}
	if !output.Secret || !strings.Contains(output.Text, "새 암호") {
		t.Fatalf("signup password prompt is not secret: %+v", output)
	}
	output = g2SendLine(t, ctx, conn, "pw1234")
	if output.Closed || output.Secret || !strings.Contains(output.Text, "광장") {
		t.Fatalf("registration did not open world: %+v", output)
	}
	output = g2SendLine(t, ctx, conn, "봐")
	if output.Closed || output.Secret || !strings.Contains(output.Text, "광장") {
		t.Fatalf("first command did not reach world: %+v", output)
	}
	_, state, account := store.snapshot(t)
	if account.id == "" || account.worldPlayer != account.id || store.registrars != 0 || store.registers != 1 || len(state.Players) != 1 {
		t.Fatalf("registration bypassed world identity boundary: account=%+v registers=%d draftPath=%d players=%d", account, store.registers, store.registrars, len(state.Players))
	}
	conn.CloseNow()
	g2WaitOffline(t, ctx, connector, store, account.id)

	conn, output = open()
	defer conn.CloseNow()
	if output.Closed || output.Secret || !strings.Contains(output.Text, "이름") {
		t.Fatalf("relogin initial prompt: %+v", output)
	}
	output = g2SendLine(t, ctx, conn, "ALICE")
	if output.Closed || !output.Secret || !strings.Contains(output.Text, "암호") {
		t.Fatalf("existing character did not select password stage: %+v", output)
	}
	output = g2SendLine(t, ctx, conn, "pw1234")
	if output.Closed || output.Secret || !strings.Contains(output.Text, "광장") {
		t.Fatalf("relogin did not open the same world character: %+v", output)
	}
	output = g2SendLine(t, ctx, conn, "봐")
	if output.Closed || output.Secret || !strings.Contains(output.Text, "광장") {
		t.Fatalf("reconnected first command did not reach world: %+v", output)
	}
	_, state, reloginAccount := store.snapshot(t)
	if reloginAccount.id != account.id || reloginAccount.worldPlayer != account.worldPlayer || len(state.Players) != 1 {
		t.Fatalf("relogin changed identity: first=%+v second=%+v players=%d", account, reloginAccount, len(state.Players))
	}
	conn.CloseNow()
	g2WaitOffline(t, ctx, connector, store, account.id)
	final, state, _ := store.snapshot(t)
	if final.Revision != 7 || state.Players[account.id].Online || len(state.Rooms[1].PlayerIDs) != 0 {
		t.Fatalf("unexpected durable reconnect lifecycle: revision=%d player=%+v room=%+v", final.Revision, state.Players[account.id], state.Rooms[1].PlayerIDs)
	}
}
