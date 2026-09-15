package world

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mailSendWorldFixture() State {
	var flags [8]byte
	flags[RoomPostOfficeFlag/8] |= 1 << (RoomPostOfficeFlag % 8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Flags: flags}}, PlayerIDs: []string{"sender-id"}},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]PlayerState{
			"sender-id":    {Body: LegacyMonster{Name: "보내는이", RoomID: 1}, Online: true},
			"recipient-id": {Body: LegacyMonster{Name: "받는이", RoomID: 2}, Online: false},
		},
		// A nonnil empty map means the post directory has been imported and is
		// ready to accept a first canonical message.
		Mailboxes: map[string][]MailMessage{},
	}
}

func mailSendAllocator(id string, calls *int) MailMessageIDAllocator {
	return func() (string, error) {
		*calls = *calls + 1
		return id, nil
	}
}

func validMailSendMessages(count int) []MailMessage {
	messages := make([]MailMessage, count)
	for i := range messages {
		messages[i] = MailMessage{
			ID:        "old-" + strconv.Itoa(i),
			SenderID:  "sender-id",
			Sender:    "",
			Body:      "old\n",
			Timestamp: time.Unix(int64(i+1), 0),
		}
	}
	return messages
}

func TestResolveMailRecipientIDAcceptsCanonicalIDAndExactLegacyName(t *testing.T) {
	s := mailSendWorldFixture()
	if got, err := s.ResolveMailRecipientID("recipient-id"); err != nil || got != "recipient-id" {
		t.Fatalf("canonical ID got=%q err=%v", got, err)
	}
	if got, err := s.ResolveMailRecipientID("받는이"); err != nil || got != "recipient-id" {
		t.Fatalf("display name got=%q err=%v", got, err)
	}
	for _, tc := range []struct {
		name string
		want error
	}{
		{name: "", want: ErrMailSendRecipientRequired},
		{name: "unknown", want: ErrMailSendRecipientAbsent},
		{name: "sender-id", want: nil},
	} {
		got, err := s.ResolveMailRecipientID(tc.name)
		if tc.want == nil {
			if err != nil || got != tc.name {
				t.Fatalf("ResolveMailRecipientID(%q) got=%q err=%v", tc.name, got, err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Fatalf("ResolveMailRecipientID(%q) err=%v want %v", tc.name, err, tc.want)
		}
	}
}

func TestResolveMailRecipientIDRejectsAmbiguousNameAndNPC(t *testing.T) {
	s := mailSendWorldFixture()
	s.Players["other-id"] = PlayerState{Body: LegacyMonster{Name: "받는이", RoomID: 2}, Online: false}
	if _, err := s.ResolveMailRecipientID("받는이"); !errors.Is(err, ErrMailSendRecipientAmbiguous) {
		t.Fatalf("ambiguous recipient err=%v", err)
	}
	// NPCs are not admitted to State.Players; a legacy target that is not a
	// canonical player therefore resolves as absent.
	if _, err := s.ResolveMailRecipientID("npc-id"); !errors.Is(err, ErrMailSendRecipientAbsent) {
		t.Fatalf("non-player recipient err=%v", err)
	}
}

func TestCanonicalizeMailSendBodyPreservesLinesAndNormalizesFinalNewline(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "one line", body: "첫 줄", want: "첫 줄\n"},
		{name: "already terminated", body: "첫 줄\n둘째 줄\n", want: "첫 줄\n둘째 줄\n"},
		{name: "empty editor", body: "", want: "\n"},
		{name: "blank line", body: "첫 줄\n\n", want: "첫 줄\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeMailSendBody(tc.body)
			if err != nil || got != tc.want {
				t.Fatalf("canonical body=%q err=%v want=%q", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{name: "invalid UTF8", body: string([]byte{0xff}), want: ErrMailSendPayload},
		{name: "control", body: "bad\x1b", want: ErrMailSendPayload},
		{name: "line too long", body: strings.Repeat("a", MaxMailSendLineBytes+1), want: ErrMailSendBodyLineTooLong},
		{name: "body too long", body: strings.Repeat("a\n", MaxMailBodyBytes/2+1), want: ErrMailSendPayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMailSendBody(tc.body); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateMailSendBody err=%v want %v", err, tc.want)
			}
		})
	}
	if err := ValidateMailSendBody(strings.Repeat("a", MaxMailSendLineBytes)); err != nil {
		t.Fatalf("79-byte line rejected: %v", err)
	}
}

func TestPlanAndApplyMailSendAppendsOneCanonicalMessage(t *testing.T) {
	s := mailSendWorldFixture()
	before := s.clone()
	calls := 0
	proposal, err := s.PlanMailSend("sender-id", MailSendPayload{
		RecipientID: "recipient-id",
		Body:        "첫 줄\n둘째 줄",
		Timestamp:   time.Date(2026, 9, 9, 10, 11, 12, 0, time.FixedZone("KST", 9*60*60)),
	}, mailSendAllocator("mail-1", &calls))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("allocator calls=%d want 1", calls)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated source state")
	}
	next, result, err := s.ApplyMailSend(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("apply reran allocator: calls=%d", calls)
	}
	if result.Action != MailSend || !result.Changed || result.ActorID != "sender-id" || result.SenderID != "sender-id" || result.SenderName != "보내는이" || result.RecipientID != "recipient-id" || result.RecipientName != "받는이" || result.MessageID != "mail-1" || result.Response != MailSendResponse {
		t.Fatalf("result=%+v", result)
	}
	messages := next.Mailboxes["recipient-id"]
	if len(messages) != 1 {
		t.Fatalf("messages=%+v", messages)
	}
	message := messages[0]
	if message.ID != "mail-1" || message.SenderID != "sender-id" || message.Sender != "" || message.Body != "첫 줄\n둘째 줄\n" || !message.Timestamp.Equal(time.Date(2026, 9, 9, 1, 11, 12, 0, time.UTC)) {
		t.Fatalf("message=%+v", message)
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("next invalid: %v", err)
	}
	if !reflect.DeepEqual(s, before) || len(s.Mailboxes) != 0 {
		t.Fatal("apply mutated source state")
	}
}

func TestPlanMailSendByNameAndEmptyEditorBody(t *testing.T) {
	s := mailSendWorldFixture()
	calls := 0
	proposal, err := s.PlanMailSendByName("sender-id", "받는이", "", time.Unix(1_000, 0), mailSendAllocator("mail-empty", &calls))
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyMailSend(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.MessageID != "mail-empty" || next.Mailboxes["recipient-id"][0].Body != "\n" {
		t.Fatalf("calls=%d result=%+v next=%+v", calls, result, next)
	}
}

func TestPlanMailSendRejectsEveryGateBeforeAllocating(t *testing.T) {
	base := mailSendWorldFixture()
	base.Mailboxes["recipient-id"] = []MailMessage{{
		ID: "old-mail", SenderID: "sender-id", Sender: "보내는이", Body: "old\n", Timestamp: time.Unix(10, 0),
	}}
	tests := []struct {
		name  string
		state func(State) State
		input MailSendPayload
		alloc MailMessageIDAllocator
		want  error
	}{
		{name: "wrong room", state: func(s State) State { room := s.Rooms[1]; room.Resource.Flags = [8]byte{}; s.Rooms[1] = room; return s }, input: MailSendPayload{RecipientID: "recipient-id", Body: "ok", Timestamp: time.Unix(1, 0)}, want: ErrMailNotPostOffice},
		{name: "unimported mailbox", state: func(s State) State { s.Mailboxes = nil; return s }, input: MailSendPayload{RecipientID: "recipient-id", Body: "ok", Timestamp: time.Unix(1, 0)}, want: ErrMailSendMailboxUnimported},
		{name: "missing recipient", input: MailSendPayload{RecipientID: "missing", Body: "ok", Timestamp: time.Unix(1, 0)}, want: ErrMailSendRecipientAbsent},
		{name: "zero timestamp", input: MailSendPayload{RecipientID: "recipient-id", Body: "ok"}, want: ErrMailSendTimestampInvalid},
		{name: "line overflow", input: MailSendPayload{RecipientID: "recipient-id", Body: strings.Repeat("a", MaxMailSendLineBytes+1), Timestamp: time.Unix(1, 0)}, want: ErrMailSendBodyLineTooLong},
		{name: "mailbox full", state: func(s State) State { s.Mailboxes["recipient-id"] = validMailSendMessages(MaxMailboxSize); return s }, input: MailSendPayload{RecipientID: "recipient-id", Body: "ok", Timestamp: time.Unix(1, 0)}, want: ErrMailSendMailboxFull},
		{name: "nil allocator", input: MailSendPayload{RecipientID: "recipient-id", Body: "ok", Timestamp: time.Unix(1, 0)}, want: ErrMailSendMessageIDAllocator},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base.clone()
			if tc.state != nil {
				s = tc.state(s)
			}
			calls := 0
			alloc := tc.alloc
			if alloc == nil && tc.name != "nil allocator" {
				alloc = mailSendAllocator("fresh", &calls)
			}
			_, err := s.PlanMailSend("sender-id", tc.input, alloc)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if calls != 0 {
				t.Fatalf("allocator called on rejected plan: %d", calls)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		state func(State) State
		input MailSendPayload
		want  error
	}{
		{name: "offline actor", state: func(s State) State {
			p := s.Players["sender-id"]
			p.Online = false
			s.Players["sender-id"] = p
			s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource}
			return s
		}, input: MailSendPayload{RecipientID: "recipient-id", Body: "ok", Timestamp: time.Unix(1, 0)}, want: ErrMailActorAbsent},
		{name: "NPC actor", state: func(s State) State {
			p := s.Players["sender-id"]
			p.Body.Type = 1
			s.Players["sender-id"] = p
			return s
		}, input: MailSendPayload{RecipientID: "recipient-id", Body: "ok", Timestamp: time.Unix(1, 0)}, want: nil},
		{name: "ambiguous recipient", state: func(s State) State {
			s.Players["other-id"] = PlayerState{Body: LegacyMonster{Name: "받는이", RoomID: 2}}
			return s
		}, input: MailSendPayload{RecipientID: "받는이", Body: "ok", Timestamp: time.Unix(1, 0)}, want: ErrMailSendRecipientAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.state(base.clone())
			calls := 0
			_, err := s.PlanMailSend("sender-id", tc.input, mailSendAllocator("fresh", &calls))
			if tc.want == nil {
				if err == nil {
					t.Fatal("invalid NPC actor accepted")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if calls != 0 {
				t.Fatalf("allocator called on rejected plan: %d", calls)
			}
		})
	}
}

func TestPlanMailSendRejectsBadOrConflictingAllocatedID(t *testing.T) {
	s := mailSendWorldFixture()
	s.Mailboxes["recipient-id"] = []MailMessage{{ID: "taken", SenderID: "sender-id", Sender: "보내는이", Body: "old\n", Timestamp: time.Unix(10, 0)}}
	payload := MailSendPayload{RecipientID: "recipient-id", Body: "ok", Timestamp: time.Unix(20, 0)}
	allocatorErr := errors.New("sequence unavailable")
	if _, err := s.PlanMailSend("sender-id", payload, func() (string, error) { return "", allocatorErr }); !errors.Is(err, ErrMailSendMessageIDAllocator) || !errors.Is(err, allocatorErr) {
		t.Fatalf("allocator error=%v", err)
	}
	for _, tc := range []struct {
		name string
		id   string
		want error
	}{
		{name: "conflict", id: "taken", want: ErrMailSendMessageIDConflict},
		{name: "empty", id: "", want: ErrMailSendMessageIDInvalid},
		{name: "control", id: "bad\x1b", want: ErrMailSendMessageIDInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.PlanMailSend("sender-id", payload, mailSendAllocator(tc.id, new(int)))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

func TestApplyMailSendRejectsStaleOrTamperedProposalWithoutMutation(t *testing.T) {
	s := mailSendWorldFixture()
	proposal, err := s.PlanMailSend("sender-id", MailSendPayload{RecipientID: "recipient-id", Body: "hello", Timestamp: time.Unix(50, 0)}, mailSendAllocator("mail-1", new(int)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		state  func(State) State
		mutate func(*MailSendProposal)
		want   error
	}{
		{name: "mailbox changed", state: func(s State) State {
			s.Mailboxes["recipient-id"] = []MailMessage{{ID: "other", SenderID: "sender-id", Sender: "보내는이", Body: "other\n", Timestamp: time.Unix(40, 0)}}
			return s
		}, want: ErrMailSendStaleProposal},
		{name: "actor changed", state: func(s State) State {
			p := s.Players["sender-id"]
			p.Body.Name = "바뀐보내는이"
			s.Players["sender-id"] = p
			return s
		}, want: ErrMailSendStaleProposal},
		{name: "recipient changed", state: func(s State) State {
			p := s.Players["recipient-id"]
			p.Body.Name = "바뀐이"
			s.Players["recipient-id"] = p
			return s
		}, want: ErrMailSendStaleProposal},
		{name: "body tampered", mutate: func(p *MailSendProposal) { p.Body = "tampered\n" }, want: ErrMailSendInvalidProposal},
		{name: "ID tampered", mutate: func(p *MailSendProposal) { p.MessageID = "other-id" }, want: ErrMailSendInvalidProposal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := s.clone()
			if tc.state != nil {
				candidate = tc.state(candidate)
			}
			attempt := proposal
			if tc.mutate != nil {
				tc.mutate(&attempt)
			}
			before := candidate.clone()
			_, _, err := candidate.ApplyMailSend(attempt)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if !reflect.DeepEqual(candidate, before) {
				t.Fatal("failed apply mutated candidate")
			}
		})
	}
}

func TestApplyMailSendDoesNotReplayProposalAgainstCommittedState(t *testing.T) {
	s := mailSendWorldFixture()
	calls := 0
	proposal, err := s.PlanMailSend("sender-id", MailSendPayload{RecipientID: "recipient-id", Body: "once", Timestamp: time.Unix(100, 0)}, mailSendAllocator("mail-1", &calls))
	if err != nil {
		t.Fatal(err)
	}
	committed, _, err := s.ApplyMailSend(proposal)
	if err != nil {
		t.Fatal(err)
	}
	before := committed.clone()
	if _, _, err := committed.ApplyMailSend(proposal); !errors.Is(err, ErrMailSendStaleProposal) && !errors.Is(err, ErrMailSendMessageIDConflict) {
		t.Fatalf("replay err=%v", err)
	}
	if !reflect.DeepEqual(committed, before) || calls != 1 {
		t.Fatalf("replay mutated state or allocated again: calls=%d", calls)
	}
}
