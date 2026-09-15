package world

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The board directory is a closed set in src/board.c.  A board number is a
// legacy public identifier, not an arbitrary database key; accepting an
// unknown number here would make it possible to address an unowned board.
const (
	// BoardSpecial is SP_BOARD from src/mtype.h. The board object type is the
	// closed board_dir number; both values are read from canonical room items.
	BoardSpecial        = 4
	MaxBoardAuthorBytes = 14 // the legacy character-name boundary
	MaxBoardTitleBytes  = 39 // BOARD_INDEX.title[40], including its NUL slot
	MaxBoardBodyBytes   = 1 << 20
)

// Descriptive aliases keep callers from having to know whether a limit came
// from the legacy field or from this domain's name.
const (
	BoardAuthorMaxBytes = MaxBoardAuthorBytes
	BoardTitleMaxBytes  = MaxBoardTitleBytes
	BoardBodyMaxBytes   = MaxBoardBodyBytes
)

var (
	ErrBoardStateInvalid      = errors.New("invalid board state")
	ErrBoardInvalidID         = errors.New("invalid board id")
	ErrBoardNotFound          = errors.New("board not found")
	ErrBoardInvalidPostNumber = errors.New("invalid board post number")
	ErrBoardPostNotFound      = errors.New("board post not found")
	ErrBoardDuplicatePost     = errors.New("duplicate board post number")
	ErrBoardPostSequence      = errors.New("board post numbers are not contiguous")
	ErrBoardInputEmpty        = errors.New("board input is empty")
	ErrBoardInputInvalidUTF8  = errors.New("board input is not valid UTF-8")
	ErrBoardInputControl      = errors.New("board input contains a control character")
	ErrBoardInputTooLong      = errors.New("board input exceeds the byte limit")
	ErrBoardInvalidTimestamp  = errors.New("invalid board post timestamp")
	ErrBoardInvalidReadCount  = errors.New("invalid board post read count")
	ErrBoardDeleted           = errors.New("board post is deleted")
	ErrBoardUnauthorized      = errors.New("board post mutation is unauthorized")
	ErrBoardStaleProposal     = errors.New("stale board proposal")
	ErrBoardInvalidProposal   = errors.New("invalid board proposal")
	ErrBoardReadCountOverflow = errors.New("board post read count overflow")
	ErrBoardActorAbsent       = errors.New("online canonical board actor absent")
	ErrBoardObjectMissing     = errors.New("board object is not present in the room")
)

// BoardState is the pure, in-memory board aggregate.  Posts are stored in
// legacy sequence order (oldest first); callers use List for C's newest-first
// presentation.  The state deliberately does not embed State: persistence,
// command receipts, and session permissions are integration concerns.
type BoardState struct {
	Boards map[int]Board `json:"boards"`
}

// Board is one legacy board directory.  Its Posts slice is ordered by the
// legacy post number and is never sorted implicitly during validation.
type Board struct {
	Posts []BoardPost `json:"posts"`
}

// BoardPost is the normalized equivalent of one BOARD_INDEX plus its
// board.<n> body file.  Deleted is explicit instead of overloading a negative
// read count, while ReadCount remains the non-negative absolute count.
type BoardPost struct {
	Number    int       `json:"number"`
	Author    string    `json:"author"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	ReadCount int       `json:"read_count"`
	Deleted   bool      `json:"deleted"`
}

// BoardContext binds a board command to the canonical room object and actor.
// It is the only adapter that resolves the legacy board object type into a
// BoardState key; callers must not accept a client-supplied board number as
// authorization.
func (s State) BoardContext(actorID string) (BoardState, PlayerState, int, error) {
	if err := s.Validate(); err != nil {
		return BoardState{}, PlayerState{}, 0, err
	}
	if s.Boards == nil {
		return BoardState{}, PlayerState{}, 0, ErrBoardStateInvalid
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return BoardState{}, PlayerState{}, 0, ErrBoardActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Items == nil {
		return BoardState{}, PlayerState{}, 0, ErrBoardObjectMissing
	}
	if err := room.Items.Validate(); err != nil {
		return BoardState{}, PlayerState{}, 0, err
	}
	for _, itemID := range room.Items.Inventory {
		item, ok := room.Items.Items[itemID]
		if !ok || item.Object.Special != BoardSpecial || !IsValidBoardID(int(item.Object.Type)) {
			continue
		}
		boardID := int(item.Object.Type)
		if _, ok := s.Boards.Boards[boardID]; !ok {
			return BoardState{}, PlayerState{}, 0, ErrBoardNotFound
		}
		return *s.Boards, actor, boardID, nil
	}
	return BoardState{}, PlayerState{}, 0, ErrBoardObjectMissing
}

// WithBoards returns a validated world candidate with the supplied board
// aggregate installed. It keeps all other canonical domains unchanged and is
// the state-level commit adapter used by the session receipt reducer.
func (s State) WithBoards(boards BoardState) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if err := boards.Validate(); err != nil {
		return State{}, err
	}
	next := s.clone()
	copy := boards.Clone()
	next.Boards = &copy
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// BoardReadProposal binds a read to the exact post snapshot observed during
// planning.  The private fields prevent callers from manufacturing an
// increment or an admin read without going through PlanRead.
type BoardReadProposal struct {
	BoardID    int    `json:"board_id"`
	PostNumber int    `json:"post_number"`
	ViewerID   string `json:"viewer_id"`

	ExpectedReadCount int `json:"expected_read_count"`

	expectedPost BoardPost
	increment    bool
	admin        bool
}

// BoardReadResult contains the post as it was displayed by C before the
// read-modify-write. CommittedPost exposes the resulting canonical value for
// callers that need the new counter; owner reads have identical values.
type BoardReadResult struct {
	BoardID         int       `json:"board_id"`
	PostNumber      int       `json:"post_number"`
	ViewerID        string    `json:"viewer_id"`
	Post            BoardPost `json:"post"`
	CommittedPost   BoardPost `json:"committed_post"`
	ReadCountBefore int       `json:"read_count_before"`
	ReadCountAfter  int       `json:"read_count_after"`
	Changed         bool      `json:"changed"`
}

// BoardDeleteProposal toggles a post's deletion tombstone, matching
// del_board: deleting an active post and restoring a deleted post are the
// same operation. Only the author or a DM may plan it.
type BoardDeleteProposal struct {
	BoardID         int    `json:"board_id"`
	PostNumber      int    `json:"post_number"`
	ActorID         string `json:"actor_id"`
	ExpectedDeleted bool   `json:"expected_deleted"`
	DesiredDeleted  bool   `json:"desired_deleted"`

	expectedPost  BoardPost
	expectedActor string
	authorized    bool
	admin         bool
}

// BoardDeleteResult reports the committed tombstone and complete post value.
type BoardDeleteResult struct {
	BoardID    int       `json:"board_id"`
	PostNumber int       `json:"post_number"`
	ActorID    string    `json:"actor_id"`
	Post       BoardPost `json:"post"`
	Deleted    bool      `json:"deleted"`
	Changed    bool      `json:"changed"`
}

// ValidateBoardID admits only the board_dir entries in src/board.c.
func ValidateBoardID(id int) error {
	switch id {
	case 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 120:
		return nil
	default:
		return fmt.Errorf("%w: %d", ErrBoardInvalidID, id)
	}
}

// IsValidBoardID is useful to adapters that need a non-error closed-set
// check before constructing a command.
func IsValidBoardID(id int) bool { return ValidateBoardID(id) == nil }

// ValidateBoardPostNumber validates the one-based legacy index. Existence is
// checked against a BoardState by lookupBoardPost.
func ValidateBoardPostNumber(number int) error {
	if number < 1 {
		return fmt.Errorf("%w: %d", ErrBoardInvalidPostNumber, number)
	}
	return nil
}

// ValidateBoardAuthor validates the fixed BOARD_INDEX.upload field and the
// name used by C's strcmp ownership checks.
func ValidateBoardAuthor(author string) error {
	return validateBoardText(author, MaxBoardAuthorBytes, false)
}

// ValidateBoardTitle validates the fixed BOARD_INDEX.title field without
// truncating at a UTF-8 boundary. A title is a single terminal line.
func ValidateBoardTitle(title string) error {
	return validateBoardText(title, MaxBoardTitleBytes, false)
}

// ValidateBoardBody validates the text file represented by board.<n>. Newline
// is the one control character admitted as a line separator; carriage returns,
// terminal escapes, and other controls fail closed.
func ValidateBoardBody(body string) error {
	return validateBoardText(body, MaxBoardBodyBytes, true)
}

func validateBoardText(value string, maxBytes int, allowNewline bool) error {
	if value == "" || strings.TrimSpace(value) == "" {
		return ErrBoardInputEmpty
	}
	if !utf8.ValidString(value) {
		return ErrBoardInputInvalidUTF8
	}
	if len(value) > maxBytes {
		return ErrBoardInputTooLong
	}
	for _, r := range value {
		if r == '\n' && allowNewline {
			continue
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrBoardInputControl
		}
	}
	return nil
}

func validateBoardActorID(id string) error {
	if id == "" || !utf8.ValidString(id) || strings.TrimSpace(id) != id {
		return ErrBoardInputEmpty
	}
	for _, r := range id {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrBoardInputControl
		}
	}
	return nil
}

func validateBoardPost(post BoardPost) error {
	if err := ValidateBoardPostNumber(post.Number); err != nil {
		return err
	}
	if err := ValidateBoardAuthor(post.Author); err != nil {
		return fmt.Errorf("board post author: %w", err)
	}
	if err := ValidateBoardTitle(post.Title); err != nil {
		return fmt.Errorf("board post title: %w", err)
	}
	if err := ValidateBoardBody(post.Body); err != nil {
		return fmt.Errorf("board post body: %w", err)
	}
	if post.CreatedAt.IsZero() {
		return ErrBoardInvalidTimestamp
	}
	if post.ReadCount < 0 {
		return ErrBoardInvalidReadCount
	}
	return nil
}

// Validate checks the full board aggregate and its ordered post invariants.
// It does not mutate or sort anything, so malformed order is observable and
// cannot be silently repaired during a replay.
func (s BoardState) Validate() error {
	if s.Boards == nil {
		return ErrBoardStateInvalid
	}
	ids := make([]int, 0, len(s.Boards))
	for boardID := range s.Boards {
		ids = append(ids, boardID)
	}
	sort.Ints(ids)
	for _, boardID := range ids {
		if err := ValidateBoardID(boardID); err != nil {
			return fmt.Errorf("board %d: %w", boardID, err)
		}
		board := s.Boards[boardID]
		seen := make(map[int]struct{}, len(board.Posts))
		for index, post := range board.Posts {
			if err := validateBoardPost(post); err != nil {
				return fmt.Errorf("board %d post %d: %w", boardID, index+1, err)
			}
			if _, ok := seen[post.Number]; ok {
				return fmt.Errorf("board %d post %d: %w", boardID, post.Number, ErrBoardDuplicatePost)
			}
			seen[post.Number] = struct{}{}
			if post.Number != index+1 {
				return fmt.Errorf("board %d post %d: %w", boardID, post.Number, ErrBoardPostSequence)
			}
		}
	}
	return nil
}

// Clone returns an independent board aggregate. It intentionally preserves
// malformed values when called directly; Apply methods validate their input
// and therefore never clone an invalid candidate into committed state.
func (s BoardState) Clone() BoardState {
	if s.Boards == nil {
		return BoardState{}
	}
	next := BoardState{Boards: make(map[int]Board, len(s.Boards))}
	for boardID, board := range s.Boards {
		next.Boards[boardID] = Board{Posts: cloneBoardPosts(board.Posts)}
	}
	return next
}

func cloneBoardPosts(posts []BoardPost) []BoardPost {
	if posts == nil {
		return nil
	}
	clone := make([]BoardPost, len(posts))
	copy(clone, posts)
	return clone
}

func (s BoardState) board(boardID int) (Board, error) {
	if err := ValidateBoardID(boardID); err != nil {
		return Board{}, err
	}
	board, ok := s.Boards[boardID]
	if !ok {
		return Board{}, fmt.Errorf("%w: %d", ErrBoardNotFound, boardID)
	}
	return board, nil
}

func (s BoardState) lookupBoardPost(boardID, postNumber int) (Board, BoardPost, error) {
	board, err := s.board(boardID)
	if err != nil {
		return Board{}, BoardPost{}, err
	}
	if err := ValidateBoardPostNumber(postNumber); err != nil {
		return Board{}, BoardPost{}, err
	}
	if postNumber > len(board.Posts) {
		return Board{}, BoardPost{}, fmt.Errorf("%w: %d", ErrBoardPostNotFound, postNumber)
	}
	post := board.Posts[postNumber-1]
	if post.Number != postNumber {
		return Board{}, BoardPost{}, fmt.Errorf("%w: %d", ErrBoardPostNotFound, postNumber)
	}
	return board, post, nil
}

// List returns a defensive, deterministic newest-first view. C's list_board
// skips deleted rows for ordinary characters and shows them to DMs.
func (s BoardState) List(boardID int, admin bool) ([]BoardPost, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	board, err := s.board(boardID)
	if err != nil {
		return nil, err
	}
	posts := make([]BoardPost, 0, len(board.Posts))
	for i := len(board.Posts) - 1; i >= 0; i-- {
		post := board.Posts[i]
		if post.Deleted && !admin {
			continue
		}
		posts = append(posts, post)
	}
	return posts, nil
}

// ListBoard is a descriptive alias used by adapters that prefer the legacy
// command name.
func (s BoardState) ListBoard(boardID int, admin bool) ([]BoardPost, error) {
	return s.List(boardID, admin)
}

// ListPosts is another explicit spelling for callers that distinguish a board
// view from other list operations.
func (s BoardState) ListPosts(boardID int, admin bool) ([]BoardPost, error) {
	return s.List(boardID, admin)
}

// PlanRead validates visibility and captures the exact post before C's read
// counter update. A post owner is identified by exact author-name equality,
// matching strcmp in read_board.
func (s BoardState) PlanRead(boardID, postNumber int, viewerID string, admin bool) (BoardReadProposal, error) {
	if err := s.Validate(); err != nil {
		return BoardReadProposal{}, err
	}
	if err := validateBoardActorID(viewerID); err != nil {
		return BoardReadProposal{}, err
	}
	_, post, err := s.lookupBoardPost(boardID, postNumber)
	if err != nil {
		return BoardReadProposal{}, err
	}
	if post.Deleted && !admin {
		return BoardReadProposal{}, ErrBoardDeleted
	}
	return BoardReadProposal{
		BoardID:           boardID,
		PostNumber:        postNumber,
		ViewerID:          viewerID,
		ExpectedReadCount: post.ReadCount,
		expectedPost:      post,
		increment:         viewerID != post.Author,
		admin:             admin,
	}, nil
}

// ApplyRead applies only a proposal made against this exact post snapshot.
// Reapplying the same non-owner proposal is therefore rejected rather than
// incrementing a counter twice after a lost acknowledgement.
func (s BoardState) ApplyRead(proposal BoardReadProposal) (BoardState, BoardReadResult, error) {
	if err := s.Validate(); err != nil {
		return BoardState{}, BoardReadResult{}, err
	}
	if err := ValidateBoardID(proposal.BoardID); err != nil {
		return BoardState{}, BoardReadResult{}, ErrBoardInvalidProposal
	}
	if err := ValidateBoardPostNumber(proposal.PostNumber); err != nil {
		return BoardState{}, BoardReadResult{}, ErrBoardInvalidProposal
	}
	if err := validateBoardActorID(proposal.ViewerID); err != nil {
		return BoardState{}, BoardReadResult{}, ErrBoardInvalidProposal
	}
	_, post, err := s.lookupBoardPost(proposal.BoardID, proposal.PostNumber)
	if err != nil {
		return BoardState{}, BoardReadResult{}, err
	}
	if !sameBoardPost(post, proposal.expectedPost) || proposal.ExpectedReadCount != post.ReadCount || proposal.increment != (proposal.ViewerID != post.Author) {
		return BoardState{}, BoardReadResult{}, ErrBoardStaleProposal
	}
	if post.Deleted && !proposal.admin {
		return BoardState{}, BoardReadResult{}, ErrBoardDeleted
	}
	if proposal.increment && post.ReadCount == int(^uint(0)>>1) {
		return BoardState{}, BoardReadResult{}, ErrBoardReadCountOverflow
	}
	next := s.Clone()
	nextPost := post
	if proposal.increment {
		nextPost.ReadCount++
	}
	next.Boards[proposal.BoardID] = Board{Posts: cloneBoardPosts(next.Boards[proposal.BoardID].Posts)}
	next.Boards[proposal.BoardID].Posts[proposal.PostNumber-1] = nextPost
	if err := next.Validate(); err != nil {
		return BoardState{}, BoardReadResult{}, err
	}
	return next, BoardReadResult{
		BoardID:         proposal.BoardID,
		PostNumber:      proposal.PostNumber,
		ViewerID:        proposal.ViewerID,
		Post:            post,
		CommittedPost:   nextPost,
		ReadCountBefore: post.ReadCount,
		ReadCountAfter:  nextPost.ReadCount,
		Changed:         proposal.increment,
	}, nil
}

// PlanDelete toggles the C del_board tombstone. Only the post author or an
// administrator may plan the mutation.
func (s BoardState) PlanDelete(boardID, postNumber int, actorID string, admin bool) (BoardDeleteProposal, error) {
	if err := s.Validate(); err != nil {
		return BoardDeleteProposal{}, err
	}
	if err := validateBoardActorID(actorID); err != nil {
		return BoardDeleteProposal{}, err
	}
	_, post, err := s.lookupBoardPost(boardID, postNumber)
	if err != nil {
		return BoardDeleteProposal{}, err
	}
	if actorID != post.Author && !admin {
		return BoardDeleteProposal{}, ErrBoardUnauthorized
	}
	return BoardDeleteProposal{
		BoardID:         boardID,
		PostNumber:      postNumber,
		ActorID:         actorID,
		ExpectedDeleted: post.Deleted,
		DesiredDeleted:  !post.Deleted,
		expectedPost:    post,
		expectedActor:   actorID,
		authorized:      true,
		admin:           admin,
	}, nil
}

// PlanDeleteRestore is the explicit descriptive spelling for the toggle
// operation. It is intentionally not a second semantic path.
func (s BoardState) PlanDeleteRestore(boardID, postNumber int, actorID string, admin bool) (BoardDeleteProposal, error) {
	return s.PlanDelete(boardID, postNumber, actorID, admin)
}

// ApplyDelete applies an author/admin toggle against the exact planned post.
// A replay of the same proposal is rejected as stale after the first toggle.
func (s BoardState) ApplyDelete(proposal BoardDeleteProposal) (BoardState, BoardDeleteResult, error) {
	if err := s.Validate(); err != nil {
		return BoardState{}, BoardDeleteResult{}, err
	}
	if !proposal.authorized || proposal.ActorID == "" || proposal.ActorID != proposal.expectedActor {
		return BoardState{}, BoardDeleteResult{}, ErrBoardUnauthorized
	}
	if err := ValidateBoardID(proposal.BoardID); err != nil {
		return BoardState{}, BoardDeleteResult{}, ErrBoardInvalidProposal
	}
	if err := ValidateBoardPostNumber(proposal.PostNumber); err != nil {
		return BoardState{}, BoardDeleteResult{}, ErrBoardInvalidProposal
	}
	_, post, err := s.lookupBoardPost(proposal.BoardID, proposal.PostNumber)
	if err != nil {
		return BoardState{}, BoardDeleteResult{}, err
	}
	if proposal.ActorID != post.Author && !proposal.admin {
		return BoardState{}, BoardDeleteResult{}, ErrBoardUnauthorized
	}
	if !sameBoardPost(post, proposal.expectedPost) || proposal.ExpectedDeleted != post.Deleted || proposal.DesiredDeleted == post.Deleted || proposal.DesiredDeleted != !proposal.ExpectedDeleted {
		return BoardState{}, BoardDeleteResult{}, ErrBoardStaleProposal
	}
	next := s.Clone()
	nextPost := post
	nextPost.Deleted = proposal.DesiredDeleted
	board := next.Boards[proposal.BoardID]
	board.Posts[proposal.PostNumber-1] = nextPost
	next.Boards[proposal.BoardID] = board
	if err := next.Validate(); err != nil {
		return BoardState{}, BoardDeleteResult{}, err
	}
	return next, BoardDeleteResult{
		BoardID:    proposal.BoardID,
		PostNumber: proposal.PostNumber,
		ActorID:    proposal.ActorID,
		Post:       nextPost,
		Deleted:    nextPost.Deleted,
		Changed:    true,
	}, nil
}

// ApplyDeleteRestore is the explicit descriptive spelling for ApplyDelete.
func (s BoardState) ApplyDeleteRestore(proposal BoardDeleteProposal) (BoardState, BoardDeleteResult, error) {
	return s.ApplyDelete(proposal)
}

func sameBoardPost(left, right BoardPost) bool {
	return left.Number == right.Number &&
		left.Author == right.Author &&
		left.Title == right.Title &&
		left.Body == right.Body &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		left.ReadCount == right.ReadCount &&
		left.Deleted == right.Deleted
}
