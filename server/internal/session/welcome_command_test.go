package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func welcomeCommandFixture(t *testing.T, online bool) []byte {
	t.Helper()
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	player := state.Players["a"]
	player.Online = online
	state.Players["a"] = player
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admittedWelcomeLease(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestExecuteWelcomeLineReadsUTF8DocumentAndReplaysWithoutWorldMutation(t *testing.T) {
	raw := welcomeCommandFixture(t, true)
	store := &departureStore{state: raw}
	owners, lease := admittedWelcomeLease(t)
	source := fstest.MapFS{
		"welcome": &fstest.MapFile{Data: []byte("무한대전에 오신 것을 환영합니다.\n[엔터]를 누르면 시작합니다.\n")},
	}

	first, err := owners.ExecuteWelcomeLine(context.Background(), store, "w", "welcome-1", lease, "  환영  ", source)
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "무한대전에 오신 것을 환영합니다") {
		t.Fatalf("welcome response=%q", text)
	}
	if string(store.state) != string(raw) {
		t.Fatal("welcome command mutated world state")
	}

	// A receipt replay must not depend on the document source still being
	// present, and must not invoke the reducer or create another commit.
	replay, err := owners.ExecuteWelcomeLine(context.Background(), store, "w", "welcome-1", lease, "환영", fstest.MapFS{})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteWelcomeLineFailsClosedForMissingOrNonUTF8Document(t *testing.T) {
	store := &departureStore{state: welcomeCommandFixture(t, true)}
	owners, lease := admittedWelcomeLease(t)

	for _, tc := range []struct {
		name   string
		source fstest.MapFS
	}{
		{name: "missing", source: fstest.MapFS{}},
		{name: "non-utf8", source: fstest.MapFS{"welcome": &fstest.MapFile{Data: []byte{0xff, 0xfe, 'x'}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]byte(nil), store.state...)
			if _, err := owners.ExecuteWelcomeLine(context.Background(), store, "w", "welcome-"+tc.name, lease, "환영", tc.source); err == nil {
				t.Fatal("invalid document unexpectedly succeeded")
			}
			if store.commits != 0 {
				t.Fatalf("invalid document created receipt: commits=%d", store.commits)
			}
			if string(store.state) != string(before) {
				t.Fatal("invalid document mutated world state")
			}
		})
	}
}

func TestExecuteWelcomeLineRequiresOnlineActor(t *testing.T) {
	for _, online := range []bool{false} {
		store := &departureStore{state: welcomeCommandFixture(t, online)}
		owners, lease := admittedWelcomeLease(t)
		source := fstest.MapFS{"welcome": &fstest.MapFile{Data: []byte("환영")}}
		if _, err := owners.ExecuteWelcomeLine(context.Background(), store, "w", "welcome-offline", lease, "환영", source); err == nil {
			t.Fatal("offline actor unexpectedly accepted")
		}
		if store.commits != 0 {
			t.Fatalf("offline actor created receipt: commits=%d", store.commits)
		}
	}
}

func TestExecuteWelcomeLineRejectsUnknownAndArgumentsBeforeReceipt(t *testing.T) {
	store := &departureStore{state: welcomeCommandFixture(t, true)}
	owners, lease := admittedWelcomeLease(t)
	source := fstest.MapFS{"welcome": &fstest.MapFile{Data: []byte("환영")}}
	for _, line := range []string{"welcome", "도움말", "환영 추가", "환영 1", ""} {
		if _, err := owners.ExecuteWelcomeLine(context.Background(), store, "w", "welcome-bad-"+line, lease, line, source); !errors.Is(err, ErrUnsupportedWelcomeLine) {
			t.Fatalf("line=%q err=%v", line, err)
		}
		if store.commits != 0 {
			t.Fatalf("unsupported line created receipt: line=%q commits=%d", line, store.commits)
		}
	}
}
