package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/transport"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestProcessWorldSignupRestartAndSignalDrain(t *testing.T) {
	testProcessRestart(t, false)
}

func TestProcessWorldRecoversAfterSIGKILL(t *testing.T) {
	testProcessRestart(t, true)
}

func testProcessRestart(t *testing.T, crash bool) {
	t.Helper()
	worldID, name := "process", "Alice"
	if crash {
		worldID, name = "process-crash", "Crashalice"
	}
	dsn := os.Getenv("MUHAN_PROCESS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pg := storage.NewPostgres(db)
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	initial := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}, Players: map[string]world.PlayerState{}}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err = pg.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "muhan")
	if out, err := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %s %v", out, err)
	}
	templates := t.TempDir() // fixture has no spawn records; no template reads expected
	var characterID string
	for round := 0; round < 2; round++ {
		func() {
			cmd := exec.CommandContext(ctx, binary, "-world", worldID, "-templates", templates, "-game-hour", "12", "-npc-combat-tick", "1s")
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "DATABASE_URL=") && !strings.HasPrefix(value, "ALLOWED_ORIGINS=") && !strings.HasPrefix(value, "LISTEN_ADDR=") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			cmd.Env = append(cmd.Env, "DATABASE_URL="+dsn, "ALLOWED_ORIGINS=https://mud.test", "LISTEN_ADDR=127.0.0.1:0")
			pipe, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			stopped := false
			defer func() {
				if !stopped {
					_ = cmd.Process.Kill()
					<-exited
				}
			}()
			address := make(chan string, 1)
			schedulerStarted := make(chan struct{})
			scanned := make(chan struct{})
			go func() {
				defer close(scanned)
				scanner := bufio.NewScanner(pipe)
				for scanner.Scan() {
					line := scanner.Text()
					const marker = "Go terminal server listening on "
					if i := strings.Index(line, marker); i >= 0 {
						select {
						case address <- strings.Fields(line[i+len(marker):])[0]:
						default:
						}
					}
					if strings.Contains(line, "NPC combat scheduler started") {
						select {
						case <-schedulerStarted:
						default:
							close(schedulerStarted)
						}
					}
				}
			}()
			var addr string
			select {
			case addr = <-address:
			case err := <-exited:
				stopped = true
				t.Fatalf("server failed before listening: %v", err)
			case <-ctx.Done():
				t.Fatal("startup timeout")
			}
			select {
			case <-schedulerStarted:
			case <-ctx.Done():
				t.Fatal("NPC combat scheduler did not start")
			}
			conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			var output transport.Output
			if err = wsjson.Read(ctx, conn, &output); err != nil {
				t.Fatal(err)
			}
			// Read authoritative state after listening but BEFORE logging in again.
			if round == 1 {
				snapshot, err := pg.LoadWorld(ctx, worldID)
				if err != nil {
					t.Fatal(err)
				}
				recovered, err := world.DecodeState(snapshot.State)
				if err != nil {
					t.Fatal(err)
				}
				if recovered.Players[characterID].Online || len(recovered.Rooms[1].PlayerIDs) != 0 {
					t.Fatal("listener opened before cold-start recovery")
				}
			}
			lines := []string{name, "pw1234", "봐"}
			if round == 0 {
				lines = []string{name, "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7", "pw1234", "봐"}
			}
			for _, line := range lines {
				if line == "pw1234" && !output.Secret {
					t.Fatal("password not secret")
				}
				if err = wsjson.Write(ctx, conn, transport.Input{Type: "line", Text: line}); err != nil {
					t.Fatal(err)
				}
				if err = wsjson.Read(ctx, conn, &output); err != nil {
					t.Fatal(err)
				}
				if output.Closed || strings.Contains(output.Text, "pw1234") {
					t.Fatal("bad terminal response")
				}
				if line == "봐" && !strings.Contains(output.Text, "광장") {
					t.Fatal("game scene missing")
				}
			}
			signal := syscall.SIGTERM
			if crash && round == 0 {
				signal = syscall.SIGKILL
			}
			if err = cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-exited:
				stopped = true
				if crash && round == 0 {
					var exitError *exec.ExitError
					if !errors.As(err, &exitError) {
						t.Fatalf("expected killed process: %v", err)
					}
					status, ok := exitError.Sys().(syscall.WaitStatus)
					if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
						t.Fatalf("unexpected exit status: %v", err)
					}
				} else if err != nil {
					t.Fatalf("signal shutdown: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("shutdown timeout")
			}
			<-scanned
			identity, err := pg.Authenticate(ctx, name, []byte("pw1234"))
			if err != nil {
				t.Fatal(err)
			}
			if round == 0 {
				characterID = identity.ID
			}
			if identity.ID != characterID {
				t.Fatal("restart changed identity")
			}
			saved, err := pg.LoadWorld(ctx, worldID)
			if err != nil {
				t.Fatal(err)
			}
			s, err := world.DecodeState(saved.State)
			if err != nil {
				t.Fatal(err)
			}
			p := s.Players[characterID]
			wantRevision := int64(5 + round*4)
			wantOnline := false
			occupants := 0
			if crash {
				wantRevision--
				if round == 0 {
					wantOnline = true
					occupants = 1
				}
			}
			if saved.Revision != wantRevision || p.Online != wantOnline || p.Body.HPCurrent != 56 || p.Body.Gold != 500 || len(s.Rooms[1].PlayerIDs) != occupants {
				t.Fatalf("process exited before persistence: rev%d %+v", saved.Revision, p)
			}
		}()
	}
}
