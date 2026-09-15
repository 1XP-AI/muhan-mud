package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func divorceSessionFixture(t *testing.T) []byte {
	t.Helper()
	var state world.State
	if err := json.Unmarshal(marriageSessionFixture(t), &state); err != nil {
		t.Fatal(err)
	}
	for id, spouse := range map[string]string{"alice": "mBob", "bob": "mAlice"} {
		player := state.Players[id]
		player.Body.Flags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
		player.Body.Keys[world.MarriageSpouseKeyIndex] = spouse
		state.Players[id] = player
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseMarriageFollowupLinesAndCommandClassification(t *testing.T) {
	for _, line := range []string{"이혼", "  이혼  "} {
		if _, ok := ParseDivorceLine(line); !ok || !IsDivorceLine(line) {
			t.Fatalf("divorce line rejected: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandDivorce {
			t.Fatalf("divorce classification line=%q parsed=%+v err=%v", line, parsed, err)
		}
	}
	for _, test := range []struct {
		line, message string
	}{
		{line: "사랑말", message: ""},
		{line: " hello 사랑말 ", message: "hello"},
		{line: "안녕하세요 세계 사랑말", message: "안녕하세요 세계"},
	} {
		command, ok := ParseMarriageSendLine(test.line)
		if !ok || command.Message != test.message || !IsMarriageSendLine(test.line) {
			t.Fatalf("spouse message line=%q command=%+v ok=%t", test.line, command, ok)
		}
		parsed, err := ParseCommand(test.line)
		if err != nil || parsed.Kind != CommandMarriageSend {
			t.Fatalf("spouse message classification line=%q parsed=%+v err=%v", test.line, parsed, err)
		}
	}
	for _, line := range []string{"사랑말x hello", "사랑말 hello", "hello 사랑말 extra", "이혼 extra", "이혼\n", string([]byte{0xff})} {
		if IsDivorceLine(line) || IsMarriageSendLine(line) {
			t.Fatalf("invalid follow-up accepted: %q", line)
		}
	}
}

func TestExecuteDivorceLinePersistsAcceptAndReplays(t *testing.T) {
	store := &departureStore{state: divorceSessionFixture(t)}
	var owners Ownership
	aliceLease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(aliceLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	bobLease, err := owners.Acquire("bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(bobLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	request, err := owners.ExecuteDivorceLine(context.Background(), store, "w", "divorce-request-1", aliceLease, "이혼")
	if err != nil || request.Replayed || store.commits != 1 {
		t.Fatalf("request=%+v err=%v commits=%d", request, err, store.commits)
	}
	var requested world.DivorceResult
	if err := json.Unmarshal(request.Response, &requested); err != nil {
		t.Fatal(err)
	}
	if requested.Action != world.DivorceRequest || requested.TargetID != "bob" || !requested.Changed || len(requested.Events) != 1 || !strings.Contains(requested.Response, "이혼신청") {
		t.Fatalf("request result=%+v", requested)
	}

	accept, err := owners.ExecuteDivorceLine(context.Background(), store, "w", "divorce-accept-1", bobLease, "이혼")
	if err != nil || accept.Replayed || store.commits != 2 {
		t.Fatalf("accept=%+v err=%v commits=%d", accept, err, store.commits)
	}
	var accepted world.DivorceResult
	if err := json.Unmarshal(accept.Response, &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Action != world.DivorceAccept || !accepted.Broadcast || len(accepted.Events) != 2 || !strings.Contains(accepted.Response, "받아들입니다") {
		t.Fatalf("accepted result=%+v", accepted)
	}

	replay, err := owners.ExecuteDivorceLine(context.Background(), store, "w", "divorce-accept-1", bobLease, "이혼")
	if err != nil || !replay.Replayed || store.commits != 2 || string(replay.Response) != string(accept.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	for id, key := range map[string]string{"alice": "mBob", "bob": "mAlice"} {
		body := saved.Players[id].Body
		if world.PlayerFlagSet(body, world.MarriageActiveFlag) || world.PlayerFlagSet(body, world.MarriagePendingFlag) || world.PlayerFlagSet(body, world.MarriageDivorcePendingFlag) || body.Keys[world.MarriageSpouseKeyIndex] != key {
			t.Fatalf("saved divorce state id=%s body=%+v", id, body)
		}
	}
}

func TestExecuteMarriageSendPersistsAndReplaysReceipt(t *testing.T) {
	store := &departureStore{state: divorceSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteMarriageSendLine(context.Background(), store, "w", "marriage-send-1", lease, "hello 사랑말")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MarriageSendResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.TargetID != "bob" || result.Message != "hello" || !result.Delivered || result.Event == nil || !strings.Contains(result.Response, "Bob님") {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteMarriageSendLine(context.Background(), store, "w", "marriage-send-1", lease, "hello 사랑말")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
