package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type memoryPasswordStore struct {
	mu       sync.Mutex
	name     string
	account  string
	hash     []byte
	lookups  int
	updates  int
	failOnce bool
	old      [][]byte
	new      [][]byte
}

func newMemoryPasswordStore(t *testing.T) *memoryPasswordStore {
	t.Helper()
	hash, err := identity.HashPassword([]byte("old-password"))
	if err != nil {
		t.Fatal(err)
	}
	return &memoryPasswordStore{name: "Alice", account: "account-1", hash: hash}
}

func (s *memoryPasswordStore) LookupCredential(_ context.Context, name string) (string, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookups++
	if name != s.name {
		return "", nil, storage.ErrCredentials
	}
	return s.account, bytes.Clone(s.hash), nil
}

func (s *memoryPasswordStore) UpdateCredential(_ context.Context, accountID string, expected, replacement []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates++
	s.old = append(s.old, bytes.Clone(expected))
	s.new = append(s.new, bytes.Clone(replacement))
	if accountID != s.account {
		return errors.New("unexpected account")
	}
	if s.failOnce {
		s.failOnce = false
		return errors.New("transient save failure")
	}
	if !bytes.Equal(s.hash, expected) && !bytes.Equal(s.hash, replacement) {
		return errors.New("stale credential")
	}
	s.hash = bytes.Clone(replacement)
	return nil
}

func passwordConnector(t *testing.T, passwordStore session.PasswordChangeStore) (*WorldConnector, *worldConnection, *connectorCommandStore) {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"player-1"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"player-1": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Level: 1, HPCurrent: 30},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldStore := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:         worldStore,
		WorldID:       "password-world",
		Clock:         func() (int32, int) { return 100, 12 },
		MaxSessions:   1,
		PasswordStore: passwordStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("player-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return connector, &worldConnection{game: connector, lease: lease, ready: true, accountName: "Alice"}, worldStore
}

func TestWorldConnectorPasswordChangeIsConnectionLocalAndSecret(t *testing.T) {
	passwordStore := newMemoryPasswordStore(t)
	connector, connection, worldStore := passwordConnector(t, passwordStore)
	before, _ := worldStore.snapshot()

	output, err := connection.Submit(context.Background(), "암호")
	if err != nil || !strings.Contains(output, "현재 암호") || !connection.InputIsSecret() {
		t.Fatalf("start output=%q err=%v secret=%v", output, err, connection.InputIsSecret())
	}
	if worldStore.commits != 0 {
		t.Fatalf("password start created world receipt: commits=%d", worldStore.commits)
	}

	if output, err = connection.Submit(context.Background(), "old-password"); err != nil || !strings.Contains(output, "새 암호") || !connection.InputIsSecret() {
		t.Fatalf("current output=%q err=%v secret=%v", output, err, connection.InputIsSecret())
	}
	if output, err = connection.Submit(context.Background(), "new-password"); err != nil || !strings.Contains(output, "다시") || !connection.InputIsSecret() {
		t.Fatalf("new output=%q err=%v secret=%v", output, err, connection.InputIsSecret())
	}

	passwordStore.failOnce = true
	if output, err = connection.Submit(context.Background(), "new-password"); err != nil || !strings.Contains(output, "확인할 수 없습니다") || !connection.InputIsSecret() {
		t.Fatalf("transient output=%q err=%v secret=%v", output, err, connection.InputIsSecret())
	}
	if output, err = connection.Submit(context.Background(), "new-password"); err != nil || !strings.Contains(output, "변경되었습니다") || connection.InputIsSecret() {
		t.Fatalf("retry output=%q err=%v secret=%v", output, err, connection.InputIsSecret())
	}

	if passwordStore.lookups != 1 || passwordStore.updates != 2 || len(passwordStore.new) != 2 || !bytes.Equal(passwordStore.new[0], passwordStore.new[1]) {
		t.Fatalf("unexpected credential operations: lookups=%d updates=%d hashes=%d", passwordStore.lookups, passwordStore.updates, len(passwordStore.new))
	}
	if string(passwordStore.new[0]) == "new-password" || strings.Contains(output, "new-password") || strings.Contains(output, "old-password") {
		t.Fatal("password crossed the output or persistence boundary")
	}
	after, _ := worldStore.snapshot()
	if worldStore.commits != 0 || !bytes.Equal(before, after) {
		t.Fatalf("password flow mutated world state: commits=%d", worldStore.commits)
	}
	_ = connector
}

func TestWorldConnectorPasswordChangeDoesNotEnterHistoryOrSurviveClose(t *testing.T) {
	passwordStore := newMemoryPasswordStore(t)
	_, connection, worldStore := passwordConnector(t, passwordStore)
	if _, err := connection.Submit(context.Background(), "시간"); err != nil {
		t.Fatal(err)
	}
	previous := connection.lastCommand
	if _, err := connection.Submit(context.Background(), "암호"); err != nil {
		t.Fatal(err)
	}
	if connection.lastCommand != previous {
		t.Fatalf("password command entered history: before=%q after=%q", previous, connection.lastCommand)
	}
	if _, err := connection.Submit(context.Background(), "old-password"); err != nil {
		t.Fatal(err)
	}
	connection.Close(context.Background())
	if connection.passwordChange != nil || connection.passwordSecret {
		t.Fatal("closed connection retained password continuation")
	}
	if worldStore.commits != 2 {
		t.Fatalf("unexpected world commit count=%d", worldStore.commits)
	}
}
