package transport

import (
	"context"
	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type accountStub struct{}

func (accountStub) Exists(context.Context, string) (bool, error) { return true, nil }
func (accountStub) Register(context.Context, string, []byte, game.Creation) (string, error) {
	panic("unexpected registration")
}
func (accountStub) Authenticate(_ context.Context, _ string, p []byte) (storage.Character, error) {
	if string(p) != "pw1234" {
		return storage.Character{}, storage.ErrCredentials
	}
	return storage.Character{ID: "private-id"}, nil
}

func TestWebSocketLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(NewHandler(ctx, accountStub{}, []string{"https://mud.test"}))
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
	if !strings.Contains(view.Text, "이름") || view.Secret {
		t.Fatal("wrong initial view")
	}
	if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, &view); err != nil {
		t.Fatal(err)
	}
	if !view.Secret {
		t.Fatal("password echo not disabled")
	}
	if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: "pw1234"}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, &view); err != nil {
		t.Fatal(err)
	}
	if !view.Closed || strings.Contains(view.Text, "pw1234") || strings.Contains(view.Text, "private-id") {
		t.Fatal("unexpected completion output")
	}
}

func TestWrongOriginRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(NewHandler(ctx, accountStub{}, []string{"https://mud.test"}))
	defer server.Close()
	_, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://elsewhere.test"}}})
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatal("origin not rejected")
	}
}
