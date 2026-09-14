package session

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func legacyLoginRaw(name, password string, room int16) []byte {
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

type memoryLegacyFiles map[string]LegacyPlayerFile

func (f memoryLegacyFiles) Lookup(_ context.Context, name string) (LegacyPlayerFile, bool, error) {
	rec, ok := f[name]
	return rec, ok, nil
}

type memoryLegacyWorld struct {
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

func newMemoryLegacyWorld() *memoryLegacyWorld {
	return &memoryLegacyWorld{
		worldID: "legacy-login-world",
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

func (m *memoryLegacyWorld) Exists(_ context.Context, name string) (bool, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.characters[canonical]
	return ok, nil
}

func (m *memoryLegacyWorld) Register(context.Context, string, []byte, game.Creation) (string, error) {
	m.mu.Lock()
	m.registrars++
	m.mu.Unlock()
	return "", errors.New("draft-only registration path used")
}

func (m *memoryLegacyWorld) Authenticate(_ context.Context, name string, password []byte) (storage.Character, error) {
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

func (m *memoryLegacyWorld) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := json.Marshal(m.state)
	if err != nil {
		return storage.WorldSnapshot{}, err
	}
	return storage.WorldSnapshot{Revision: m.revision, State: raw}, nil
}

func (m *memoryLegacyWorld) ImportPlayerSnapshot(_ context.Context, input storage.PlayerSnapshotImport) (storage.PlayerSnapshotImportResult, error) {
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
	hash := append([]byte(nil), input.CredentialHash...)
	m.state = next
	m.revision++
	m.imports++
	m.hashes[canonical] = hash
	m.characters[canonical] = storage.Character{
		ID: input.PlayerID, Name: canonical, Linked: true, WorldID: m.worldID, WorldPlayerID: input.PlayerID,
	}
	m.receipts[input.CommandID] = input.PlayerID
	return storage.PlayerSnapshotImportResult{PlayerID: input.PlayerID}, nil
}

func (m *memoryLegacyWorld) CreateInWorld(_ context.Context, worldID, commandID string, expected int64, name string, hash []byte, draft game.Creation) (string, error) {
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

func legacyLoginAccounts(t *testing.T, password string) (*memoryLegacyWorld, Accounts) {
	t.Helper()
	raw := legacyLoginRaw("Alice", password, 1)
	if _, err := world.DecodeLegacyPlayerSnapshotRawV1(raw); err != nil {
		t.Fatal(err)
	}
	store := newMemoryLegacyWorld()
	files := memoryLegacyFiles{
		"Alice": {PlayerID: "legacy-alice", CommandID: "import-alice", Raw: raw},
	}
	return store, NewLiveAccounts(store, store, files, store, store.worldID)
}

func TestLiveAccountsWrapsWorldAccounts(t *testing.T) {
	store := newMemoryLegacyWorld()
	accounts := NewLiveAccounts(store, store, nil, store, store.worldID)
	legacy, ok := accounts.(*LegacyImportAccounts)
	if !ok {
		t.Fatal("live accounts must be LegacyImportAccounts")
	}
	if _, ok := legacy.Accounts.(*WorldAccounts); !ok {
		t.Fatal("LegacyImportAccounts must wrap WorldAccounts")
	}
}

func TestLiveAccountsSignupUsesWorldRegistration(t *testing.T) {
	store, accounts := legacyLoginAccounts(t, "pw1234")
	login := NewLogin(accounts)
	ctx := context.Background()
	for _, line := range []string{"Bob", "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7"} {
		view := login.Submit(ctx, line)
		if view.Closed || view.Verified != nil {
			t.Fatalf("premature completion: %+v", view)
		}
	}
	if !login.View().Secret {
		t.Fatal("password echo enabled")
	}
	view := login.Submit(ctx, "pw1234")
	store.mu.Lock()
	registers, registrars, imports := store.registers, store.registrars, store.imports
	store.mu.Unlock()
	if view.Closed || view.Verified == nil || view.Verified.ID != "character-Bob" {
		t.Fatalf("world signup failed: %+v", view.Verified)
	}
	if strings.Contains(view.Text, "pw1234") || registers != 1 || registrars != 0 || imports != 0 {
		t.Fatalf("signup bypassed WorldAccounts: registers=%d draft=%d imports=%d", registers, registrars, imports)
	}
	login.Close()
	relogin := NewLogin(accounts)
	relogin.Submit(ctx, "Bob")
	view = relogin.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != "character-Bob" {
		t.Fatal("bcrypt relogin after live signup failed")
	}
}

func TestLegacyPlayerFileRequiresPasswordThroughLogin(t *testing.T) {
	store, accounts := legacyLoginAccounts(t, "pw1234")
	s := NewLogin(accounts)
	view := s.Submit(context.Background(), "Alice")
	if view.Verified != nil || view.Closed || !view.Secret || !strings.Contains(view.Text, "암호") {
		t.Fatal("name-only ownership")
	}
	store.mu.Lock()
	imports := store.imports
	store.mu.Unlock()
	if imports != 0 {
		t.Fatal("name-only admitted the C file")
	}
}

func TestLegacyPlayerFilePasswordLoginDecodesAndAdmits(t *testing.T) {
	store, accounts := legacyLoginAccounts(t, "pw1234")
	s := NewLogin(accounts)
	ctx := context.Background()
	s.Submit(ctx, "Alice")
	view := s.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != "legacy-alice" || view.Verified.WorldPlayerID != "legacy-alice" || !view.Verified.Linked || view.Secret || view.Closed {
		t.Fatalf("legacy login failed: %+v", view.Verified)
	}
	if strings.Contains(view.Text, "pw1234") {
		t.Fatal("password echoed")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	player, ok := store.state.Players["legacy-alice"]
	if store.imports != 1 || !ok || player.Online || player.Body.Name != "Alice" {
		t.Fatalf("AdmitPlayerSnapshot was not the login path: imports=%d player=%+v ok=%v", store.imports, player, ok)
	}
}

func TestLegacyPlayerFileWrongPasswordThenRetry(t *testing.T) {
	store, accounts := legacyLoginAccounts(t, "pw1234")
	s := NewLogin(accounts)
	ctx := context.Background()
	s.Submit(ctx, "Alice")
	view := s.Submit(ctx, "wrong")
	if view.Verified != nil || view.Closed || !view.Secret {
		t.Fatal("first failure closed")
	}
	store.mu.Lock()
	if store.imports != 0 {
		t.Fatal("wrong password imported")
	}
	store.mu.Unlock()
	view = s.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != "legacy-alice" {
		t.Fatal("retry login failed")
	}
}

func TestLegacyPlayerFileDisconnectThenReloginUsesImportedHash(t *testing.T) {
	store, accounts := legacyLoginAccounts(t, "pw1234")
	ctx := context.Background()
	first := NewLogin(accounts)
	first.Submit(ctx, "Alice")
	if view := first.Submit(ctx, "pw1234"); view.Verified == nil {
		t.Fatal("import login failed")
	}
	first.Close()
	second := NewLogin(accounts)
	second.Submit(ctx, "Alice")
	view := second.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != "legacy-alice" {
		t.Fatal("relogin after import failed")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.imports != 1 {
		t.Fatalf("relogin re-imported C file: %d", store.imports)
	}
}

func TestLegacyPlayerFileEmptyPasswordCloses(t *testing.T) {
	_, accounts := legacyLoginAccounts(t, "pw1234")
	s := NewLogin(accounts)
	ctx := context.Background()
	s.Submit(ctx, "Alice")
	view := s.Submit(ctx, "")
	if !view.Closed || view.Verified != nil {
		t.Fatal("empty password admitted")
	}
}

func TestLegacyPlayerFileUnknownNameIsNotClaimed(t *testing.T) {
	_, accounts := legacyLoginAccounts(t, "pw1234")
	s := NewLogin(accounts)
	view := s.Submit(context.Background(), "Bob")
	if view.Verified != nil || view.Secret || !strings.Contains(view.Text, "새 캐릭터") {
		t.Fatal("unknown name treated as existing C file")
	}
}
