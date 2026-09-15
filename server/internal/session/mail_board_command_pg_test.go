package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestPostgresMailAndBoardCommandsPersistAndReplay is one opt-in persistence
// batch for the bounded social slices. Feature lanes use the in-memory receipt
// tests; an explicitly disposable PostgreSQL URL runs both domains together so
// the expensive database startup is not repeated for every command.
func TestPostgresMailAndBoardCommandsPersistAndReplay(t *testing.T) {
	dsn := os.Getenv("MUHAN_MAIL_BOARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("mail", func(t *testing.T) {
		worldID := fmt.Sprintf("mail-pg-%d", time.Now().UnixNano())
		if err := store.CreateWorld(ctx, worldID, mailCommandFixture(t, true)); err != nil {
			t.Fatal(err)
		}
		var owners Ownership
		lease, err := owners.Acquire("recipient")
		if err != nil {
			t.Fatal(err)
		}
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		first, err := owners.ExecuteMailLine(ctx, store, worldID, "mail-read-pg-1", lease, "편지받기")
		if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "hello") {
			t.Fatalf("mail read=%+v err=%v", first, err)
		}
		replay, err := owners.ExecuteMailLine(ctx, store, worldID, "mail-read-pg-1", lease, "편지받기")
		if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
			t.Fatalf("mail replay=%+v err=%v", replay, err)
		}
		deleted, err := owners.ExecuteMailLine(ctx, store, worldID, "mail-delete-pg-1", lease, "편지삭제")
		if err != nil || deleted.Replayed || deleted.Revision != 2 {
			t.Fatalf("mail delete=%+v err=%v", deleted, err)
		}
		state, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := world.DecodeState(state.State)
		if err != nil || len(decoded.Mailboxes["recipient"]) != 0 {
			t.Fatalf("mail state=%+v err=%v", decoded.Mailboxes, err)
		}
	})

	t.Run("mail send", func(t *testing.T) {
		worldID := fmt.Sprintf("mail-send-pg-%d", time.Now().UnixNano())
		if err := store.CreateWorld(ctx, worldID, mailSendCommandFixture(t)); err != nil {
			t.Fatal(err)
		}
		var owners Ownership
		lease, err := owners.Acquire("sender")
		if err != nil {
			t.Fatal(err)
		}
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		payload := world.MailSendPayload{
			RecipientID: "recipient",
			Body:        "PG 본문",
			Timestamp:   time.Date(2026, 9, 9, 10, 11, 12, 0, time.FixedZone("KST", 9*60*60)),
		}
		options := MailSendOptions{MessageID: "mail-pg-send-1"}
		first, err := owners.ExecuteMailSendWithOptions(ctx, store, worldID, "mail-send-pg-1", lease, payload, options)
		if err != nil || first.Replayed || first.Revision != 1 {
			t.Fatalf("mail send=%+v err=%v", first, err)
		}
		replay, err := owners.ExecuteMailSendWithOptions(ctx, store, worldID, "mail-send-pg-1", lease, payload, options)
		if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
			t.Fatalf("mail send replay=%+v err=%v", replay, err)
		}
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := world.DecodeState(snapshot.State)
		if err != nil || len(decoded.Mailboxes["recipient"]) != 1 || decoded.Mailboxes["recipient"][0].ID != "mail-pg-send-1" || decoded.Mailboxes["recipient"][0].Body != "PG 본문\n" {
			t.Fatalf("mail send state=%+v err=%v", decoded.Mailboxes, err)
		}
	})

	t.Run("board", func(t *testing.T) {
		worldID := fmt.Sprintf("board-pg-%d", time.Now().UnixNano())
		if err := store.CreateWorld(ctx, worldID, boardCommandFixture(t)); err != nil {
			t.Fatal(err)
		}
		var owners Ownership
		lease, err := owners.Acquire("alice")
		if err != nil {
			t.Fatal(err)
		}
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		list, err := owners.ExecuteBoardLine(ctx, store, worldID, "board-list-pg-1", lease, "게시판")
		if err != nil || list.Replayed || list.Revision != 1 || !strings.Contains(string(list.Response), "공지") {
			t.Fatalf("board list=%+v err=%v", list, err)
		}
		read, err := owners.ExecuteBoardLine(ctx, store, worldID, "board-read-pg-1", lease, "읽어 게시판 1")
		if err != nil || read.Replayed || read.Revision != 2 || !strings.Contains(string(read.Response), "본문") {
			t.Fatalf("board read=%+v err=%v", read, err)
		}
		replay, err := owners.ExecuteBoardLine(ctx, store, worldID, "board-read-pg-1", lease, "읽어 게시판 1")
		if err != nil || !replay.Replayed || replay.Revision != read.Revision || string(replay.Response) != string(read.Response) {
			t.Fatalf("board replay=%+v err=%v", replay, err)
		}
		state, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		var decoded world.State
		if err := json.Unmarshal(state.State, &decoded); err != nil || decoded.Boards == nil || decoded.Boards.Boards[100].Posts[0].ReadCount != 1 {
			t.Fatalf("board state=%+v err=%v", decoded.Boards, err)
		}
		if _, err := owners.ExecuteBoardLine(ctx, store, worldID, "board-delete-pg-1", lease, "글삭제 게시판 1"); err == nil {
			t.Fatal("non-author board delete accepted")
		}
	})

	t.Run("board write", func(t *testing.T) {
		worldID := fmt.Sprintf("board-write-pg-%d", time.Now().UnixNano())
		if err := store.CreateWorld(ctx, worldID, boardCommandFixture(t)); err != nil {
			t.Fatal(err)
		}
		var owners Ownership
		lease, err := owners.Acquire("alice")
		if err != nil {
			t.Fatal(err)
		}
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		payload := world.BoardWritePayload{BoardID: 100, Title: "PG 제목", Body: "PG 본문\n"}
		now := time.Date(2026, 9, 9, 10, 11, 12, 0, time.FixedZone("KST", 9*60*60))
		first, err := owners.ExecuteBoardWrite(ctx, store, worldID, "board-write-pg-1", lease, payload, now)
		if err != nil || first.Replayed || first.Revision != 1 {
			t.Fatalf("board write=%+v err=%v", first, err)
		}
		replay, err := owners.ExecuteBoardWrite(ctx, store, worldID, "board-write-pg-1", lease, payload, now)
		if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
			t.Fatalf("board write replay=%+v err=%v", replay, err)
		}
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := world.DecodeState(snapshot.State)
		if err != nil || decoded.Boards == nil || len(decoded.Boards.Boards[100].Posts) != 2 || decoded.Boards.Boards[100].Posts[1].Title != "PG 제목" || decoded.Boards.Boards[100].Posts[1].Body != "PG 본문\n" {
			t.Fatalf("board write state=%+v err=%v", decoded.Boards, err)
		}
	})
}

func mailSendCommandFixture(t *testing.T) []byte {
	t.Helper()
	var flags [8]byte
	flags[world.RoomPostOfficeFlag/8] |= 1 << (world.RoomPostOfficeFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: flags}}, PlayerIDs: []string{"sender"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]world.PlayerState{
			"sender":    {Body: world.LegacyMonster{Name: "보내는이", RoomID: 1}, Online: true},
			"recipient": {Body: world.LegacyMonster{Name: "받는이", RoomID: 2}, Online: false},
		},
		Mailboxes: map[string][]world.MailMessage{},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
