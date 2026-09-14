package transport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type suicideAccounts struct {
	name     string
	password string
	calls    int
	authErr  error
}

func (s *suicideAccounts) Exists(context.Context, string) (bool, error) { return true, nil }
func (s *suicideAccounts) Register(context.Context, string, []byte, game.Creation) (string, error) {
	return "", errors.New("register unused")
}
func (s *suicideAccounts) Authenticate(_ context.Context, name string, password []byte) (storage.Character, error) {
	s.calls++
	if s.authErr != nil {
		return storage.Character{}, s.authErr
	}
	if name != s.name || string(password) != s.password {
		return storage.Character{}, storage.ErrCredentials
	}
	return storage.Character{ID: "actor", Name: name}, nil
}

func connectorSuicideState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "광장", Exits: []world.LegacyExit{{Name: "동", Destination: 2}},
				}},
				PlayerIDs: []string{"actor"},
			},
			2: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "동쪽"}},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: 50}, Online: true},
		},
	}
}

func connectorSuicideReady(t *testing.T, store *connectorCommandStore, accounts session.Accounts) *worldConnection {
	t.Helper()
	raw, err := json.Marshal(connectorSuicideState())
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "suicide", Accounts: accounts,
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4), accountName: "Alice"}
	connector.connections[conn] = struct{}{}
	return conn
}

func TestWorldConnectorSubmitDispatchesSuicidePromptAndArmsWaitState(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorSuicideReady(t, store, &suicideAccounts{name: "Alice", password: "secret-pass"})
	output, err := conn.Submit(context.Background(), "목매달기")
	if err != nil || output != world.SuicidePromptResponse || store.commits != 1 {
		t.Fatalf("suicide=%q err=%v commits=%d", output, err, store.commits)
	}
	if !conn.suicidePasswordPending || !conn.InputIsSecret() {
		t.Fatalf("pending=%t secret=%t", conn.suicidePasswordPending, conn.InputIsSecret())
	}
	if conn.lastCommand != "목매달기" {
		t.Fatalf("lastCommand=%q", conn.lastCommand)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if !world.PlayerFlagSet(saved.Players["actor"].Body, world.SuicideReadingFlag) {
		t.Fatalf("saved flags=%+v want PREADI", saved.Players["actor"].Body.Flags)
	}
	if saved.Players["actor"].Body.Gold != 50 || saved.Players["actor"].Body.RoomID != 1 {
		t.Fatalf("deleted or mutated body=%+v", saved.Players["actor"].Body)
	}
}

func TestWorldConnectorSubmitSuicidePasswordRejectsMismatchWithoutParseOrDelete(t *testing.T) {
	store := &connectorCommandStore{}
	accounts := &suicideAccounts{name: "Alice", password: "secret-pass"}
	conn := connectorSuicideReady(t, store, accounts)
	if output, err := conn.Submit(context.Background(), "목매달기"); err != nil || output != world.SuicidePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "동")
	if err != nil || output != world.SuicideRejectResponse || store.commits != 2 {
		t.Fatalf("password=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.suicidePasswordPending || conn.InputIsSecret() {
		t.Fatalf("pending=%t secret=%t after mismatch", conn.suicidePasswordPending, conn.InputIsSecret())
	}
	if conn.lastCommand != "목매달기" {
		t.Fatalf("password entered history lastCommand=%q", conn.lastCommand)
	}
	if accounts.calls != 1 {
		t.Fatalf("authenticate calls=%d", accounts.calls)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	body := saved.Players["actor"].Body
	if world.PlayerFlagSet(body, world.SuicideReadingFlag) || body.Gold != 50 || body.RoomID != 1 {
		t.Fatalf("mismatch mutated body=%+v", body)
	}
	if _, ok := saved.Players["actor"]; !ok {
		t.Fatal("mismatch deleted the actor")
	}
	if len(saved.Rooms[1].PlayerIDs) != 1 || saved.Rooms[1].PlayerIDs[0] != "actor" {
		t.Fatalf("mismatch moved occupancy=%v", saved.Rooms[1].PlayerIDs)
	}

	again, err := conn.Submit(context.Background(), "목매달기")
	if err != nil || again != world.SuicidePromptResponse || store.commits != 3 {
		t.Fatalf("return to command=%q err=%v commits=%d", again, err, store.commits)
	}
	if !conn.suicidePasswordPending {
		t.Fatal("restart did not arm suicide password continuation")
	}
}

func TestWorldConnectorSubmitSuicideDoesNotAdmitEnglishOrDelete(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorSuicideState())
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "suicide-en", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	connector.connections[conn] = struct{}{}

	output, err := conn.Submit(context.Background(), "suicide")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("english=%q err=%v commits=%d", output, err, store.commits)
	}
	output, err = conn.Submit(context.Background(), "목매달기 암호")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("args=%q err=%v commits=%d", output, err, store.commits)
	}

	unsafe := connectorSuicideState()
	actor := unsafe.Players["actor"]
	actor.Body.Name = "Alice "
	unsafe.Players["actor"] = actor
	raw, err = json.Marshal(unsafe)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	output, err = conn.Submit(context.Background(), "목매달기")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("unsafe name=%q err=%v commits=%d", output, err, store.commits)
	}
}

func TestWorldConnectorSubmitSuicidePasswordRetriesSameCommandIDWithoutReprompt(t *testing.T) {
	raw, err := json.Marshal(connectorSuicideState())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &infoContinuationRetryStore{base: base}
	accounts := &suicideAccounts{name: "Alice", password: "secret-pass"}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "suicide-retry", Accounts: accounts,
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, accountName: "Alice"}
	if output, err := conn.Submit(context.Background(), "목매달기"); err != nil || output != world.SuicidePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "wrong-pass"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.suicidePasswordPending || conn.suicidePasswordCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.suicidePasswordCommandID
	output, err := conn.Submit(context.Background(), "wrong-pass")
	if err != nil || output != world.SuicideRejectResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.suicidePasswordPending || conn.suicidePasswordCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if len(store.commands) != 3 || store.commands[1] != wantID || store.commands[2] != wantID {
		t.Fatalf("continuation command IDs=%v want repeated %q", store.commands, wantID)
	}
	if _, commits := base.snapshot(); commits != 2 {
		t.Fatalf("commits=%d want start plus one continuation", commits)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.SuicideReadingFlag) || saved.Players["actor"].Body.Gold != 50 {
		t.Fatalf("retry mutated body=%+v", saved.Players["actor"].Body)
	}
}

func TestWorldConnectorSubmitSuicidePasswordMatchDoesNotConfirmOrDelete(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorSuicideReady(t, store, &suicideAccounts{name: "Alice", password: "secret-pass"})
	if output, err := conn.Submit(context.Background(), "목매달기"); err != nil || output != world.SuicidePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "secret-pass")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 1 {
		t.Fatalf("match=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.suicidePasswordPending {
		t.Fatal("match left suicide password continuation pending")
	}
	if conn.lastCommand == "secret-pass" {
		t.Fatal("matching password entered history")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50 {
		t.Fatalf("match deleted or mutated gold=%d", saved.Players["actor"].Body.Gold)
	}
	if _, ok := saved.Players["actor"]; !ok {
		t.Fatal("match deleted the actor")
	}
}
