package main

import (
	"context"
	"encoding/binary"
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
	"github.com/1XP-Inc/muhan-mud/server/internal/transport"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func livePlayerRaw(name, password string, room int16) []byte {
	raw := make([]byte, 1952+4)
	copy(raw[0:], name)
	copy(raw[240:], password)
	raw[332] = 20
	raw[334] = 20
	raw[336] = 10
	raw[338] = 10
	binary.LittleEndian.PutUint16(raw[502:504], uint16(room))
	return raw
}

type liveMemoryFiles map[string]session.LegacyPlayerFile

func (f liveMemoryFiles) Lookup(_ context.Context, name string) (session.LegacyPlayerFile, bool, error) {
	rec, ok := f[name]
	return rec, ok, nil
}

type liveMemoryWorld struct {
	mu         sync.Mutex
	worldID    string
	revision   int64
	state      world.State
	hashes     map[string][]byte
	characters map[string]storage.Character
	receipts   map[string]string
	imports    int
	registers  int
	registrars int
}

func newLiveMemoryWorld() *liveMemoryWorld {
	return &liveMemoryWorld{
		worldID: "live-game-world",
		state: world.State{
			Version: 1,
			Rooms: map[int16]world.RoomState{
				1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}},
			},
			Players: map[string]world.PlayerState{},
		},
		hashes:     map[string][]byte{},
		characters: map[string]storage.Character{},
		receipts:   map[string]string{},
	}
}

func (m *liveMemoryWorld) Exists(_ context.Context, name string) (bool, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.characters[canonical]
	return ok, nil
}

func (m *liveMemoryWorld) Register(context.Context, string, []byte, game.Creation) (string, error) {
	m.mu.Lock()
	m.registrars++
	m.mu.Unlock()
	return "", errors.New("draft-only registration path used")
}

func (m *liveMemoryWorld) Authenticate(_ context.Context, name string, password []byte) (storage.Character, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return storage.Character{}, storage.ErrCredentials
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	hash, ok := m.hashes[canonical]
	if !ok || !identity.CheckPassword(hash, password) {
		return storage.Character{}, storage.ErrCredentials
	}
	return m.characters[canonical], nil
}

func (m *liveMemoryWorld) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := json.Marshal(m.state)
	if err != nil {
		return storage.WorldSnapshot{}, err
	}
	return storage.WorldSnapshot{Revision: m.revision, State: raw}, nil
}

func (m *liveMemoryWorld) ImportPlayerSnapshot(_ context.Context, input storage.PlayerSnapshotImport) (storage.PlayerSnapshotImportResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if playerID, ok := m.receipts[input.CommandID]; ok {
		if playerID != input.PlayerID {
			return storage.PlayerSnapshotImportResult{}, storage.ErrCommandConflict
		}
		return storage.PlayerSnapshotImportResult{PlayerID: playerID, Replayed: true}, nil
	}
	if input.ExpectedRevision != m.revision {
		return storage.PlayerSnapshotImportResult{}, storage.ErrWorldConflict
	}
	inspection, err := world.InspectPlayerSnapshotV1(input.Snapshot)
	if err != nil {
		return storage.PlayerSnapshotImportResult{}, err
	}
	itemIndex := 0
	allocate := func() (string, error) {
		if itemIndex >= len(input.ItemIDs) {
			return "", storage.ErrPlayerSnapshotImport
		}
		id := input.ItemIDs[itemIndex]
		itemIndex++
		return id, nil
	}
	next, err := m.state.AdmitPlayerSnapshot(input.PlayerID, inspection.Snapshot, allocate)
	if err != nil {
		return storage.PlayerSnapshotImportResult{}, err
	}
	canonical, err := identity.CanonicalName(input.AccountName)
	if err != nil {
		return storage.PlayerSnapshotImportResult{}, err
	}
	if _, exists := m.characters[canonical]; exists {
		return storage.PlayerSnapshotImportResult{}, storage.ErrPlayerSnapshotImportConflict
	}
	m.state = next
	m.revision++
	m.imports++
	m.hashes[canonical] = append([]byte(nil), input.CredentialHash...)
	m.characters[canonical] = storage.Character{
		ID: input.PlayerID, Name: canonical, Linked: true, WorldID: m.worldID, WorldPlayerID: input.PlayerID,
	}
	m.receipts[input.CommandID] = input.PlayerID
	return storage.PlayerSnapshotImportResult{PlayerID: input.PlayerID}, nil
}

func (m *liveMemoryWorld) CreateInWorld(_ context.Context, worldID, commandID string, expected int64, name string, hash []byte, draft game.Creation) (string, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", err
	}
	player, err := world.NewPlayerFromDraft(canonical, draft)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if worldID != m.worldID {
		return "", errors.New("unknown world")
	}
	if playerID, ok := m.receipts[commandID]; ok {
		return playerID, nil
	}
	if expected != m.revision {
		return "", storage.ErrWorldConflict
	}
	if _, exists := m.characters[canonical]; exists {
		return "", storage.ErrPlayerSnapshotImportConflict
	}
	playerID := "character-" + canonical
	if _, exists := m.state.Players[playerID]; exists {
		return "", storage.ErrPlayerSnapshotImportConflict
	}
	next := m.state
	players := make(map[string]world.PlayerState, len(m.state.Players)+1)
	for id, existing := range m.state.Players {
		if existing.Body.Name == player.Body.Name {
			return "", storage.ErrPlayerSnapshotImportConflict
		}
		players[id] = existing
	}
	players[playerID] = player
	next.Players = players
	if err := next.Validate(); err != nil {
		return "", err
	}
	m.state = next
	m.revision++
	m.registers++
	m.hashes[canonical] = append([]byte(nil), hash...)
	m.characters[canonical] = storage.Character{
		ID: playerID, Name: canonical, Linked: true, WorldID: m.worldID, WorldPlayerID: playerID,
	}
	m.receipts[commandID] = playerID
	return playerID, nil
}

type liveGameProbe struct {
	mu     sync.Mutex
	opened []storage.Character
}

func (p *liveGameProbe) Open(_ context.Context, c storage.Character) (transport.GameConnection, string, error) {
	p.mu.Lock()
	p.opened = append(p.opened, c)
	p.mu.Unlock()
	return p, "광장", nil
}

func (p *liveGameProbe) Submit(_ context.Context, line string) (string, error) {
	return "명령: " + line, nil
}

func (p *liveGameProbe) Close(context.Context) {}

func (p *liveGameProbe) openedIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, len(p.opened))
	for i, c := range p.opened {
		ids[i] = c.ID
	}
	return ids
}

func liveGameAccounts(t *testing.T) (*liveMemoryWorld, session.Accounts) {
	t.Helper()
	raw := livePlayerRaw("Alice", "pw1234", 1)
	if _, err := world.DecodeLegacyPlayerSnapshotRawV1(raw); err != nil {
		t.Fatal(err)
	}
	store := newLiveMemoryWorld()
	files := liveMemoryFiles{
		"Alice": {PlayerID: "legacy-alice", CommandID: "import-alice", Raw: raw},
	}
	accounts := newGameAccounts(store, store, files, store, store.worldID)
	legacy, ok := accounts.(*session.LegacyImportAccounts)
	if !ok {
		t.Fatal("cmd/muhan construction must return LegacyImportAccounts")
	}
	if _, ok := legacy.Accounts.(*session.WorldAccounts); !ok {
		t.Fatal("cmd/muhan construction must wrap WorldAccounts")
	}
	return store, accounts
}

func dialLiveGame(t *testing.T, ctx context.Context, server *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func readLiveView(t *testing.T, ctx context.Context, conn *websocket.Conn) transport.Output {
	t.Helper()
	var output transport.Output
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func sendLiveLine(t *testing.T, ctx context.Context, conn *websocket.Conn, line string) transport.Output {
	t.Helper()
	if err := wsjson.Write(ctx, conn, transport.Input{Type: "line", Text: line}); err != nil {
		t.Fatal(err)
	}
	return readLiveView(t, ctx, conn)
}

func TestNewGameAccountsWrapsLegacyImportAroundWorldAccounts(t *testing.T) {
	liveGameAccounts(t)
}

func TestNewGameHandlerLegacyPlayerFileRequiresPassword(t *testing.T) {
	store, accounts := liveGameAccounts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	game := &liveGameProbe{}
	server := httptest.NewServer(transport.NewGameHandler(ctx, accounts, []string{"https://mud.test"}, game))
	defer server.Close()
	conn := dialLiveGame(t, ctx, server)
	defer conn.CloseNow()
	view := readLiveView(t, ctx, conn)
	if view.Secret || view.Closed || !strings.Contains(view.Text, "이름") {
		t.Fatalf("missing name prompt: %+v", view)
	}
	view = sendLiveLine(t, ctx, conn, "Alice")
	if view.Closed || !view.Secret || !strings.Contains(view.Text, "암호") {
		t.Fatalf("name-only ownership: %+v", view)
	}
	if strings.Contains(view.Text, "pw1234") {
		t.Fatal("password echoed")
	}
	store.mu.Lock()
	imports := store.imports
	store.mu.Unlock()
	if imports != 0 || len(game.openedIDs()) != 0 {
		t.Fatal("name-only admitted the C file")
	}
}

func TestNewGameHandlerLegacyPlayerFilePasswordLogin(t *testing.T) {
	store, accounts := liveGameAccounts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	game := &liveGameProbe{}
	server := httptest.NewServer(transport.NewGameHandler(ctx, accounts, []string{"https://mud.test"}, game))
	defer server.Close()
	conn := dialLiveGame(t, ctx, server)
	defer conn.CloseNow()
	_ = readLiveView(t, ctx, conn)
	view := sendLiveLine(t, ctx, conn, "Alice")
	if !view.Secret {
		t.Fatal("password prompt not secret")
	}
	view = sendLiveLine(t, ctx, conn, "pw1234")
	if view.Closed || view.Secret || view.Text != "광장" || strings.Contains(view.Text, "pw1234") {
		t.Fatalf("legacy login did not open world: %+v", view)
	}
	if ids := game.openedIDs(); len(ids) != 1 || ids[0] != "legacy-alice" {
		t.Fatalf("GameHandler opened unverified actor: %v", ids)
	}
	store.mu.Lock()
	player, ok := store.state.Players["legacy-alice"]
	imports := store.imports
	store.mu.Unlock()
	if imports != 1 || !ok || player.Online || player.Body.Name != "Alice" {
		t.Fatalf("AdmitPlayerSnapshot was not the live login path: imports=%d player=%+v ok=%v", imports, player, ok)
	}
}

func TestNewGameHandlerSignupStillUsesWorldAccounts(t *testing.T) {
	store, accounts := liveGameAccounts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	game := &liveGameProbe{}
	server := httptest.NewServer(transport.NewGameHandler(ctx, accounts, []string{"https://mud.test"}, game))
	defer server.Close()
	conn := dialLiveGame(t, ctx, server)
	defer conn.CloseNow()
	_ = readLiveView(t, ctx, conn)
	for _, line := range []string{"Bob", "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7"} {
		view := sendLiveLine(t, ctx, conn, line)
		if view.Closed {
			t.Fatalf("signup closed early: line=%q view=%+v", line, view)
		}
	}
	view := sendLiveLine(t, ctx, conn, "pw1234")
	if view.Closed || view.Secret || view.Text != "광장" || strings.Contains(view.Text, "pw1234") {
		t.Fatalf("signup did not open world: %+v", view)
	}
	if ids := game.openedIDs(); len(ids) != 1 || ids[0] != "character-Bob" {
		t.Fatalf("signup opened wrong actor: %v", ids)
	}
	store.mu.Lock()
	registers, registrars, imports := store.registers, store.registrars, store.imports
	store.mu.Unlock()
	if registers != 1 || registrars != 0 || imports != 0 {
		t.Fatalf("signup bypassed WorldAccounts: registers=%d draft=%d imports=%d", registers, registrars, imports)
	}
}
