package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func familyStatusHandlerSetFlag(body *world.LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	body.Flags[bit/8] &^= 1 << (bit % 8)
}

func familyStatusHandlerPlayer(name string, class byte, familyID int16, member, pending bool) world.PlayerState {
	body := world.LegacyMonster{Name: name, Type: 0, Class: class, RoomID: 1}
	body.Daily[world.FamilyDailySlot].Max = byte(familyID)
	familyStatusHandlerSetFlag(&body, world.FamilyMemberFlag, member)
	familyStatusHandlerSetFlag(&body, world.FamilyPendingFlag, pending)
	return world.PlayerState{Body: body, Online: true}
}

func familyStatusHandlerFixture(t *testing.T) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor", "member", "pending"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor":   familyStatusHandlerPlayer("Alice", 4, 2, true, false),
			"member":  familyStatusHandlerPlayer("Bob", 3, 2, true, false),
			"pending": familyStatusHandlerPlayer("Carol", 4, 2, false, true),
		},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func familyStatusHandlerCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
		1: {ID: 1, Name: "백호", Boss: "Tiger"},
	}}
}

func admitFamilyStatusHandlerOwner(t *testing.T, actorID string) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func familyStatusHandlerKind(kind CommandKind) bool {
	switch kind {
	case CommandFamilyWho, CommandFamilyMember, CommandFamilyList:
		return true
	default:
		return false
	}
}

func TestFamilyStatusParseLineAndCentralClassification(t *testing.T) {
	tests := []struct {
		line   string
		action FamilyCommandAction
		target string
		kind   CommandKind
	}{
		{line: "패거리누구", action: FamilyWhoAction, kind: CommandFamilyWho},
		{line: "  패거리누구  ", action: FamilyWhoAction, kind: CommandFamilyWho},
		{line: "패거리누구 Bob", action: FamilyWhoAction, target: "Bob", kind: CommandFamilyWho},
		{line: "패거리원", action: FamilyMemberAction, kind: CommandFamilyMember},
		{line: "모든패거리", action: FamilyListAction, kind: CommandFamilyList},
	}
	for _, test := range tests {
		command, ok := ParseFamilyLine(test.line)
		if !ok || command.Action != test.action || command.Target != test.target {
			t.Fatalf("ParseFamilyLine(%q)=%+v ok=%t", test.line, command, ok)
		}
		parsed, err := ParseCommand(test.line)
		if err != nil || parsed.Kind != test.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want kind=%d", test.line, parsed, err, test.kind)
		}
	}

	for _, line := range []string{
		"패거리누구 Bob extra",
		"패거리원 extra",
		"모든패거리 extra",
		"패거리누구\nBob",
		"패거리원\x00",
		string([]byte{0xff}),
	} {
		if _, ok := ParseFamilyLine(line); ok {
			t.Fatalf("malformed family status line accepted: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil {
			continue
		}
		if familyStatusHandlerKind(parsed.Kind) {
			t.Fatalf("ParseCommand(%q) routed malformed line to family status: %+v", line, parsed)
		}
	}
}

func TestFamilyStatusExecuteUsesOneReceiptExactResponseAndReplays(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "who",
			line: "패거리누구",
			want: "당신은 [청룡] 패거리에 소속되어 있습니다.\r\n\r\n" +
				"   Alice         " + "   Bob           " + "(-)Carol         " +
				"\r\n\r\n총 3명의 패거리원들이 이용중입니다.\r\n",
		},
		{
			name: "member",
			line: "패거리원",
			want: "당신은 [청룡] 패거리에 가입되어 있습니다.\r\n" +
				"[4]  Alice            [3]  Bob              \r\n" +
				"총 2명의 사람들이 가입되어 있습니다.\r\n",
		},
		{
			name: "list",
			line: "모든패거리",
			want: "다음과 같은 패거리가 있습니다.\r\n" +
				"패거리이름      두목이름\r\n" +
				"--------------------------------------------\r\n" +
				"백호             Tiger         \r\n" +
				"청룡             Boss          \r\n" +
				"\r\n총 2 개의 패거리가 활동중에 있습니다.\r\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initial := familyStatusHandlerFixture(t)
			store := &departureStore{state: append([]byte(nil), initial...)}
			owners, lease := admitFamilyStatusHandlerOwner(t, "actor")
			first, err := owners.ExecuteFamilyLineWithCatalog(context.Background(), store, "world", "family-status-"+test.name, lease, test.line, familyStatusHandlerCatalog())
			if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
				t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
			}
			wantResponse, err := json.Marshal(test.want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first.Response, wantResponse) {
				t.Fatalf("response=%q want=%q", first.Response, wantResponse)
			}
			if !bytes.Equal(store.state, initial) {
				t.Fatal("family status changed world snapshot")
			}

			replay, err := owners.ExecuteFamilyLineWithCatalog(context.Background(), store, "world", "family-status-"+test.name, lease, test.line, familyStatusHandlerCatalog())
			if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
				t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
			}
		})
	}
}

func TestFamilyStatusExecuteRejectsBeforeReceipt(t *testing.T) {
	tests := []struct {
		name     string
		actorID  string
		line     string
		catalog  world.FamilyCatalog
		wantErrs []error
	}{
		{name: "missing catalog", actorID: "actor", line: "패거리누구", catalog: world.FamilyCatalog{}, wantErrs: []error{world.ErrFamilyCatalogUnavailable}},
		{name: "invalid actor", actorID: "missing", line: "패거리누구", catalog: familyStatusHandlerCatalog(), wantErrs: []error{world.ErrFamilyActorAbsent}},
		{name: "unsupported line", actorID: "actor", line: "패거리누구 Bob extra", catalog: familyStatusHandlerCatalog(), wantErrs: []error{ErrUnsupportedFamilyLine}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &departureStore{state: familyStatusHandlerFixture(t)}
			owners, lease := admitFamilyStatusHandlerOwner(t, test.actorID)
			receipt, err := owners.ExecuteFamilyLineWithCatalog(context.Background(), store, "world", "family-status-reject-"+test.name, lease, test.line, test.catalog)
			if err == nil {
				t.Fatalf("unexpected success receipt=%+v", receipt)
			}
			for _, wantErr := range test.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Fatalf("err=%v want errors.Is(%v)", err, wantErr)
				}
			}
			if store.commits != 0 || store.receipt != nil {
				t.Fatalf("rejected command left receipt/commit: receipt=%+v commits=%d", store.receipt, store.commits)
			}
		})
	}
}
