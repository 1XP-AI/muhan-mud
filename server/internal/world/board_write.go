package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// BoardWriteAction is the durable action recorded for a completed `써`
// submission.  The interactive editor itself is connection-local; only this
// action crosses the world/receipt boundary.
type BoardWriteAction string

const (
	BoardWrite BoardWriteAction = "write"

	// BoardWriteResponse is the actor-facing response emitted after the index
	// and body are committed together.
	BoardWriteResponse = "게시물이 등록되었습니다.\r\n"
)

var (
	ErrBoardWriteInvalidPayload    = errors.New("invalid board write payload")
	ErrBoardWriteBoardMismatch     = errors.New("board write board does not match canonical board object")
	ErrBoardWriteBodyNotTerminated = errors.New("board write body must end with LF")
	ErrBoardWriteInvalidLine       = errors.New("invalid board write body line")
	ErrBoardWriteSequenceExhausted = errors.New("board post sequence exhausted")
	ErrBoardWriteNumberAllocator   = errors.New("board post number allocator failed")
	ErrBoardWriteSequence          = errors.New("board post number is not the next sequence value")
	ErrBoardWriteStaleProposal     = errors.New("stale board write proposal")
	ErrBoardWriteInvalidProposal   = errors.New("invalid board write proposal")
)

// BoardWritePayload is the canonical terminal-editor submission. BoardID is
// optional at the edge (zero means "resolve the board object in the actor's
// current room"); once planned, the proposal always contains the resolved
// board ID. Author is intentionally absent: it is derived from the
// authenticated canonical PlayerState and can never be supplied by a client.
// Body is UTF-8 text with one LF after every submitted editor line.
type BoardWritePayload struct {
	BoardID int    `json:"board_id,omitempty"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

// BoardWriteSubmitPayload is a descriptive alias used by session adapters.
// It is the same wire contract, not a second write path.
type BoardWriteSubmitPayload = BoardWritePayload

// BoardPostNumberAllocator is called once during planning, after all actor,
// board-object and text validation has passed. It may reserve the proposed
// sequence value in an external transaction, but it must return exactly
// nextNumber; arbitrary numbers would break the legacy append-only index.
// A nil allocator uses the in-memory sequence (len(posts)+1).
type BoardPostNumberAllocator func(boardID, nextNumber int) (int, error)

// BoardWriteOptions injects nondeterministic dependencies at the pure
// boundary. The timestamp is normalized to UTC without a monotonic component;
// callers must provide it rather than letting the reducer call time.Now.
type BoardWriteOptions struct {
	Now            time.Time
	AllocateNumber BoardPostNumberAllocator
}

// BoardWriteEvent is the post-commit room notification corresponding to
// write_board's broadcast_rom call. Session/transport publishes it only after
// the receipt commits and must suppress it for a replayed command.
type BoardWriteEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

// BoardWriteResult is the deterministic receipt projection. Post is the
// canonical index/body record appended by the reducer; the event is an
// out-of-state delivery projection and does not itself authorize a commit.
type BoardWriteResult struct {
	Action    BoardWriteAction `json:"action"`
	ActorID   string           `json:"actor_id"`
	ActorName string           `json:"actor_name"`
	RoomID    int16            `json:"room_id"`
	BoardID   int              `json:"board_id"`
	Post      BoardPost        `json:"post"`
	LineCount int              `json:"line_count"`
	Response  string           `json:"response"`
	Changed   bool             `json:"changed"`
	Broadcast bool             `json:"broadcast"`
	Event     *BoardWriteEvent `json:"event,omitempty"`
}

// BoardWriteProposal is a State-bound candidate. The expected snapshots are
// private so callers cannot manufacture a proposal that bypasses the actor,
// room-object, or append-sequence checks in ApplyBoardWrite.
type BoardWriteProposal struct {
	Action    BoardWriteAction
	ActorID   string
	ActorName string
	RoomID    int16
	BoardID   int
	Payload   BoardWritePayload
	Post      BoardPost
	LineCount int
	Response  string
	Event     *BoardWriteEvent

	expectedActor     PlayerState
	expectedBoard     Board
	expectedRoomItems ItemCollection
}

// ValidateBoardWritePayload validates the submit shape without resolving the
// board. A zero BoardID is allowed only as the explicit "resolve from room"
// sentinel; nonzero IDs must belong to board_dir.
func ValidateBoardWritePayload(payload BoardWritePayload) error {
	if payload.BoardID != 0 {
		if err := ValidateBoardID(payload.BoardID); err != nil {
			return fmt.Errorf("%w: board id: %v", ErrBoardWriteInvalidPayload, err)
		}
	}
	if err := ValidateBoardTitle(payload.Title); err != nil {
		return fmt.Errorf("%w: title: %v", ErrBoardWriteInvalidPayload, err)
	}
	if err := ValidateBoardBody(payload.Body); err != nil {
		return fmt.Errorf("%w: body: %v", ErrBoardWriteInvalidPayload, err)
	}
	if !strings.HasSuffix(payload.Body, "\n") {
		return ErrBoardWriteBodyNotTerminated
	}
	if strings.Count(payload.Body, "\n") < 1 {
		return ErrBoardWriteInvalidPayload
	}
	return nil
}

// ValidateBoardWriteLine validates one line collected by the interactive
// editor. Empty lines are allowed when another non-whitespace line exists;
// line separators must be supplied by the editor, not embedded in a line.
func ValidateBoardWriteLine(line string) error {
	if !utf8.ValidString(line) || strings.ContainsRune(line, '\n') {
		return ErrBoardWriteInvalidLine
	}
	if len(line) > MaxBoardBodyBytes {
		return ErrBoardWriteInvalidLine
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrBoardWriteInvalidLine
		}
	}
	return nil
}

// BoardWritePayloadFromLines turns the connection-local editor buffer into a
// canonical payload. It appends exactly one LF per submitted line, matching
// write_board's write(str); write("\\n") sequence. The caller should pass
// only lines before the terminating `.`; `!!` cancellation never reaches this
// function.
func BoardWritePayloadFromLines(boardID int, title string, lines []string) (BoardWritePayload, error) {
	if len(lines) == 0 {
		return BoardWritePayload{}, ErrBoardWriteInvalidPayload
	}
	for _, line := range lines {
		if err := ValidateBoardWriteLine(line); err != nil {
			return BoardWritePayload{}, err
		}
	}
	payload := BoardWritePayload{BoardID: boardID, Title: title, Body: strings.Join(lines, "\n") + "\n"}
	if err := ValidateBoardWritePayload(payload); err != nil {
		return BoardWritePayload{}, err
	}
	return payload, nil
}

// BoardWriteLineCount returns the C-compatible line count for a canonical
// body. Each LF represents one line written by the editor, including blank
// lines between non-empty lines.
func BoardWriteLineCount(body string) (int, error) {
	if err := ValidateBoardBody(body); err != nil {
		return 0, err
	}
	if !strings.HasSuffix(body, "\n") {
		return 0, ErrBoardWriteBodyNotTerminated
	}
	return strings.Count(body, "\n"), nil
}

func canonicalBoardWriteTimestamp(now time.Time) (time.Time, error) {
	if now.IsZero() {
		return time.Time{}, ErrBoardInvalidTimestamp
	}
	// Round(0) strips a process-local monotonic reading, and UTC gives the
	// database/receipt a stable representation regardless of runner locale.
	return now.Round(0).UTC(), nil
}

func nextBoardPostNumber(posts []BoardPost) (int, error) {
	maxInt := int(^uint(0) >> 1)
	if len(posts) >= maxInt {
		return 0, ErrBoardWriteSequenceExhausted
	}
	return len(posts) + 1, nil
}

func boardWriteEvent(actor PlayerState, actorID string) *BoardWriteEvent {
	return &BoardWriteEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n%s님이 게시판에 글을 씁니다.\r\n", actor.Body.Name),
	}
}

// boardWriteContext resolves all authority from State. In particular, a
// client-supplied board ID is only an optional consistency assertion; the
// actual board comes from the canonical SP_BOARD object in the current room.
func boardWriteContext(s State, actorID string, payload BoardWritePayload) (BoardState, PlayerState, RoomState, int, error) {
	if err := ValidateBoardWritePayload(payload); err != nil {
		return BoardState{}, PlayerState{}, RoomState{}, 0, err
	}
	boards, actor, boardID, err := s.BoardContext(actorID)
	if err != nil {
		return BoardState{}, PlayerState{}, RoomState{}, 0, err
	}
	if payload.BoardID != 0 && payload.BoardID != boardID {
		return BoardState{}, PlayerState{}, RoomState{}, 0, ErrBoardWriteBoardMismatch
	}
	if err := ValidateBoardAuthor(actor.Body.Name); err != nil {
		return BoardState{}, PlayerState{}, RoomState{}, 0, fmt.Errorf("board write author: %w", err)
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Items == nil {
		return BoardState{}, PlayerState{}, RoomState{}, 0, ErrBoardObjectMissing
	}
	return boards, actor, room, boardID, nil
}

// PlanBoardWrite creates a proposal with the default in-memory sequence
// allocator. Use PlanBoardWriteWithOptions when the persistence boundary
// supplies a deterministic allocator or a test clock.
func (s State) PlanBoardWrite(actorID string, payload BoardWritePayload, now time.Time) (BoardWriteProposal, error) {
	return s.PlanBoardWriteWithOptions(actorID, payload, BoardWriteOptions{Now: now})
}

// PlanBoardWriteWithAllocator is a convenience for callers that need a
// durable sequence allocator but no other options.
func (s State) PlanBoardWriteWithAllocator(actorID string, payload BoardWritePayload, now time.Time, allocate BoardPostNumberAllocator) (BoardWriteProposal, error) {
	return s.PlanBoardWriteWithOptions(actorID, payload, BoardWriteOptions{Now: now, AllocateNumber: allocate})
}

// PlanBoardWrite is deliberately separate from the interactive continuation:
// the session collects title/body lines, builds BoardWritePayloadFromLines,
// then submits that immutable payload here exactly once.
func (s State) PlanBoardWriteWithOptions(actorID string, payload BoardWritePayload, options BoardWriteOptions) (BoardWriteProposal, error) {
	boards, actor, room, boardID, err := boardWriteContext(s, actorID, payload)
	if err != nil {
		return BoardWriteProposal{}, err
	}
	now, err := canonicalBoardWriteTimestamp(options.Now)
	if err != nil {
		return BoardWriteProposal{}, err
	}
	board, ok := boards.Boards[boardID]
	if !ok {
		return BoardWriteProposal{}, ErrBoardNotFound
	}
	nextNumber, err := nextBoardPostNumber(board.Posts)
	if err != nil {
		return BoardWriteProposal{}, err
	}
	allocator := options.AllocateNumber
	if allocator != nil {
		allocated, allocErr := allocator(boardID, nextNumber)
		if allocErr != nil {
			return BoardWriteProposal{}, fmt.Errorf("%w: %v", ErrBoardWriteNumberAllocator, allocErr)
		}
		if allocated != nextNumber {
			return BoardWriteProposal{}, fmt.Errorf("%w: allocated=%d expected=%d", ErrBoardWriteSequence, allocated, nextNumber)
		}
	}
	lineCount, err := BoardWriteLineCount(payload.Body)
	if err != nil {
		return BoardWriteProposal{}, fmt.Errorf("%w: %v", ErrBoardWriteInvalidPayload, err)
	}
	canonicalPayload := payload
	canonicalPayload.BoardID = boardID
	post := BoardPost{
		Number:    nextNumber,
		Author:    actor.Body.Name,
		Title:     canonicalPayload.Title,
		Body:      canonicalPayload.Body,
		CreatedAt: now,
		ReadCount: 0,
		Deleted:   false,
	}
	if err := validateBoardPost(post); err != nil {
		return BoardWriteProposal{}, fmt.Errorf("%w: %v", ErrBoardWriteInvalidPayload, err)
	}
	return BoardWriteProposal{
		Action:            BoardWrite,
		ActorID:           actorID,
		ActorName:         actor.Body.Name,
		RoomID:            actor.Body.RoomID,
		BoardID:           boardID,
		Payload:           canonicalPayload,
		Post:              post,
		LineCount:         lineCount,
		Response:          BoardWriteResponse,
		Event:             boardWriteEvent(actor, actorID),
		expectedActor:     actor,
		expectedBoard:     Board{Posts: cloneBoardPosts(board.Posts)},
		expectedRoomItems: room.Items.clone(),
	}, nil
}

func sameBoardWriteEvent(left, right *BoardWriteEvent) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// ApplyBoardWrite applies a proposal atomically. The board object, actor and
// target board are re-resolved before cloning; any change since planning
// rejects the proposal without publishing a partial index/body update.
func (s State) ApplyBoardWrite(proposal BoardWriteProposal) (State, BoardWriteResult, error) {
	if proposal.Action != BoardWrite || proposal.ActorID == "" || proposal.Response != BoardWriteResponse {
		return State{}, BoardWriteResult{}, ErrBoardWriteInvalidProposal
	}
	if err := ValidateBoardWritePayload(proposal.Payload); err != nil {
		return State{}, BoardWriteResult{}, ErrBoardWriteInvalidProposal
	}
	if proposal.Payload.BoardID != proposal.BoardID || proposal.BoardID == 0 {
		return State{}, BoardWriteResult{}, ErrBoardWriteInvalidProposal
	}
	lineCount, err := BoardWriteLineCount(proposal.Payload.Body)
	if err != nil || lineCount != proposal.LineCount {
		return State{}, BoardWriteResult{}, ErrBoardWriteInvalidProposal
	}
	if err := validateBoardPost(proposal.Post); err != nil || proposal.Post.Author != proposal.ActorName || proposal.Post.Title != proposal.Payload.Title || proposal.Post.Body != proposal.Payload.Body || proposal.Post.ReadCount != 0 || proposal.Post.Deleted {
		return State{}, BoardWriteResult{}, ErrBoardWriteInvalidProposal
	}
	if proposal.Event == nil || !sameBoardWriteEvent(proposal.Event, boardWriteEvent(proposal.expectedActor, proposal.ActorID)) {
		return State{}, BoardWriteResult{}, ErrBoardWriteInvalidProposal
	}

	boards, actor, room, boardID, err := boardWriteContext(s, proposal.ActorID, proposal.Payload)
	if err != nil {
		return State{}, BoardWriteResult{}, err
	}
	if boardID != proposal.BoardID || actor.Body.Name != proposal.ActorName || actor.Body.RoomID != proposal.RoomID || !reflect.DeepEqual(actor, proposal.expectedActor) || room.Items == nil || !reflect.DeepEqual(*room.Items, proposal.expectedRoomItems) {
		return State{}, BoardWriteResult{}, ErrBoardWriteStaleProposal
	}
	board, ok := boards.Boards[boardID]
	if !ok || !reflect.DeepEqual(board, proposal.expectedBoard) {
		return State{}, BoardWriteResult{}, ErrBoardWriteStaleProposal
	}
	nextNumber, err := nextBoardPostNumber(board.Posts)
	if err != nil {
		return State{}, BoardWriteResult{}, err
	}
	if proposal.Post.Number != nextNumber {
		return State{}, BoardWriteResult{}, ErrBoardWriteSequence
	}

	nextBoards := boards.Clone()
	nextBoards.Boards[boardID] = Board{Posts: append(cloneBoardPosts(board.Posts), proposal.Post)}
	next, err := s.WithBoards(nextBoards)
	if err != nil {
		return State{}, BoardWriteResult{}, err
	}
	result := BoardWriteResult{
		Action:    BoardWrite,
		ActorID:   proposal.ActorID,
		ActorName: actor.Body.Name,
		RoomID:    actor.Body.RoomID,
		BoardID:   boardID,
		Post:      proposal.Post,
		LineCount: proposal.LineCount,
		Response:  BoardWriteResponse,
		Changed:   true,
		Broadcast: true,
		Event:     boardWriteEvent(actor, proposal.ActorID),
	}
	return next, result, nil
}

// WriteBoard is the one-call pure candidate/apply convenience. Session code
// should use the explicit two-phase methods so the durable receipt records the
// proposal and suppresses duplicate event delivery on replay.
func (s State) WriteBoard(actorID string, payload BoardWritePayload, now time.Time) (State, BoardWriteResult, error) {
	proposal, err := s.PlanBoardWrite(actorID, payload, now)
	if err != nil {
		return State{}, BoardWriteResult{}, err
	}
	return s.ApplyBoardWrite(proposal)
}
