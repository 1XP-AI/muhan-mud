package world

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mailWorldFixture() State {
	var flags [8]byte
	flags[RoomPostOfficeFlag/8] |= 1 << (RoomPostOfficeFlag % 8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Flags: flags}}, PlayerIDs: []string{"recipient"}},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]PlayerState{
			"recipient": {Body: LegacyMonster{Name: "받는이", RoomID: 1}, Online: true},
			"sender":    {Body: LegacyMonster{Name: "보내는이", RoomID: 2}, Online: false},
		},
	}
}

func mailMessagesFixture() []MailMessage {
	return []MailMessage{
		{ID: "mail-1", SenderID: "sender", Body: "첫 번째 편지\n", Timestamp: time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)},
		{ID: "mail-2", SenderID: "sender", Body: "두 번째 편지\n다음 줄\n", Timestamp: time.Date(2026, 9, 9, 2, 3, 4, 0, time.UTC)},
	}
}

func TestMailboxesNilIsPreMigrationAndRoundTrips(t *testing.T) {
	s := mailWorldFixture()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeState(raw)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got.Mailboxes != nil {
		t.Fatal("nil mailbox migration marker was materialized")
	}
}

func TestMailboxesValidateStableOrderingIdentityAndBody(t *testing.T) {
	s := mailWorldFixture()
	s.Mailboxes = map[string][]MailMessage{"recipient": mailMessagesFixture()}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*State){
		"unknown recipient": func(s *State) { s.Mailboxes["missing"] = nil },
		"unknown sender": func(s *State) {
			messages := s.Mailboxes["recipient"]
			messages[0].SenderID = "missing"
			s.Mailboxes["recipient"] = messages
		},
		"duplicate ID": func(s *State) {
			messages := s.Mailboxes["recipient"]
			messages[1].ID = messages[0].ID
			s.Mailboxes["recipient"] = messages
		},
		"control body": func(s *State) {
			messages := s.Mailboxes["recipient"]
			messages[0].Body = "unsafe\x1b[31m"
			s.Mailboxes["recipient"] = messages
		},
		"zero timestamp": func(s *State) {
			messages := s.Mailboxes["recipient"]
			messages[0].Timestamp = time.Time{}
			s.Mailboxes["recipient"] = messages
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := mailWorldFixture()
			bad.Mailboxes = map[string][]MailMessage{"recipient": mailMessagesFixture()}
			mutate(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatal("invalid mailbox accepted")
			}
		})
	}
}

func TestMailReadPreservesOrderedMessagesAndDeleteIsAtomic(t *testing.T) {
	s := mailWorldFixture()
	s.Mailboxes = map[string][]MailMessage{"recipient": mailMessagesFixture()}
	proposal, err := s.PlanMailRead("recipient")
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyMail(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, s) || result.Action != MailRead || result.Response == MailReadResponseEmpty || len(result.Messages) != 2 || result.Messages[0].ID != "mail-1" || result.Messages[1].ID != "mail-2" {
		t.Fatalf("next=%+v result=%+v", next, result)
	}
	if !strings.Contains(result.Response, "보내는이") || !strings.Contains(result.Response, "첫 번째 편지") || !strings.Contains(result.Response, "두 번째 편지") {
		t.Fatalf("response=%q", result.Response)
	}

	deleteProposal, err := s.PlanMailDelete("recipient")
	if err != nil {
		t.Fatal(err)
	}
	deleted, deleteResult, err := s.ApplyMail(deleteProposal)
	if err != nil || deleteResult.Deleted != 2 || !deleteResult.Changed || len(deleted.Mailboxes["recipient"]) != 0 {
		t.Fatalf("deleted=%+v result=%+v err=%v", deleted, deleteResult, err)
	}
	if len(s.Mailboxes["recipient"]) != 2 {
		t.Fatal("delete mutated the source snapshot")
	}
	second, secondResult, err := deleted.ApplyMailDelete("recipient")
	if err != nil || secondResult.Deleted != 0 || secondResult.Changed || !reflect.DeepEqual(second, deleted) {
		t.Fatalf("repeat delete second=%+v result=%+v err=%v", second, secondResult, err)
	}
}

func TestMailReadAndDeleteRequireCanonicalPostOfficeActor(t *testing.T) {
	s := mailWorldFixture()
	for name, mutate := range map[string]func(*State){
		"wrong room": func(s *State) {
			r := s.Rooms[1]
			r.Resource.Flags = [8]byte{}
			s.Rooms[1] = r
		},
		"offline actor": func(s *State) {
			p := s.Players["recipient"]
			p.Online = false
			s.Players["recipient"] = p
			s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource}
		},
		"NPC actor": func(s *State) {
			p := s.Players["recipient"]
			p.Body.Type = 1
			s.Players["recipient"] = p
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := s
			mutate(&bad)
			if _, err := bad.PlanMailRead("recipient"); err == nil {
				t.Fatal("read accepted invalid actor/room")
			}
			if _, err := bad.PlanMailDelete("recipient"); err == nil {
				t.Fatal("delete accepted invalid actor/room")
			}
		})
	}
}

func TestMailStaleProposalFailsClosed(t *testing.T) {
	s := mailWorldFixture()
	s.Mailboxes = map[string][]MailMessage{"recipient": mailMessagesFixture()}
	proposal, err := s.PlanMailDelete("recipient")
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	changed.Mailboxes["recipient"][0].Body = "changed\n"
	if _, _, err := changed.ApplyMail(proposal); err == nil {
		t.Fatal("stale mail deletion accepted")
	}
}
