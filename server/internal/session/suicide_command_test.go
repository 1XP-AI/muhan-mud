package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
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

type suicidePasswordStore struct {
	name string
	hash []byte
}

func (s *suicidePasswordStore) LookupCredential(_ context.Context, name string) (string, []byte, error) {
	if name != s.name {
		return "", nil, storage.ErrCredentials
	}
	return "account-1", append([]byte(nil), s.hash...), nil
}
func (s *suicidePasswordStore) UpdateCredential(context.Context, string, []byte, []byte) error {
	return errors.New("update unused")
}

type suicideCaptureStore struct {
	*departureStore
	requests []json.RawMessage
}

func (s *suicideCaptureStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.requests = append(s.requests, append(json.RawMessage(nil), request...))
	return s.departureStore.CommitWorldCommand(ctx, worldID, commandID, request, revision, state, response)
}

func sessionSuicideFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: 50}, Online: true},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitSuicideOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseSuicideLineAdmitsBareAliasOnly(t *testing.T) {
	command, ok := ParseSuicideLine("  목매달기  ")
	if !ok || command.Alias != "목매달기" || !IsSuicideLine("목매달기") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{
		"목매달기 암호", "암호 목매달기", "suicide", "목매달기\n", "목매달기\x00", string([]byte{0xff}),
	} {
		if _, ok := ParseSuicideLine(line); ok || IsSuicideLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesSuicide(t *testing.T) {
	parsed, err := ParseCommand("목매달기")
	if err != nil || parsed.Kind != CommandSuicide {
		t.Fatalf("ParseCommand(목매달기)=%+v err=%v", parsed, err)
	}
	for _, line := range []string{"목매달기 암호", "suicide", "암호 목매달기"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandUnknown {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
}

func TestExecuteSuicideLineStartsContinuationAndReplaysWithoutReprompt(t *testing.T) {
	initial := sessionSuicideFixture(t)
	store := &departureStore{state: initial}
	owners, lease := admitSuicideOwner(t)
	first, err := owners.ExecuteSuicideLine(context.Background(), store, "w", "suicide-1", lease, "목매달기")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.SuicideResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.SuicidePrompt || !result.Changed || result.Continuation != world.SuicidePasswordPhase ||
		result.Response != world.SuicidePromptResponse || !result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if !world.PlayerFlagSet(saved.Players["actor"].Body, world.SuicideReadingFlag) {
		t.Fatalf("saved flags=%+v want PREADI", saved.Players["actor"].Body.Flags)
	}
	if saved.Players["actor"].Body.Gold != 50 {
		t.Fatalf("deleted or mutated gold=%d", saved.Players["actor"].Body.Gold)
	}
	if _, ok := saved.Players["actor"]; !ok {
		t.Fatal("execute deleted the actor")
	}
	replay, err := owners.ExecuteSuicideLine(context.Background(), store, "w", "suicide-1", lease, "목매달기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Flags != saved.Players["actor"].Body.Flags {
		t.Fatalf("replay mutated flags err=%v", err)
	}
}

func TestExecuteSuicideLineRejectsUnsupportedAndUnmigratedBeforeReceipt(t *testing.T) {
	initial := sessionSuicideFixture(t)
	store := &departureStore{state: initial}
	owners, lease := admitSuicideOwner(t)
	if _, err := owners.ExecuteSuicideLine(context.Background(), store, "w", "suicide-bad", lease, "suicide"); !errors.Is(err, ErrUnsupportedSuicideLine) || store.commits != 0 {
		t.Fatalf("english err=%v commits=%d", err, store.commits)
	}
	unmigrated := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
	}
	raw, err := json.Marshal(unmigrated)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	if _, err := owners.ExecuteSuicideLine(context.Background(), store, "w", "suicide-unmigrated", lease, "목매달기"); err == nil || store.commits != 0 {
		t.Fatalf("unmigrated err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteSuicidePasswordLineRejectsMismatchAndReplaysWithoutReprompt(t *testing.T) {
	initial := sessionSuicideFixture(t)
	base := &departureStore{state: initial}
	store := &suicideCaptureStore{departureStore: base}
	owners, lease := admitSuicideOwner(t)
	if _, err := owners.ExecuteSuicideLine(context.Background(), store, "w", "suicide-1", lease, "목매달기"); err != nil || store.commits != 1 {
		t.Fatalf("start err=%v commits=%d", err, store.commits)
	}
	accounts := &suicideAccounts{name: "Alice", password: "secret-pass"}
	first, err := owners.ExecuteSuicidePasswordLine(context.Background(), store, "w", "suicide-pw-1", lease, "wrong-pass", SuicidePasswordOptions{
		Accounts: accounts, Name: "Alice",
	})
	if err != nil || first.Replayed || store.commits != 2 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	if accounts.calls != 1 {
		t.Fatalf("authenticate calls=%d", accounts.calls)
	}
	var result world.SuicideResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.SuicideReject || !result.Changed || result.Continuation != 0 ||
		result.Response != world.SuicideRejectResponse || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	if bytes.Contains(first.Response, []byte("wrong-pass")) || bytes.Contains(first.Response, []byte("secret-pass")) {
		t.Fatal("password crossed the receipt response")
	}
	for _, request := range store.requests {
		if bytes.Contains(request, []byte("wrong-pass")) || bytes.Contains(request, []byte("secret-pass")) {
			t.Fatalf("password crossed the receipt request: %s", request)
		}
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.SuicideReadingFlag) {
		t.Fatalf("saved flags=%+v still PREADI", saved.Players["actor"].Body.Flags)
	}
	if saved.Players["actor"].Body.Gold != 50 {
		t.Fatalf("deleted or mutated gold=%d", saved.Players["actor"].Body.Gold)
	}
	if _, ok := saved.Players["actor"]; !ok {
		t.Fatal("mismatch deleted the actor")
	}
	replay, err := owners.ExecuteSuicidePasswordLine(context.Background(), store, "w", "suicide-pw-1", lease, "wrong-pass", SuicidePasswordOptions{
		Accounts: accounts, Name: "Alice",
	})
	if err != nil || !replay.Replayed || store.commits != 2 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Flags != saved.Players["actor"].Body.Flags {
		t.Fatalf("replay mutated flags err=%v", err)
	}
}

func TestExecuteSuicidePasswordLineMatchFailsClosedBeforeConfirm(t *testing.T) {
	initial := sessionSuicideFixture(t)
	store := &departureStore{state: initial}
	owners, lease := admitSuicideOwner(t)
	if _, err := owners.ExecuteSuicideLine(context.Background(), store, "w", "suicide-1", lease, "목매달기"); err != nil || store.commits != 1 {
		t.Fatalf("start err=%v commits=%d", err, store.commits)
	}
	accounts := &suicideAccounts{name: "Alice", password: "secret-pass"}
	if _, err := owners.ExecuteSuicidePasswordLine(context.Background(), store, "w", "suicide-pw-ok", lease, "secret-pass", SuicidePasswordOptions{
		Accounts: accounts, Name: "Alice",
	}); !errors.Is(err, world.ErrSuicideConfirmUnmigrated) || store.commits != 1 {
		t.Fatalf("match err=%v commits=%d", err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if !world.PlayerFlagSet(saved.Players["actor"].Body, world.SuicideReadingFlag) || saved.Players["actor"].Body.Gold != 50 {
		t.Fatalf("match mutated body=%+v", saved.Players["actor"].Body)
	}
	if _, ok := saved.Players["actor"]; !ok {
		t.Fatal("match deleted the actor")
	}
}

func TestExecuteSuicidePasswordLineUsesPasswordStoreAndRequiresReading(t *testing.T) {
	hash, err := identity.HashPassword([]byte("store-pass"))
	if err != nil {
		t.Fatal(err)
	}
	initial := sessionSuicideFixture(t)
	store := &departureStore{state: initial}
	owners, lease := admitSuicideOwner(t)
	if _, err := owners.ExecuteSuicidePasswordLine(context.Background(), store, "w", "suicide-pw-unarmed", lease, "store-pass", SuicidePasswordOptions{
		PasswordStore: &suicidePasswordStore{name: "Alice", hash: hash}, Name: "Alice",
	}); !errors.Is(err, world.ErrSuicideNotReading) || store.commits != 0 {
		t.Fatalf("unarmed err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteSuicideLine(context.Background(), store, "w", "suicide-1", lease, "목매달기"); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteSuicidePasswordLine(context.Background(), store, "w", "suicide-pw-store", lease, "wrong-store", SuicidePasswordOptions{
		PasswordStore: &suicidePasswordStore{name: "Alice", hash: hash}, Name: "Alice",
	})
	if err != nil || first.Replayed || store.commits != 2 {
		t.Fatalf("store mismatch=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.SuicideResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.SuicideReject || result.Response != world.SuicideRejectResponse {
		t.Fatalf("result=%+v", result)
	}
}
