package session

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func expressCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 30, HPCurrent: 30}, Online: true},
		},
	}
	p := s.Players["a"]
	p.Body.Flags[0] |= 1 << 1 // PHIDDN
	s.Players["a"] = p
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newExpressOwners(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return &owners, lease
}

func TestParseExpressLineKeepsOneBoundedFreeFormPayload(t *testing.T) {
	for _, tc := range []struct {
		line string
		text string
	}{
		{line: "표현", text: ""},
		{line: " 표현   hello  world ", text: "hello  world"},
		{line: "표현 가\t나", text: ""}, // control payload is rejected below
	} {
		command, ok := ParseExpressLine(tc.line)
		if tc.line == "표현 가\t나" {
			if ok {
				t.Fatalf("control payload accepted: %+v", command)
			}
			continue
		}
		if !ok || command.Text != tc.text {
			t.Fatalf("line=%q command=%+v ok=%v", tc.line, command, ok)
		}
	}
	for _, line := range []string{
		"",
		"표현x hello",
		"표현 \xff",
		"표현 " + strings.Repeat("a", world.MaxExpressTextBytes+1),
		"표현 hello\nworld",
		"표현 hello\n",
	} {
		if _, ok := ParseExpressLine(line); ok {
			t.Fatalf("unsupported 표현 line accepted: %q", line)
		}
	}
}

func TestExecuteExpressLinePersistsOnlyResponseAndReplays(t *testing.T) {
	store := &departureStore{state: expressCommandFixture(t)}
	owners, lease := newExpressOwners(t)
	first, err := owners.ExecuteExpressLine(context.Background(), store, "w", "express-1", lease, "표현 hello %s")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var response string
	if err := json.Unmarshal(first.Response, &response); err != nil || response != "예. 좋습니다.\r\n" {
		t.Fatalf("response=%q raw=%s err=%v", response, first.Response, err)
	}
	if strings.Contains(string(first.Response), "hello") {
		t.Fatal("arbitrary room event text was persisted in the receipt")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if flag := saved.Players["a"].Body.Flags[0]&(1<<1) != 0; flag {
		t.Fatal("successful expression did not clear PHIDDN")
	}
	event, ok, err := saved.RoomExpressEvent("a", "hello %s")
	if err != nil || !ok || event.Text != "\n:Alice님이 hello %s.\r\n" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}

	replay, err := owners.ExecuteExpressLine(context.Background(), store, "w", "express-1", lease, "표현 hello %s")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteExpressLineEmptyAndSilentCommitPureSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{name: "empty", line: "표현", want: "무슨말을 표현하시려구요?\r\n"},
		{name: "silent", line: "표현 hello", want: "당신은 지금당장 그것을 할 수 없습니다.\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := expressCommandFixture(t)
			if tc.name == "silent" {
				s, err := world.DecodeState(raw)
				if err != nil {
					t.Fatal(err)
				}
				p := s.Players["a"]
				p.Body.Flags[44/8] |= 1 << (44 % 8) // PSILNC
				s.Players["a"] = p
				raw, err = json.Marshal(s)
				if err != nil {
					t.Fatal(err)
				}
			}
			store := &departureStore{state: raw}
			before := append([]byte(nil), raw...)
			owners, lease := newExpressOwners(t)
			first, err := owners.ExecuteExpressLine(context.Background(), store, "w", "express-"+tc.name, lease, tc.line)
			if err != nil || first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
			}
			var response string
			if err := json.Unmarshal(first.Response, &response); err != nil || response != tc.want {
				t.Fatalf("response=%q want=%q err=%v", response, tc.want, err)
			}
			if tc.name == "empty" || tc.name == "silent" {
				if !bytes.Equal(store.state, before) {
					t.Fatal("no-op expression partially changed the snapshot")
				}
			}
			replay, err := owners.ExecuteExpressLine(context.Background(), store, "w", "express-"+tc.name, lease, tc.line)
			if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
				t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
			}
		})
	}
}

func TestExecuteExpressLineRejectsMalformedOrOversizedWithoutReceipt(t *testing.T) {
	for _, line := range []string{
		"표현 \xff",
		"표현 " + strings.Repeat("a", world.MaxExpressTextBytes+1),
		"표현 hello\rworld",
	} {
		store := &departureStore{state: expressCommandFixture(t)}
		before := append([]byte(nil), store.state...)
		owners, lease := newExpressOwners(t)
		if _, err := owners.ExecuteExpressLine(context.Background(), store, "w", "express-invalid", lease, line); err == nil || store.commits != 0 || !bytes.Equal(store.state, before) {
			t.Fatalf("line=%q err=%v commits=%d stateChanged=%v", line, err, store.commits, !bytes.Equal(store.state, before))
		}
	}
}

func TestExecuteExpressLineCommitFailureHasNoPartialState(t *testing.T) {
	store := &departureStore{state: expressCommandFixture(t), fail: true}
	before := append([]byte(nil), store.state...)
	owners, lease := newExpressOwners(t)
	if _, err := owners.ExecuteExpressLine(context.Background(), store, "w", "express-fail", lease, "표현 hello"); err == nil || store.commits != 1 || !bytes.Equal(store.state, before) {
		t.Fatalf("err=%v commits=%d stateChanged=%v", err, store.commits, !bytes.Equal(store.state, before))
	}
}

func TestExpressLineTextDoesNotNormalizeMalformedPayload(t *testing.T) {
	if text, ok := ExpressLineText("표현 hello"); !ok || text != "hello" {
		t.Fatalf("text=%q ok=%v", text, ok)
	}
	if _, ok := ExpressLineText(string([]byte{0xff})); ok {
		t.Fatal("invalid UTF-8 표현 alias accepted")
	}
	if got, ok := ExpressLineText("표현"); !ok || !reflect.DeepEqual(got, "") {
		t.Fatalf("empty text=%q ok=%v", got, ok)
	}
}
