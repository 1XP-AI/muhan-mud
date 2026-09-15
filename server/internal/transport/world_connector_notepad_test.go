package transport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func notepadConnectorFixture(class byte) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Caretaker", RoomID: 1, Class: class},
				Online: true,
			},
		},
		Notepad: []string{},
	}
}

func newNotepadConnection(t *testing.T, initial world.State, store engine.CommandStore) *worldConnection {
	t.Helper()
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if memory, ok := store.(*connectorCommandStore); ok {
		memory.state = raw
	}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "notepad-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
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
	return &worldConnection{game: connector, lease: lease, ready: true}
}

func notepadLinesWithCount(count int) []string {
	lines := make([]string, count)
	if count > 0 {
		lines[0] = world.NotepadHeaderLine
	}
	if count > 1 {
		for i := 2; i < count; i++ {
			lines[i] = "existing"
		}
	}
	return lines
}

func notepadBodyLinesWithBytes(total int) []string {
	lines := []string{}
	for total > world.MaxNotepadLineBytes+1 {
		lines = append(lines, strings.Repeat("b", world.MaxNotepadLineBytes))
		total -= world.MaxNotepadLineBytes + 1
	}
	if total > 0 {
		lines = append(lines, strings.Repeat("b", total-1))
	}
	return lines
}

func notepadStateWithBytes(total int) world.State {
	state := notepadConnectorFixture(10)
	lines := []string{world.NotepadHeaderLine, ""}
	lines = append(lines, notepadBodyLinesWithBytes(total-notepadDraftBytes(lines))...)
	state.Notepad = lines
	return state
}

func setNotepadStoreState(t *testing.T, store *connectorCommandStore, state world.State) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.state = raw
	store.mu.Unlock()
}

func TestWorldConnectorNotepadContinuationChecksCurrentCanonicalLineAndByteLimits(t *testing.T) {
	t.Run("line boundary commits once and rejects the next line before dot", func(t *testing.T) {
		initial := notepadConnectorFixture(10)
		initial.Notepad = notepadLinesWithCount(world.MaxNotepadLines - 1)
		raw, err := json.Marshal(initial)
		if err != nil {
			t.Fatal(err)
		}
		store := &connectorCommandStore{state: raw}
		connection := newNotepadConnection(t, initial, store)

		if output, err := connection.Submit(context.Background(), "*notepad a"); err != nil || output != world.NotepadAppendPrompt {
			t.Fatalf("start output=%q err=%v", output, err)
		}
		if output, err := connection.Submit(context.Background(), "boundary"); err != nil || output != world.NotepadAppendContinuePrompt || store.commits != 0 {
			t.Fatalf("boundary output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
		}
		if output, err := connection.Submit(context.Background(), "."); err != nil || output != world.NotepadAppendResponse || connection.notepad != nil || store.commits != 1 {
			t.Fatalf("dot output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
		}

		if output, err := connection.Submit(context.Background(), "*notepad a"); err != nil || output != world.NotepadAppendPrompt {
			t.Fatalf("second start output=%q err=%v", output, err)
		}
		if output, err := connection.Submit(context.Background(), "over-limit"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || connection.notepad != nil || store.commits != 1 {
			t.Fatalf("over-limit output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
		}
	})

	t.Run("byte boundary commits once and rejects the next line before dot", func(t *testing.T) {
		initial := notepadStateWithBytes(world.MaxNotepadBytes - world.MaxNotepadLineBytes - 1)
		raw, err := json.Marshal(initial)
		if err != nil {
			t.Fatal(err)
		}
		store := &connectorCommandStore{state: raw}
		connection := newNotepadConnection(t, initial, store)

		if output, err := connection.Submit(context.Background(), "*메모 a"); err != nil || output != world.NotepadAppendPrompt {
			t.Fatalf("start output=%q err=%v", output, err)
		}
		boundary := strings.Repeat("z", world.MaxNotepadLineBytes)
		if output, err := connection.Submit(context.Background(), boundary); err != nil || output != world.NotepadAppendContinuePrompt || store.commits != 0 {
			t.Fatalf("boundary output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
		}
		if output, err := connection.Submit(context.Background(), "."); err != nil || output != world.NotepadAppendResponse || connection.notepad != nil || store.commits != 1 {
			t.Fatalf("dot output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil || notepadDraftBytes(saved.Notepad) != world.MaxNotepadBytes {
			t.Fatalf("saved bytes=%d err=%v", notepadDraftBytes(saved.Notepad), err)
		}

		if output, err := connection.Submit(context.Background(), "*메모 a"); err != nil || output != world.NotepadAppendPrompt {
			t.Fatalf("second start output=%q err=%v", output, err)
		}
		if output, err := connection.Submit(context.Background(), "over-byte-limit"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || connection.notepad != nil || store.commits != 1 {
			t.Fatalf("over-limit output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
		}
	})

	t.Run("utf8 truncation is checked after canonicalization", func(t *testing.T) {
		initial := notepadConnectorFixture(10)
		raw, err := json.Marshal(initial)
		if err != nil {
			t.Fatal(err)
		}
		store := &connectorCommandStore{state: raw}
		connection := newNotepadConnection(t, initial, store)
		if output, err := connection.Submit(context.Background(), "*notepad a"); err != nil || output != world.NotepadAppendPrompt {
			t.Fatalf("start output=%q err=%v", output, err)
		}

		line := strings.Repeat("한", world.MaxNotepadLineBytes)
		if output, err := connection.Submit(context.Background(), line); err != nil || output != world.NotepadAppendContinuePrompt || store.commits != 0 {
			t.Fatalf("utf8 output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
		}
		if connection.notepad == nil || len(connection.notepad.lines) != 1 || len(connection.notepad.lines[0]) > world.MaxNotepadLineBytes || !utf8.ValidString(connection.notepad.lines[0]) {
			t.Fatalf("utf8 draft=%+v", connection.notepad)
		}
		if output, err := connection.Submit(context.Background(), "."); err != nil || output != world.NotepadAppendResponse || store.commits != 1 {
			t.Fatalf("dot output=%q err=%v commits=%d", output, err, store.commits)
		}
	})
}

func TestWorldConnectorNotepadContinuationAccountsForEmptyFileHeader(t *testing.T) {
	initial := notepadConnectorFixture(10)
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connection := newNotepadConnection(t, initial, store)
	if output, err := connection.Submit(context.Background(), "*notepad a"); err != nil || output != world.NotepadAppendPrompt {
		t.Fatalf("start output=%q err=%v", output, err)
	}

	// Seed the local draft to the byte boundary so one more 79-byte line
	// would fit only if the empty-file header and separator were omitted. The
	// seed avoids issuing millions of individual terminal lines in this test;
	// admission still runs through the connection continuation path.
	connection.notepad.lines = notepadBodyLinesWithBytes(world.MaxNotepadBytes - world.MaxNotepadLineBytes - 1)
	if output, err := connection.Submit(context.Background(), strings.Repeat("x", world.MaxNotepadLineBytes)); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || connection.notepad != nil || store.commits != 0 {
		t.Fatalf("header over-limit output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
}

func TestWorldConnectorNotepadContinuationUsesCanonicalChangesWithBufferedDraft(t *testing.T) {
	initial := notepadConnectorFixture(10)
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connection := newNotepadConnection(t, initial, store)
	if output, err := connection.Submit(context.Background(), "*메모 a"); err != nil || output != world.NotepadAppendPrompt {
		t.Fatalf("start output=%q err=%v", output, err)
	}
	if output, err := connection.Submit(context.Background(), "local-draft"); err != nil || output != world.NotepadAppendContinuePrompt || connection.notepad == nil || store.commits != 0 {
		t.Fatalf("draft output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}

	changed := notepadConnectorFixture(10)
	changed.Notepad = notepadLinesWithCount(world.MaxNotepadLines - 1)
	setNotepadStoreState(t, store, changed)
	if output, err := connection.Submit(context.Background(), "canonical-plus-draft"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || connection.notepad != nil || store.commits != 0 {
		t.Fatalf("concurrent limit output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
}

func TestWorldConnectorNotepadContinuationStaysLocalUntilDot(t *testing.T) {
	initial := notepadConnectorFixture(10)
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connection := newNotepadConnection(t, initial, store)

	output, err := connection.Submit(context.Background(), "*메모 a")
	if err != nil || output != world.NotepadAppendPrompt || connection.notepad == nil || store.commits != 0 {
		t.Fatalf("start output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	before := append([]byte(nil), store.state...)
	longLine := strings.Repeat("x", world.MaxNotepadLineBytes+10)
	output, err = connection.Submit(context.Background(), longLine)
	if err != nil || output != world.NotepadAppendContinuePrompt || len(connection.notepad.lines) != 1 || len(connection.notepad.lines[0]) != world.MaxNotepadLineBytes || store.commits != 0 || !bytes.Equal(store.state, before) {
		t.Fatalf("body output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	output, err = connection.Submit(context.Background(), "second")
	if err != nil || output != world.NotepadAppendContinuePrompt || len(connection.notepad.lines) != 2 || store.commits != 0 {
		t.Fatalf("second output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	output, err = connection.Submit(context.Background(), ".")
	if err != nil || output != world.NotepadAppendResponse || connection.notepad != nil || store.commits != 1 {
		t.Fatalf("commit output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Notepad) != 4 || len(saved.Notepad[2]) != world.MaxNotepadLineBytes || saved.Notepad[3] != "second" {
		t.Fatalf("saved notepad=%+v err=%v", saved.Notepad, err)
	}

	output, err = connection.Submit(context.Background(), "*notepad")
	if err != nil || output != world.NotepadHeaderLine+"\n\n"+strings.Repeat("x", world.MaxNotepadLineBytes)+"\nsecond\n" || store.commits != 2 {
		t.Fatalf("view output=%q err=%v commits=%d", output, err, store.commits)
	}
	output, err = connection.Submit(context.Background(), "*메모 x")
	if err != nil || output != world.NotepadInvalidOptionResponse || store.commits != 3 {
		t.Fatalf("invalid output=%q err=%v commits=%d", output, err, store.commits)
	}
	output, err = connection.Submit(context.Background(), "*notepad d")
	if err != nil || output != world.NotepadClearResponse || store.commits != 4 {
		t.Fatalf("clear output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || saved.Notepad == nil || len(saved.Notepad) != 0 {
		t.Fatalf("cleared notepad=%+v err=%v", saved.Notepad, err)
	}
	output, err = connection.Submit(context.Background(), "*notepad a extra")
	if err != nil || output != world.NotepadMissingResponse || store.commits != 5 {
		t.Fatalf("extra-token view output=%q err=%v commits=%d", output, err, store.commits)
	}
	output, err = connection.Submit(context.Background(), "*notepadish")
	if err != nil || !strings.Contains(output, "아직 구현되지 않은 명령") || store.commits != 5 {
		t.Fatalf("unsupported output=%q err=%v commits=%d", output, err, store.commits)
	}
}

func TestWorldConnectorNotepadMalformedContinuationAndAuthorizationFailClosed(t *testing.T) {
	initial := notepadConnectorFixture(10)
	store := &connectorCommandStore{}
	connection := newNotepadConnection(t, initial, store)
	output, err := connection.Submit(context.Background(), "*notepad a")
	if err != nil || output != world.NotepadAppendPrompt {
		t.Fatalf("start output=%q err=%v", output, err)
	}
	output, err = connection.Submit(context.Background(), "bad\x00line")
	if err != nil || output != session.NotepadAppendInvalidLineResponse || connection.notepad == nil || store.commits != 0 {
		t.Fatalf("malformed output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	output, err = connection.Submit(context.Background(), "*notepad d")
	if err != nil || output != world.NotepadAppendContinuePrompt || len(connection.notepad.lines) != 1 || store.commits != 0 {
		t.Fatalf("command-shaped body output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	output, err = connection.Submit(context.Background(), ".")
	if err != nil || output != world.NotepadAppendResponse || store.commits != 1 || connection.notepad != nil {
		t.Fatalf("empty/command body commit output=%q err=%v commits=%d draft=%+v", output, err, store.commits, connection.notepad)
	}

	unauthorizedStore := &connectorCommandStore{}
	unauthorized := newNotepadConnection(t, notepadConnectorFixture(9), unauthorizedStore)
	output, err = unauthorized.Submit(context.Background(), "*notepad a")
	if err != nil || output != world.NotepadUnknownResponse("*notepad") || unauthorized.notepad != nil || unauthorizedStore.commits != 0 {
		t.Fatalf("unauthorized output=%q err=%v draft=%+v commits=%d", output, err, unauthorized.notepad, unauthorizedStore.commits)
	}
}

type notepadFlakyStore struct {
	mu       sync.Mutex
	state    json.RawMessage
	receipt  *storage.WorldReceipt
	command  string
	commits  int
	failOnce bool
}

func (s *notepadFlakyStore) ReadWorldReceipt(_ context.Context, _ string, command string, _ json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil || s.command != command {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	receipt := *s.receipt
	receipt.Replayed = true
	return receipt, nil
}

func (s *notepadFlakyStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *notepadFlakyStore) CommitWorldCommand(_ context.Context, _ string, command string, _ json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits++
	if s.failOnce {
		s.failOnce = false
		return storage.WorldReceipt{}, errors.New("commit response lost")
	}
	s.state = append(json.RawMessage(nil), state...)
	s.command = command
	s.receipt = &storage.WorldReceipt{Revision: revision + 1, Response: append(json.RawMessage(nil), response...)}
	return *s.receipt, nil
}

func TestWorldConnectorNotepadRetriesSameDurableBoundaryAfterLostCommitResponse(t *testing.T) {
	initial := notepadConnectorFixture(10)
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &notepadFlakyStore{state: raw, failOnce: true}
	connection := newNotepadConnection(t, initial, store)
	if output, err := connection.Submit(context.Background(), "*notepad a"); err != nil || output != world.NotepadAppendPrompt {
		t.Fatalf("start output=%q err=%v", output, err)
	}
	if output, err := connection.Submit(context.Background(), "replay-safe"); err != nil || output != world.NotepadAppendContinuePrompt {
		t.Fatalf("body output=%q err=%v", output, err)
	}
	output, err := connection.Submit(context.Background(), ".")
	if err != nil || output != session.NotepadAppendRetryResponse || connection.notepad == nil || store.commits != 1 {
		t.Fatalf("lost response output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	output, err = connection.Submit(context.Background(), "must-not-become-a-second-line")
	if err != nil || output != session.NotepadAppendRetryResponse || len(connection.notepad.lines) != 1 || store.commits != 1 {
		t.Fatalf("retry gate output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	output, err = connection.Submit(context.Background(), ".")
	if err != nil || output != world.NotepadAppendResponse || connection.notepad != nil || store.commits != 2 {
		t.Fatalf("replay output=%q err=%v draft=%+v commits=%d", output, err, connection.notepad, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Notepad) != 3 || saved.Notepad[2] != "replay-safe" {
		t.Fatalf("saved=%+v err=%v", saved.Notepad, err)
	}
}
