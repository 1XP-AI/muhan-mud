package transport

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// The client submits complete lines, not raw keystrokes. It waits for each
// response before sending the next line, preventing password-mode races.
type Input struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type Output struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Secret bool   `json:"secret"`
	Closed bool   `json:"closed"`
}

func NewHandler(lifetime context.Context, accounts session.Accounts, origins []string) http.Handler {
	return NewGameHandler(lifetime, accounts, origins, nil)
}

// GameConnection implementations own durable command IDs and unresolved cleanup
// retries. Close must retain ownership when a departure cannot be confirmed.
type GameConnection interface {
	Submit(context.Context, string) (string, error)
	Close(context.Context)
}
type EventSource interface {
	Events() <-chan string
}

// followerProjectionEventSource is the transport-only extension used for
// receipt-backed follower output. Unlike EventSource, its envelope carries
// the exact receipt key so the writer can acknowledge one recipient only
// after wsjson.Write succeeds.
type followerProjectionEventSource interface {
	followerProjectionEvents() <-chan followerProjectionDelivery
	followerProjectionWrite(followerProjectionDelivery)
}
type CloseAfterSubmit interface {
	ShouldClose() bool
}
type GameConnector interface {
	// Resolve the verified character into an initialized world character; never
	// admit a creation draft merely because credentials were verified.
	Open(context.Context, storage.Character) (GameConnection, string, error)
}

func NewGameHandler(lifetime context.Context, accounts session.Accounts, origins []string, games GameConnector) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		if origin != "" {
			allowed[origin] = true
		}
	}
	slots := make(chan struct{}, 32)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Header.Get("Origin")] {
			http.Error(w, "origin rejected", http.StatusForbidden)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "server busy", http.StatusServiceUnavailable)
			return
		}
		// Full origin (including scheme/port) was checked above, before upgrade.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(2048)
		ctx, cancel := context.WithTimeout(lifetime, 10*time.Minute)
		defer cancel()
		var writeMu sync.Mutex
		writeOutput := func(writeCtx context.Context, output Output) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			return wsjson.Write(writeCtx, conn, output)
		}
		var eventDone chan struct{}
		var eventWG sync.WaitGroup
		startEvents := func(game GameConnection) {
			source, hasEvents := game.(EventSource)
			projectionSource, hasProjections := game.(followerProjectionEventSource)
			if !hasEvents && !hasProjections {
				return
			}
			var events <-chan string
			if hasEvents {
				events = source.Events()
			}
			var projectionEvents <-chan followerProjectionDelivery
			if hasProjections {
				projectionEvents = projectionSource.followerProjectionEvents()
			}
			if events == nil && projectionEvents == nil {
				return
			}
			eventDone = make(chan struct{})
			if events != nil {
				eventWG.Add(1)
				go func() {
					defer eventWG.Done()
					for {
						select {
						case text, ok := <-events:
							if !ok {
								return
							}
							writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
							_ = writeOutput(writeCtx, Output{Type: "event", Text: text})
							writeCancel()
						case <-eventDone:
							return
						}
					}
				}()
			}
			if projectionEvents != nil {
				eventWG.Add(1)
				go func() {
					defer eventWG.Done()
					for {
						select {
						case delivery, ok := <-projectionEvents:
							if !ok {
								return
							}
							writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
							writeErr := writeOutput(writeCtx, Output{Type: "event", Text: delivery.text})
							writeCancel()
							if writeErr == nil {
								projectionSource.followerProjectionWrite(delivery)
							}
						case <-eventDone:
							return
						}
					}
				}()
			}
		}
		login := session.NewLogin(accounts)
		defer login.Close()
		view := login.View()
		var game GameConnection
		startEventsPending := false
		defer func() {
			if game != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				game.Close(cleanup)
			}
		}()
		// Stop and join event writers before closing the game connection. This
		// prevents a successful projection write from racing unregister/close and
		// makes the callback's wire boundary explicit.
		defer func() {
			if eventDone != nil {
				close(eventDone)
				eventWG.Wait()
			}
		}()
		for {
			output := Output{Type: "view", Text: view.Text, Secret: view.Secret, Closed: view.Closed}
			if view.Verified != nil {
				if games == nil {
					output.Text += "월드 연결은 아직 구현 중입니다. 캐릭터는 저장되어 있습니다.\r\n"
					output.Closed = true
				} else {
					openCtx, openCancel := context.WithTimeout(ctx, 10*time.Second)
					var scene string
					game, scene, err = games.Open(openCtx, *view.Verified)
					openCancel()
					if err != nil || game == nil {
						output.Text = "게임 입장을 완료하지 못했습니다. 잠시 후 다시 접속해 주세요.\r\n"
						output.Closed = true
					} else {
						output.Text = scene
						output.Secret = false
						output.Closed = false
						startEventsPending = true
					}
				}
				view.Verified = nil
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
			err := writeOutput(writeCtx, output)
			writeCancel()
			if err != nil {
				return
			}
			if startEventsPending && game != nil {
				startEvents(game)
				startEventsPending = false
			}
			if output.Closed {
				conn.Close(websocket.StatusNormalClosure, "session complete")
				return
			}
			var input Input
			readCtx, readCancel := context.WithTimeout(ctx, 2*time.Minute)
			err = wsjson.Read(readCtx, conn, &input)
			readCancel()
			if err != nil {
				return
			}
			if input.Type != "line" || len(input.Text) > 512 || !utf8.ValidString(input.Text) || strings.ContainsAny(input.Text, "\r\n\x00") {
				conn.Close(websocket.StatusPolicyViolation, "invalid line")
				return
			}
			callCtx, callCancel := context.WithTimeout(ctx, 10*time.Second)
			if game == nil {
				view = login.Submit(callCtx, input.Text)
			} else {
				text, callErr := game.Submit(callCtx, input.Text)
				if callErr != nil {
					view = session.View{Text: "명령 처리를 확인하지 못했습니다. 다시 접속해 주세요.\r\n", Closed: true}
				} else {
					secret := false
					if source, ok := game.(SecretPromptSource); ok {
						secret = source.InputIsSecret()
					}
					view = session.View{Text: text, Secret: secret}
					if closeAfter, ok := game.(CloseAfterSubmit); ok && closeAfter.ShouldClose() {
						view.Closed = true
					}
				}
			}
			callCancel()
			input.Text = ""
		}
	})
}
