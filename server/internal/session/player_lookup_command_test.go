package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func playerLookupCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a", "b"},
			},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Race: 5, Level: 3}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Race: 5, Level: 7}, Online: true},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 2, Type: 0, Class: 4, Race: 5, Level: 9}, Online: false},
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

func TestParsePlayerLookupLineAdmitsOnlyBoundedExactNameForms(t *testing.T) {
	tests := []struct {
		line   string
		kind   PlayerLookupKind
		target string
		ok     bool
	}{
		{line: "사용자검색 Bob", kind: PlayerLookupSearch, target: "Bob", ok: true},
		{line: "사용자정보 Bob", kind: PlayerLookupInfo, target: "Bob", ok: true},
		{line: "사용자검색 \"Bob\"", kind: PlayerLookupSearch, target: "Bob", ok: true},
		{line: "사용자검색", ok: false},
		{line: "사용자정보 Bob Carol", ok: false},
		{line: "누구 Bob", ok: false},
		{line: "사용자검색 Bob\n", ok: false},
	}
	for _, tc := range tests {
		got, ok := ParsePlayerLookupLine(tc.line)
		if ok != tc.ok || (ok && (got.Kind != tc.kind || got.Target != tc.target)) {
			t.Fatalf("ParsePlayerLookupLine(%q)=%+v,%v want kind=%d target=%q ok=%v", tc.line, got, ok, tc.kind, tc.target, tc.ok)
		}
	}
}

func TestExecutePlayerSearchLinePersistsCanonicalResponseAndReplays(t *testing.T) {
	store := &departureStore{state: playerLookupCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecutePlayerSearchLine(context.Background(), store, "w", "player-search-1", lease, "사용자검색 Bob")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "사용자: Bob") || !strings.Contains(text, "직업: 검사") || !strings.Contains(text, "종족: 인간족") {
		t.Fatalf("search response=%q", text)
	}
	if store.commits != 1 {
		t.Fatalf("read-only search committed unexpected count=%d", store.commits)
	}

	replay, err := owners.ExecutePlayerSearchLine(context.Background(), store, "w", "player-search-1", lease, "사용자검색 Bob")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecutePlayerLookupInfoIsOnlineCanonicalAndOfflineFailsClosed(t *testing.T) {
	store := &departureStore{state: playerLookupCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecutePlayerInfoLookupLine(context.Background(), store, "w", "player-info-1", lease, "사용자정보 Bob")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	want := "사용자: Bob\r\n종족: 인간족\r\n직업: 검사\r\n현재 접속 중 입니다.\r\n"
	if text != want {
		t.Fatalf("info response=%q want=%q", text, want)
	}

	replay, err := owners.ExecutePlayerInfoLookupLine(context.Background(), store, "w", "player-info-1", lease, "사용자정보 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}

	offline, err := owners.ExecutePlayerInfoLookupLine(context.Background(), store, "w", "player-info-2", lease, "사용자정보 Carol")
	if err != nil || offline.Replayed || store.commits != 2 {
		t.Fatalf("offline=%+v err=%v commits=%d", offline, err, store.commits)
	}
	if err := json.Unmarshal(offline.Response, &text); err != nil {
		t.Fatal(err)
	}
	if text != PlayerInfoUnavailableResponse {
		t.Fatalf("offline response=%q want=%q", text, PlayerInfoUnavailableResponse)
	}
	offlineReplay, err := owners.ExecutePlayerInfoLookupLine(context.Background(), store, "w", "player-info-2", lease, "사용자정보 Carol")
	if err != nil || !offlineReplay.Replayed || store.commits != 2 || string(offlineReplay.Response) != string(offline.Response) {
		t.Fatalf("offline replay=%+v err=%v commits=%d", offlineReplay, err, store.commits)
	}
}

func TestExecutePlayerLookupRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: playerLookupCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"사용자검색", "사용자정보 Bob Carol", "누구 Bob"} {
		if _, err := owners.ExecutePlayerLookupLine(context.Background(), store, "w", "player-bad-"+line, lease, line); err != ErrUnsupportedPlayerLookupLine {
			t.Fatalf("line %q err=%v", line, err)
		}
		if store.commits != 0 {
			t.Fatalf("unsupported line %q created receipt", line)
		}
	}
}
