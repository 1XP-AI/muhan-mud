package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// MailSendPrompt is emitted only while the connection-local post editor is
// active. Intermediate editor lines never cross the receipt boundary.
const MailSendPrompt = "편지 내용을 입력하십시요. 문장처음에 [.]을 치시면 편지쓰기를 종료합니다.\n각 행은 80자를 넘길 수 없습니다.\n-: "

// MailSendContinuePrompt mirrors postedit's line prompt. It deliberately does
// not include any user-provided content.
const MailSendContinuePrompt = ": "

// BoardWriteTitlePrompt is emitted after `써` starts the connection-local
// title phase.
const BoardWriteTitlePrompt = "제목: "

// BoardWriteBodyPrompt is emitted after a valid title. The line number is
// supplied separately so the connector can keep the prompt deterministic.
const BoardWriteBodyPrompt = "게시물을 작성합니다. 끝내시려면 행의 처음에 [.]을 입력하십시요.\n중간에 취소하시려면 행의 처음에 [!!]를 입력하십시요.\n\n"

const (
	BoardWriteCancelResponse       = "게시물 작성을 취소합니다.\r\n"
	MailSendInvalidLineResponse    = "편지 내용을 확인해 주세요.\n-: "
	BoardWriteInvalidTitleResponse = "게시판 제목을 확인해 주세요.\r\n제목: "
	BoardWriteInvalidLineResponse  = "게시판 본문을 확인해 주세요.\r\n"
)

var (
	ErrInvalidMailSendStart   = errors.New("invalid mail send command")
	ErrInvalidBoardWriteStart = errors.New("invalid board write command")
)

// MailSendCommand is the non-interactive start line. Recipient is retained as
// a legacy display-name token only long enough for the transport adapter to
// resolve it against the canonical world snapshot; the durable reducer
// receives the canonical character ID.
type MailSendCommand struct {
	Recipient string
}

// BoardWriteCommand identifies the bare legacy `써` start command.
type BoardWriteCommand struct{}

func validComposeLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseMailSendLine accepts the original bare prompt and its one-recipient
// form. A malformed extra-token line is deliberately rejected before the
// connector can enter a continuation.
func ParseMailSendLine(line string) (MailSendCommand, bool) {
	if !validComposeLine(line) {
		return MailSendCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || tokens[0] != "편지보내기" || len(tokens) > 2 {
		return MailSendCommand{}, false
	}
	if len(tokens) == 1 {
		return MailSendCommand{}, true
	}
	// Character names are single legacy command tokens; an empty or
	// whitespace-only token is impossible after tokenization.
	return MailSendCommand{Recipient: tokens[1]}, true
}

// IsMailSendLine reports whether the line is a valid compose start, including
// the no-recipient form which produces the original explanatory response.
func IsMailSendLine(line string) bool {
	_, ok := ParseMailSendLine(line)
	return ok
}

// ParseBoardWriteLine accepts only the bare `써` command. The title and body
// are consumed by the connection-local continuation, not by ParseCommand's
// ordinary token parser.
func ParseBoardWriteLine(line string) (BoardWriteCommand, bool) {
	if !validComposeLine(line) || strings.TrimSpace(line) != "써" {
		return BoardWriteCommand{}, false
	}
	return BoardWriteCommand{}, true
}

func IsBoardWriteLine(line string) bool {
	_, ok := ParseBoardWriteLine(line)
	return ok
}

type mailSendRequest struct {
	Kind        string    `json:"kind"`
	RecipientID string    `json:"recipient_id"`
	MessageID   string    `json:"message_id,omitempty"`
	Body        string    `json:"body"`
	Timestamp   time.Time `json:"timestamp"`
}

type boardWriteRequest struct {
	Kind      string                  `json:"kind"`
	Payload   world.BoardWritePayload `json:"payload"`
	Timestamp time.Time               `json:"timestamp"`
}

// MailSendOptions separates the one-time ID allocation from the receipt
// reducer. A continuation can retain MessageID after an uncertain commit and
// retry the exact same request; the allocator is then never called again.
type MailSendOptions struct {
	MessageID string
	Allocate  world.MailMessageIDAllocator
}

// ExecuteMailSend commits one already-collected editor buffer. The caller
// owns commandID and must reuse it if the commit outcome is uncertain. The
// reducer derives the sender from lease.ActorID and rechecks the recipient,
// room, mailbox migration, capacity and message ID invariants against the
// state loaded for this receipt.
func (o *Ownership) ExecuteMailSend(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, payload world.MailSendPayload, allocate world.MailMessageIDAllocator) (storage.WorldReceipt, error) {
	return o.ExecuteMailSendWithOptions(ctx, store, worldID, commandID, lease, payload, MailSendOptions{Allocate: allocate})
}

// ExecuteMailSendWithOptions is the retry-safe form used by the interactive
// terminal continuation. MessageID is placed in the durable request and the
// reducer's allocator closure returns that exact value, so a retry after an
// uncertain store outcome cannot create a second mail identity.
func (o *Ownership) ExecuteMailSendWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, payload world.MailSendPayload, options MailSendOptions) (storage.WorldReceipt, error) {
	canonicalPayload := payload
	messageID := options.MessageID
	allocate := options.Allocate
	if messageID != "" {
		allocate = func() (string, error) { return messageID, nil }
	}
	request, err := json.Marshal(mailSendRequest{
		Kind:        "mail-send",
		RecipientID: canonicalPayload.RecipientID,
		MessageID:   messageID,
		Body:        canonicalPayload.Body,
		Timestamp:   canonicalPayload.Timestamp,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanMailSend(actorID, canonicalPayload, allocate)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyMailSend(proposal)
		if err != nil {
			return nil, nil, err
		}
		stateBytes, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return stateBytes, response, err
	})
}

// ExecuteBoardWrite commits one already-collected title/body buffer. The
// board object and author are still resolved by the world reducer; Payload's
// BoardID is only a consistency assertion captured by the continuation.
func (o *Ownership) ExecuteBoardWrite(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, payload world.BoardWritePayload, now time.Time) (storage.WorldReceipt, error) {
	request, err := json.Marshal(boardWriteRequest{Kind: "board-write", Payload: payload, Timestamp: now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		var command boardWriteRequest
		if err := json.Unmarshal(request, &command); err != nil || command.Kind != "board-write" {
			return nil, nil, ErrInvalidBoardWriteStart
		}
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanBoardWriteWithOptions(actorID, command.Payload, world.BoardWriteOptions{Now: command.Timestamp})
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyBoardWrite(proposal)
		if err != nil {
			return nil, nil, err
		}
		stateBytes, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return stateBytes, response, err
	})
}
