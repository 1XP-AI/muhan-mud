package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// This is deliberately opt-in: the caller supplies a disposable ARM64
// postgres:17-alpine URL. The test never creates, drops, or cleans a database
// outside that URL, so a normal local `go test` remains safe and fast.
func composePostgresTestDB(t *testing.T) (*sql.DB, *storage.Postgres, context.Context) {
	t.Helper()
	dsn := os.Getenv("MUHAN_MAIL_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required; set MUHAN_MAIL_BOARD_TEST_DATABASE_URL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db, store, ctx
}

func composePGMailState(t *testing.T) json.RawMessage {
	t.Helper()
	var flags [8]byte
	flags[world.RoomPostOfficeFlag/8] |= 1 << (world.RoomPostOfficeFlag % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "우체국", Flags: flags}}, PlayerIDs: []string{"sender"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "광장"}}},
		},
		Players: map[string]world.PlayerState{
			"sender":    {Body: world.LegacyMonster{Name: "Sender", Type: 0, RoomID: 1}, Online: true},
			"recipient": {Body: world.LegacyMonster{Name: "Recipient", Type: 0, RoomID: 2}, Online: false},
		},
		Mailboxes: map[string][]world.MailMessage{},
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

func composePGBoardState(t *testing.T) json.RawMessage {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "게시판방"}},
			Items: &world.ItemCollection{
				Items:     map[string]world.Item{"board": {Object: world.LegacyObject{Name: "게시판", Type: 100, Special: world.BoardSpecial}}},
				Inventory: []string{"board"},
			},
			PlayerIDs: []string{"sender", "observer"},
		}},
		Players: map[string]world.PlayerState{
			"sender":   {Body: world.LegacyMonster{Name: "Sender", Type: 0, RoomID: 1}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1}, Online: true},
		},
		Boards: &world.BoardState{Boards: map[int]world.Board{100: {Posts: nil}}},
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

func createComposePGWorld(t *testing.T, store *storage.Postgres, ctx context.Context, prefix string, state json.RawMessage) string {
	t.Helper()
	worldID := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, state); err != nil {
		t.Fatal(err)
	}
	return worldID
}

func addComposePGConnection(t *testing.T, connector *WorldConnector, actorID string, events bool) *worldConnection {
	t.Helper()
	lease, err := connector.owners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	var eventCh chan string
	if events {
		eventCh = make(chan string, 8)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true, events: eventCh}
	connector.mu.Lock()
	connector.connections[connection] = struct{}{}
	connector.mu.Unlock()
	t.Cleanup(func() {
		connector.mu.Lock()
		delete(connector.connections, connection)
		connector.mu.Unlock()
		_ = connector.owners.Release(lease)
	})
	return connection
}

func newComposePGConnector(t *testing.T, store engine.CommandStore, worldID string, allocate func() (string, error)) *WorldConnector {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 },
		MaxSessions: 4, Allocate: allocate,
	})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func assertComposePGReceiptHasNoBody(t *testing.T, ctx context.Context, db *sql.DB, worldID, commandID, body string) {
	t.Helper()
	var response string
	var requestHash []byte
	if err := db.QueryRowContext(ctx, `SELECT response::text,request_hash FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, commandID).Scan(&response, &requestHash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response, body) {
		t.Fatalf("private mail body leaked into durable response: %q", response)
	}
	if len(requestHash) != 32 {
		t.Fatalf("request hash length=%d", len(requestHash))
	}
}

func TestPostgresWorldConnectorComposePersistReplayPrivacyAndRetry(t *testing.T) {
	db, store, ctx := composePostgresTestDB(t)

	t.Run("mail commit replay and private body", func(t *testing.T) {
		worldID := createComposePGWorld(t, store, ctx, "transport-mail-compose-pg", composePGMailState(t))
		const messageID = "transport-mail-compose-message-1"
		const bodyLine = "PG_PRIVATE_MAIL_BODY"
		connector := newComposePGConnector(t, store, worldID, func() (string, error) { return messageID, nil })
		connection := addComposePGConnection(t, connector, "sender", false)
		if output, err := connection.Submit(ctx, "편지보내기 Recipient"); err != nil || !strings.Contains(output, "편지 내용을 입력") {
			t.Fatalf("start=%q err=%v", output, err)
		}
		if output, err := connection.Submit(ctx, bodyLine); err != nil || output != ": " {
			t.Fatalf("body prompt=%q err=%v", output, err)
		}
		commandID := connection.compose.commandID
		if commandID == "" {
			t.Fatal("compose command ID missing")
		}
		if output, err := connection.Submit(ctx, "."); err != nil || output != world.MailSendResponse {
			t.Fatalf("finish=%q err=%v", output, err)
		}
		if connection.compose != nil {
			t.Fatal("mail draft retained after commit")
		}
		// The compose editor sends its line buffer without the terminal's
		// canonical trailing LF; the reducer adds that LF before persistence.
		// Replay must use the exact pre-canonicalization request bytes.
		payload := world.MailSendPayload{RecipientID: "recipient", Body: bodyLine, Timestamp: time.Unix(100, 0).UTC()}
		replay, err := connector.owners.ExecuteMailSendWithOptions(ctx, store, worldID, commandID, connection.lease, payload, session.MailSendOptions{MessageID: messageID})
		if err != nil || !replay.Replayed || replay.Revision != 1 {
			t.Fatalf("mail replay=%+v err=%v", replay, err)
		}
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := world.DecodeState(snapshot.State)
		if err != nil || snapshot.Revision != 1 || len(saved.Mailboxes["recipient"]) != 1 || saved.Mailboxes["recipient"][0].Body != bodyLine+"\n" {
			t.Fatalf("saved revision=%d mailbox=%v err=%v", snapshot.Revision, saved.Mailboxes, err)
		}
		assertComposePGReceiptHasNoBody(t, ctx, db, worldID, commandID, bodyLine)
	})

	t.Run("mail transient retry keeps command identity", func(t *testing.T) {
		worldID := createComposePGWorld(t, store, ctx, "transport-mail-retry-pg", composePGMailState(t))
		const messageID = "transport-mail-retry-message-1"
		const bodyLine = "PG_TRANSIENT_PRIVATE_BODY"
		flaky := &composePGTransientStore{base: store, failNext: true}
		connector := newComposePGConnector(t, flaky, worldID, func() (string, error) { return messageID, nil })
		connection := addComposePGConnection(t, connector, "sender", false)
		if _, err := connection.Submit(ctx, "편지보내기 Recipient"); err != nil {
			t.Fatal(err)
		}
		if _, err := connection.Submit(ctx, bodyLine); err != nil {
			t.Fatal(err)
		}
		commandID := connection.compose.commandID
		first, err := connection.Submit(ctx, ".")
		if err != nil || !strings.Contains(first, "다시 시도") || connection.compose == nil {
			t.Fatalf("first=%q err=%v draft=%+v", first, err, connection.compose)
		}
		second, err := connection.Submit(ctx, ".")
		if err != nil || second != world.MailSendResponse || connection.compose != nil {
			t.Fatalf("second=%q err=%v draft=%+v", second, err, connection.compose)
		}
		attempts := flaky.attemptSnapshot()
		if len(attempts) != 2 || attempts[0] != commandID || attempts[1] != commandID || flaky.successfulCommits() != 1 {
			t.Fatalf("attempts=%v commandID=%q successes=%d", attempts, commandID, flaky.successfulCommits())
		}
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := world.DecodeState(snapshot.State)
		if err != nil || snapshot.Revision != 1 || len(saved.Mailboxes["recipient"]) != 1 || saved.Mailboxes["recipient"][0].Body != bodyLine+"\n" {
			t.Fatalf("saved revision=%d mailbox=%v err=%v", snapshot.Revision, saved.Mailboxes, err)
		}
		assertComposePGReceiptHasNoBody(t, ctx, db, worldID, commandID, bodyLine)
	})

	t.Run("board commit replay and event", func(t *testing.T) {
		worldID := createComposePGWorld(t, store, ctx, "transport-board-compose-pg", composePGBoardState(t))
		connector := newComposePGConnector(t, store, worldID, nil)
		actor := addComposePGConnection(t, connector, "sender", true)
		observer := addComposePGConnection(t, connector, "observer", true)
		if output, err := actor.Submit(ctx, "써"); err != nil || output != session.BoardWriteTitlePrompt {
			t.Fatalf("start=%q err=%v", output, err)
		}
		const title = "PG 게시물 제목"
		const bodyLine = "PG_BOARD_PUBLIC_BODY"
		if output, err := actor.Submit(ctx, title); err != nil || !strings.Contains(output, "게시물을 작성합니다") {
			t.Fatalf("title=%q err=%v", output, err)
		}
		if output, err := actor.Submit(ctx, bodyLine); err != nil || output != "  2: " {
			t.Fatalf("body=%q err=%v", output, err)
		}
		commandID := actor.compose.commandID
		select {
		case event := <-observer.events:
			t.Fatalf("board event arrived before commit: %q", event)
		default:
		}
		if output, err := actor.Submit(ctx, "."); err != nil || output != world.BoardWriteResponse {
			t.Fatalf("finish=%q err=%v", output, err)
		}
		select {
		case event := <-observer.events:
			if event != "\nSender님이 게시판에 글을 씁니다.\r\n" || strings.Contains(event, bodyLine) {
				t.Fatalf("board event=%q", event)
			}
		default:
			t.Fatal("board event missing after commit")
		}
		payload := world.BoardWritePayload{BoardID: 100, Title: title, Body: bodyLine + "\n"}
		now := time.Unix(100, 0).UTC()
		replay, err := connector.owners.ExecuteBoardWrite(ctx, store, worldID, commandID, actor.lease, payload, now)
		if err != nil || !replay.Replayed || replay.Revision != 1 {
			t.Fatalf("board replay=%+v err=%v", replay, err)
		}
		select {
		case event := <-observer.events:
			t.Fatalf("board replay fanned out event=%q", event)
		default:
		}
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := world.DecodeState(snapshot.State)
		if err != nil || snapshot.Revision != 1 || saved.Boards == nil || len(saved.Boards.Boards[100].Posts) != 1 || saved.Boards.Boards[100].Posts[0].Body != bodyLine+"\n" {
			t.Fatalf("saved revision=%d boards=%v err=%v", snapshot.Revision, saved.Boards, err)
		}
	})

	t.Run("board transient retry keeps command identity", func(t *testing.T) {
		worldID := createComposePGWorld(t, store, ctx, "transport-board-retry-pg", composePGBoardState(t))
		flaky := &composePGTransientStore{base: store, failNext: true}
		connector := newComposePGConnector(t, flaky, worldID, nil)
		actor := addComposePGConnection(t, connector, "sender", false)
		if _, err := actor.Submit(ctx, "써"); err != nil {
			t.Fatal(err)
		}
		const title = "PG 재시도 제목"
		const bodyLine = "PG_BOARD_RETRY_BODY"
		if _, err := actor.Submit(ctx, title); err != nil {
			t.Fatal(err)
		}
		if _, err := actor.Submit(ctx, bodyLine); err != nil {
			t.Fatal(err)
		}
		commandID := actor.compose.commandID
		first, err := actor.Submit(ctx, ".")
		if err != nil || !strings.Contains(first, "다시 시도") || actor.compose == nil {
			t.Fatalf("first=%q err=%v draft=%+v", first, err, actor.compose)
		}
		second, err := actor.Submit(ctx, ".")
		if err != nil || second != world.BoardWriteResponse || actor.compose != nil {
			t.Fatalf("second=%q err=%v draft=%+v", second, err, actor.compose)
		}
		attempts := flaky.attemptSnapshot()
		if len(attempts) != 2 || attempts[0] != commandID || attempts[1] != commandID || flaky.successfulCommits() != 1 {
			t.Fatalf("attempts=%v commandID=%q successes=%d", attempts, commandID, flaky.successfulCommits())
		}
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := world.DecodeState(snapshot.State)
		if err != nil || snapshot.Revision != 1 || saved.Boards == nil || len(saved.Boards.Boards[100].Posts) != 1 || saved.Boards.Boards[100].Posts[0].Body != bodyLine+"\n" {
			t.Fatalf("saved revision=%d boards=%v err=%v", snapshot.Revision, saved.Boards, err)
		}
	})
}

type composePGTransientStore struct {
	base       engine.CommandStore
	mu         sync.Mutex
	failNext   bool
	attempts   []string
	successful int
}

func (s *composePGTransientStore) ReadWorldReceipt(ctx context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	return s.base.ReadWorldReceipt(ctx, worldID, commandID, request)
}

func (s *composePGTransientStore) LoadWorld(ctx context.Context, worldID string) (storage.WorldSnapshot, error) {
	return s.base.LoadWorld(ctx, worldID)
}

func (s *composePGTransientStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	s.attempts = append(s.attempts, commandID)
	if s.failNext {
		s.failNext = false
		s.mu.Unlock()
		return storage.WorldReceipt{}, errors.New("injected transient compose commit failure")
	}
	s.mu.Unlock()
	receipt, err := s.base.CommitWorldCommand(ctx, worldID, commandID, request, revision, state, response)
	if err == nil {
		s.mu.Lock()
		s.successful++
		s.mu.Unlock()
	}
	return receipt, err
}

func (s *composePGTransientStore) attemptSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.attempts...)
}

func (s *composePGTransientStore) successfulCommits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.successful
}

var _ engine.CommandStore = (*composePGTransientStore)(nil)
