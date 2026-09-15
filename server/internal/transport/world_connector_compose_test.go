package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func composeConnection(t *testing.T, state world.State, config WorldConnectorConfig) (*WorldConnector, *worldConnection, *connectorCommandStore) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	config.Store = store
	if config.WorldID == "" {
		config.WorldID = "compose-world"
	}
	if config.Clock == nil {
		config.Clock = func() (int32, int) { return 100, 12 }
	}
	if config.MaxSessions == 0 {
		config.MaxSessions = 2
	}
	connector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("sender")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	return connector, connection, store
}

func composeMailStateForTransport() world.State {
	var flags [8]byte
	flags[world.RoomPostOfficeFlag/8] |= 1 << (world.RoomPostOfficeFlag % 8)
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: flags}},
			PlayerIDs: []string{"sender"},
		}},
		Players: map[string]world.PlayerState{
			"sender":    {Body: world.LegacyMonster{Name: "Sender", RoomID: 1}, Online: true},
			"recipient": {Body: world.LegacyMonster{Name: "Recipient", RoomID: 1}, Online: false},
		},
		Mailboxes: map[string][]world.MailMessage{},
	}
}

func composeBoardStateForTransport() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			Items: &world.ItemCollection{
				Items:     map[string]world.Item{"board": {Object: world.LegacyObject{Name: "게시판", Type: 100, Special: world.BoardSpecial}}},
				Inventory: []string{"board"},
			},
			PlayerIDs: []string{"sender", "observer"},
		}},
		Players: map[string]world.PlayerState{
			"sender":   {Body: world.LegacyMonster{Name: "Sender", RoomID: 1}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "Observer", RoomID: 1}, Online: true},
		},
		Boards: &world.BoardState{Boards: map[int]world.Board{100: {Posts: nil}}},
	}
}

func TestWorldConnectorMailComposeConsumesContinuationBeforeHistoryAndPersistsOnce(t *testing.T) {
	connector, connection, store := composeConnection(t, composeMailStateForTransport(), WorldConnectorConfig{
		Allocate: func() (string, error) { return "mail-compose-1", nil },
	})
	defer connection.Close(context.Background())

	start, err := connection.Submit(context.Background(), "편지보내기 Recipient")
	if err != nil || !strings.Contains(start, "편지 내용을 입력") || store.commits != 0 {
		t.Fatalf("start=%q err=%v commits=%d", start, err, store.commits)
	}
	// `!` is an ordinary body line while composing; it must not expand the
	// previous command or run an alias.
	if output, err := connection.Submit(context.Background(), "!본문"); err != nil || output != ": " {
		t.Fatalf("body output=%q err=%v", output, err)
	}
	if output, err := connection.Submit(context.Background(), "!!도 본문"); err != nil || output != ": " {
		t.Fatalf("bang body output=%q err=%v", output, err)
	}
	if output, err := connection.Submit(context.Background(), "."); err != nil || output != world.MailSendResponse || store.commits != 1 {
		t.Fatalf("finish output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Mailboxes["recipient"]) != 1 || saved.Mailboxes["recipient"][0].Body != "!본문\n!!도 본문\n" {
		t.Fatalf("saved mailboxes=%v err=%v", saved.Mailboxes, err)
	}
	if connection.compose != nil {
		t.Fatal("completed compose draft retained")
	}
	_ = connector
}

func TestWorldConnectorMailComposeChecksPostOfficeBeforeMissingRecipient(t *testing.T) {
	state := composeMailStateForTransport()
	room := state.Rooms[1]
	room.Resource.Flags = [8]byte{}
	state.Rooms[1] = room
	_, connection, store := composeConnection(t, state, WorldConnectorConfig{})
	if output, err := connection.Submit(context.Background(), "편지보내기"); err != nil || output != world.MailRoomResponse || connection.compose != nil || store.commits != 0 {
		t.Fatalf("output=%q err=%v compose=%+v commits=%d", output, err, connection.compose, store.commits)
	}
}

type composeTransientStore struct {
	base       *connectorCommandStore
	fail       bool
	attempts   []string
	commitSeen int
}

func (s *composeTransientStore) ReadWorldReceipt(ctx context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	return s.base.ReadWorldReceipt(ctx, worldID, commandID, request)
}

func (s *composeTransientStore) LoadWorld(ctx context.Context, worldID string) (storage.WorldSnapshot, error) {
	return s.base.LoadWorld(ctx, worldID)
}

func (s *composeTransientStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.attempts = append(s.attempts, commandID)
	if s.fail {
		s.fail = false
		return storage.WorldReceipt{}, errors.New("transient commit failure")
	}
	s.commitSeen++
	return s.base.CommitWorldCommand(ctx, worldID, commandID, request, revision, state, response)
}

func TestWorldConnectorMailComposeRetainsStableRequestAcrossCommitRetry(t *testing.T) {
	state := composeMailStateForTransport()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &composeTransientStore{base: base, fail: true}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "compose-retry-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
		Allocate: func() (string, error) { return "mail-retry-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("sender")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	if _, err := connection.Submit(context.Background(), "편지보내기 Recipient"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Submit(context.Background(), "본문"); err != nil {
		t.Fatal(err)
	}
	first, err := connection.Submit(context.Background(), ".")
	if err != nil || !strings.Contains(first, "다시 시도") || !connector.owners.Owns(lease) || connection.compose == nil {
		t.Fatalf("first=%q err=%v owns=%v draft=%+v", first, err, connector.owners.Owns(lease), connection.compose)
	}
	second, err := connection.Submit(context.Background(), ".")
	if err != nil || second != world.MailSendResponse || store.commitSeen != 1 || len(store.attempts) != 2 || store.attempts[0] != store.attempts[1] {
		t.Fatalf("second=%q err=%v seen=%d attempts=%q", second, err, store.commitSeen, store.attempts)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil || len(saved.Mailboxes["recipient"]) != 1 {
		t.Fatalf("saved=%v err=%v", saved.Mailboxes, err)
	}
}

func TestWorldConnectorBoardComposePersistsAndPublishesOnlyAfterCommit(t *testing.T) {
	connector, actor, store := composeConnection(t, composeBoardStateForTransport(), WorldConnectorConfig{})
	observerLease, err := connector.owners.Acquire("observer")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(observerLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	observer := &worldConnection{game: connector, lease: observerLease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.connections[observer] = struct{}{}
	connector.mu.Unlock()

	if output, err := actor.Submit(context.Background(), "써"); err != nil || output != "제목: " || store.commits != 0 {
		t.Fatalf("start=%q err=%v commits=%d", output, err, store.commits)
	}
	if output, err := actor.Submit(context.Background(), "새 글"); err != nil || !strings.Contains(output, "게시물을 작성합니다") || store.commits != 0 {
		t.Fatalf("title=%q err=%v commits=%d", output, err, store.commits)
	}
	if output, err := actor.Submit(context.Background(), "첫 줄"); err != nil || output != "  2: " || store.commits != 0 {
		t.Fatalf("body=%q err=%v commits=%d", output, err, store.commits)
	}
	if output, err := actor.Submit(context.Background(), ".후속 명령 금지"); err != nil || output != world.BoardWriteResponse || store.commits != 1 {
		t.Fatalf("finish=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Boards.Boards[100].Posts) != 1 || saved.Boards.Boards[100].Posts[0].Body != "첫 줄\n" {
		t.Fatalf("saved board=%v err=%v", saved.Boards, err)
	}
	select {
	case event := <-observer.events:
		if !strings.Contains(event, "Sender님이 게시판에 글을 씁니다") {
			t.Fatalf("event=%q", event)
		}
	default:
		t.Fatal("board write event not delivered")
	}
	if actor.compose != nil {
		t.Fatal("completed board draft retained")
	}
}

func TestWorldConnectorBoardComposeCancelsWithoutReceipt(t *testing.T) {
	_, actor, store := composeConnection(t, composeBoardStateForTransport(), WorldConnectorConfig{})
	if _, err := actor.Submit(context.Background(), "써"); err != nil {
		t.Fatal(err)
	}
	if output, err := actor.Submit(context.Background(), ""); err != nil || output != "게시물 작성을 취소합니다.\r\n" || store.commits != 0 {
		t.Fatalf("title cancel=%q err=%v commits=%d", output, err, store.commits)
	}
	if _, err := actor.Submit(context.Background(), "써"); err != nil {
		t.Fatal(err)
	}
	if _, err := actor.Submit(context.Background(), "제목"); err != nil {
		t.Fatal(err)
	}
	if output, err := actor.Submit(context.Background(), "!!"); err != nil || output != "게시물 작성을 취소합니다.\r\n" || store.commits != 0 {
		t.Fatalf("body cancel=%q err=%v commits=%d", output, err, store.commits)
	}
}

func TestWorldConnectorBoardComposeDocumentsEmptyBodyFailClosed(t *testing.T) {
	_, actor, store := composeConnection(t, composeBoardStateForTransport(), WorldConnectorConfig{})
	if _, err := actor.Submit(context.Background(), "써"); err != nil {
		t.Fatal(err)
	}
	if _, err := actor.Submit(context.Background(), "빈 본문 제목"); err != nil {
		t.Fatal(err)
	}
	output, err := actor.Submit(context.Background(), ".")
	if err != nil || !strings.Contains(output, "본문을 입력") || store.commits != 0 || actor.compose == nil {
		t.Fatalf("empty body output=%q err=%v commits=%d draft=%+v", output, err, store.commits, actor.compose)
	}
}

func TestWorldConnectorCloseDiscardsComposeDraft(t *testing.T) {
	_, connection, _ := composeConnection(t, composeMailStateForTransport(), WorldConnectorConfig{Allocate: func() (string, error) { return "mail-close", nil }})
	if _, err := connection.Submit(context.Background(), "편지보내기 Recipient"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Submit(context.Background(), "secret body"); err != nil {
		t.Fatal(err)
	}
	connection.Close(context.Background())
	if connection.compose != nil || connection.ready {
		t.Fatalf("compose=%+v ready=%v", connection.compose, connection.ready)
	}
	if _, err := connection.Submit(context.Background(), "x"); err == nil {
		t.Fatal("closed connection accepted input")
	}
}
