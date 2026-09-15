package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseComposeStartLines(t *testing.T) {
	tests := []struct {
		line       string
		mailTarget string
		kind       CommandKind
		ok         bool
	}{
		{line: "편지보내기", kind: CommandMailSend, ok: true},
		{line: "편지보내기 Bob", mailTarget: "Bob", kind: CommandMailSend, ok: true},
		{line: "써", kind: CommandBoardWrite, ok: true},
		{line: "편지보내기 Bob extra", ok: false},
		{line: "써 extra", ok: false},
		{line: "편지보내기\x1bBob", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			mail, mailOK := ParseMailSendLine(tt.line)
			board, boardOK := ParseBoardWriteLine(tt.line)
			parsed, err := ParseCommand(tt.line)
			if err != nil {
				t.Fatalf("ParseCommand err=%v", err)
			}
			if tt.kind == CommandMailSend {
				if !mailOK || mail.Recipient != tt.mailTarget || boardOK || parsed.Kind != tt.kind {
					t.Fatalf("mail=%+v/%v board=%+v/%v parsed=%+v", mail, mailOK, board, boardOK, parsed)
				}
				return
			}
			if tt.kind == CommandBoardWrite {
				if !boardOK || mailOK || parsed.Kind != tt.kind {
					t.Fatalf("board=%+v/%v mail=%+v/%v parsed=%+v", board, boardOK, mail, mailOK, parsed)
				}
				return
			}
			if mailOK || boardOK || tt.ok {
				t.Fatalf("unexpected compose classification mail=%v board=%v", mailOK, boardOK)
			}
		})
	}
}

type composeSessionStore struct {
	state      json.RawMessage
	receipt    *storage.WorldReceipt
	commandID  string
	commits    int
	failCommit bool
}

func (s *composeSessionStore) ReadWorldReceipt(_ context.Context, _ string, commandID string, _ json.RawMessage) (storage.WorldReceipt, error) {
	if s.receipt == nil || s.commandID != commandID {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	receipt := *s.receipt
	receipt.Replayed = true
	return receipt, nil
}

func (s *composeSessionStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	return storage.WorldSnapshot{State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *composeSessionStore) CommitWorldCommand(_ context.Context, _ string, commandID string, _ json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	if s.failCommit {
		return storage.WorldReceipt{}, errors.New("transient commit failure")
	}
	s.commits++
	s.state = append(json.RawMessage(nil), state...)
	s.commandID = commandID
	s.receipt = &storage.WorldReceipt{Revision: revision + 1, Response: append(json.RawMessage(nil), response...)}
	return *s.receipt, nil
}

func TestExecuteMailSendUsesCanonicalActorAndStableMessageID(t *testing.T) {
	state := composeMailState()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &composeSessionStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("sender")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	allocateCalls := 0
	allocate := func() (string, error) {
		allocateCalls++
		return "mail-stable", nil
	}
	payload := world.MailSendPayload{RecipientID: "recipient", Body: "본문", Timestamp: time.Unix(100, 0)}
	options := MailSendOptions{MessageID: "mail-stable", Allocate: allocate}
	first, err := owners.ExecuteMailSendWithOptions(context.Background(), store, "w", "compose-mail-1", lease, payload, options)
	if err != nil || first.Replayed || store.commits != 1 || allocateCalls != 0 {
		t.Fatalf("first=%+v err=%v commits=%d alloc=%d", first, err, store.commits, allocateCalls)
	}
	replay, err := owners.ExecuteMailSendWithOptions(context.Background(), store, "w", "compose-mail-1", lease, payload, options)
	if err != nil || !replay.Replayed || store.commits != 1 || allocateCalls != 0 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d alloc=%d", replay, err, store.commits, allocateCalls)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Mailboxes["recipient"]) != 1 || saved.Mailboxes["recipient"][0].Body != "본문\n" {
		t.Fatalf("saved=%+v err=%v", saved.Mailboxes, err)
	}
}

func composeMailState() world.State {
	var flags [8]byte
	flags[world.RoomPostOfficeFlag/8] |= 1 << (world.RoomPostOfficeFlag % 8)
	return world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: flags}}, PlayerIDs: []string{"sender"}}},
		Players: map[string]world.PlayerState{
			"sender":    {Body: world.LegacyMonster{Name: "Sender", RoomID: 1}, Online: true},
			"recipient": {Body: world.LegacyMonster{Name: "Recipient", RoomID: 1}, Online: false},
		},
		Mailboxes: map[string][]world.MailMessage{},
	}
}
