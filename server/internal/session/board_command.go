package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedBoardLine = errors.New("line is not an implemented board command")

type BoardCommandAction string

const (
	BoardList   BoardCommandAction = "list"
	BoardRead   BoardCommandAction = "read"
	BoardDelete BoardCommandAction = "delete"
)

type BoardCommand struct {
	Action     BoardCommandAction
	PostNumber int
}

type boardLineRequest struct {
	Kind       string             `json:"kind"`
	Line       string             `json:"line"`
	Action     BoardCommandAction `json:"action"`
	PostNumber int                `json:"post_number,omitempty"`
}

func validBoardCommandLine(line string) bool {
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

// ParseBoardLine admits the non-interactive portion of board.c. `써` remains
// outside this slice because the legacy multi-line editor is a separate
// connection continuation protocol.
func ParseBoardLine(line string) (BoardCommand, bool) {
	if !validBoardCommandLine(line) {
		return BoardCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil {
		return BoardCommand{}, false
	}
	switch {
	case len(tokens) == 1 && tokens[0] == "게시판":
		return BoardCommand{Action: BoardList}, true
	case len(tokens) == 2 && tokens[0] == "게시판":
		n, err := strconv.Atoi(tokens[1])
		if err != nil || n < 1 {
			return BoardCommand{}, false
		}
		return BoardCommand{Action: BoardRead, PostNumber: n}, true
	case len(tokens) == 3 && tokens[0] == "읽어" && tokens[1] == "게시판":
		n, err := strconv.Atoi(tokens[2])
		if err != nil || n < 1 {
			return BoardCommand{}, false
		}
		return BoardCommand{Action: BoardRead, PostNumber: n}, true
	case len(tokens) == 3 && tokens[0] == "글삭제" && tokens[1] == "게시판":
		n, err := strconv.Atoi(tokens[2])
		if err != nil || n < 1 {
			return BoardCommand{}, false
		}
		return BoardCommand{Action: BoardDelete, PostNumber: n}, true
	default:
		return BoardCommand{}, false
	}
}

func IsBoardLine(line string) bool {
	_, ok := ParseBoardLine(line)
	return ok
}

func boardListText(boardID int, posts []world.BoardPost) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\n게시판 %d\n 번호 올린이       날짜  줄수 조회 제목\n", boardID)
	out.WriteString("------------------------------------- --------------------------------------\n")
	for _, post := range posts {
		lines := strings.Count(post.Body, "\n")
		if lines == 0 {
			lines = 1
		}
		fmt.Fprintf(&out, " %4d %-12s %02d/%02d %4d %4d %-40s\n", post.Number, post.Author, post.CreatedAt.Month(), post.CreatedAt.Day(), lines, post.ReadCount, post.Title)
	}
	if len(posts) == 0 {
		out.WriteString("등록된 게시물이 없습니다.\n")
	}
	return out.String()
}

func boardReadText(post world.BoardPost) string {
	lines := strings.Count(post.Body, "\n")
	if lines == 0 {
		lines = 1
	}
	return fmt.Sprintf("\n번호: %d  올린이: %s  제목: %s\n올린날: %s  총줄수: %d  읽은횟수: %d\n---------------------------------------------------------------\n%s", post.Number, post.Author, post.Title, post.CreatedAt.Format("2006-01-02 15:04"), lines, post.ReadCount, post.Body)
}

func boardDeleteText(deleted bool) string {
	if deleted {
		return "게시물이 삭제되었습니다.\r\n"
	}
	return "삭제된 게시물을 복구하였습니다.\r\n"
}

// ExecuteBoardLine connects list/read/delete to one durable receipt. The
// board object and board ID are always resolved from the canonical room
// snapshot; a terminal line cannot select another board directly.
func (o *Ownership) ExecuteBoardLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseBoardLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedBoardLine
	}
	request, err := json.Marshal(boardLineRequest{Kind: "board", Line: line, Action: command.Action, PostNumber: command.PostNumber})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		boards, actor, boardID, err := state.BoardContext(actorID)
		if err != nil {
			return nil, nil, err
		}
		admin := actor.Body.Class >= 12
		switch command.Action {
		case BoardList:
			posts, err := boards.List(boardID, admin)
			if err != nil {
				return nil, nil, err
			}
			response, err := json.Marshal(boardListText(boardID, posts))
			return raw, response, err
		case BoardRead:
			proposal, err := boards.PlanRead(boardID, command.PostNumber, actor.Body.Name, admin)
			if err != nil {
				return nil, nil, err
			}
			nextBoards, result, err := boards.ApplyRead(proposal)
			if err != nil {
				return nil, nil, err
			}
			next, err := state.WithBoards(nextBoards)
			if err != nil {
				return nil, nil, err
			}
			// board.c prints the index before incrementing readnum, then writes
			// the updated counter. Keep the actor response on that pre-read
			// snapshot while the committed State carries the increment.
			response, err := json.Marshal(boardReadText(result.Post))
			if err != nil {
				return nil, nil, err
			}
			stateBytes, err := json.Marshal(next)
			return stateBytes, response, err
		case BoardDelete:
			proposal, err := boards.PlanDelete(boardID, command.PostNumber, actor.Body.Name, admin)
			if err != nil {
				return nil, nil, err
			}
			nextBoards, result, err := boards.ApplyDelete(proposal)
			if err != nil {
				return nil, nil, err
			}
			next, err := state.WithBoards(nextBoards)
			if err != nil {
				return nil, nil, err
			}
			response, err := json.Marshal(boardDeleteText(result.Deleted))
			if err != nil {
				return nil, nil, err
			}
			stateBytes, err := json.Marshal(next)
			return stateBytes, response, err
		default:
			return nil, nil, ErrUnsupportedBoardLine
		}
	})
}
