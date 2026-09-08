package transport

import (
	"context"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type gameStub struct {
	closed chan bool
	actor  chan string
}

type eventGameStub struct {
	events chan string
}

func (g *eventGameStub) Open(_ context.Context, _ storage.Character) (GameConnection, string, error) {
	return g, "광장", nil
}
func (g *eventGameStub) Submit(_ context.Context, line string) (string, error) {
	return "명령: " + line, nil
}
func (g *eventGameStub) Close(context.Context) {}
func (g *eventGameStub) Events() <-chan string { return g.events }

func (g *gameStub) Open(_ context.Context, c storage.Character) (GameConnection, string, error) {
	g.actor <- c.ID
	return g, "광장", nil
}
func (g *gameStub) Submit(_ context.Context, line string) (string, error) {
	return "명령: " + line, nil
}
func (g *gameStub) Close(ctx context.Context) { g.closed <- ctx.Err() == nil }

func TestWebSocketTransitionsToGameAndCleansUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	game := &gameStub{closed: make(chan bool, 1), actor: make(chan string, 1)}
	server := httptest.NewServer(NewGameHandler(ctx, accountStub{}, []string{"https://mud.test"}, game))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var view Output
	if err := wsjson.Read(ctx, conn, &view); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Alice", "pw1234", "봐"} {
		if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: line}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(ctx, conn, &view); err != nil {
			t.Fatal(err)
		}
		if line == "pw1234" && (view.Closed || view.Secret || view.Text != "광장") {
			t.Fatalf("%+v", view)
		}
	}
	if view.Text != "명령: 봐" || view.Secret || view.Closed {
		t.Fatalf("%+v", view)
	}
	if id := <-game.actor; id != "private-id" {
		t.Fatal("unverified actor")
	}
	conn.CloseNow()
	select {
	case fresh := <-game.closed:
		if !fresh {
			t.Fatal("cleanup context already canceled")
		}
	case <-ctx.Done():
		t.Fatal("cleanup not called")
	}
}

func TestWebSocketForwardsAsynchronousRoomEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := &eventGameStub{events: make(chan string, 1)}
	server := httptest.NewServer(NewGameHandler(ctx, accountStub{}, []string{"https://mud.test"}, events))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var output Output
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Alice", "pw1234"} {
		if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: line}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(ctx, conn, &output); err != nil {
			t.Fatal(err)
		}
	}
	events.events <- "\n다른 모험가가 도착했습니다.\r\n"
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	if output.Type != "event" || !strings.Contains(output.Text, "다른 모험가") || output.Secret || output.Closed {
		t.Fatalf("unexpected async event=%+v", output)
	}
}
