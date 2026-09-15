package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func boardWriteStateFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				Items: &ItemCollection{
					Items: map[string]Item{
						"board-100": {Object: LegacyObject{Name: "게시판", Type: 100, Special: BoardSpecial}},
					},
					Inventory: []string{"board-100"},
				},
				PlayerIDs: []string{"alice"},
			},
		},
		Players: map[string]PlayerState{
			"alice": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
		Boards: &BoardState{Boards: map[int]Board{
			100: {Posts: []BoardPost{
				{Number: 1, Author: "Bob", Title: "기존 글", Body: "기존 본문\n", CreatedAt: time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC), ReadCount: 4},
			}},
		}},
	}
}

func TestBoardWritePayloadFromLinesUsesCanonicalLFAndLineCount(t *testing.T) {
	payload, err := BoardWritePayloadFromLines(100, "제목", []string{"첫 줄", "", "마지막 줄"})
	if err != nil {
		t.Fatal(err)
	}
	if payload.BoardID != 100 || payload.Body != "첫 줄\n\n마지막 줄\n" {
		t.Fatalf("payload=%+v", payload)
	}
	if got, err := BoardWriteLineCount(payload.Body); err != nil || got != 3 {
		t.Fatalf("line count=%d err=%v", got, err)
	}

	for name, line := range map[string]string{
		"embedded LF":  "두 줄\n한 입력",
		"control":      "잘못\x1b",
		"invalid UTF8": string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateBoardWriteLine(line); !errors.Is(err, ErrBoardWriteInvalidLine) {
				t.Fatalf("line error=%v", err)
			}
		})
	}
	if _, err := BoardWritePayloadFromLines(100, "제목", nil); !errors.Is(err, ErrBoardWriteInvalidPayload) {
		t.Fatalf("empty lines error=%v", err)
	}
	if err := ValidateBoardWritePayload(BoardWritePayload{BoardID: 100, Title: "제목", Body: "끝 LF 없음"}); !errors.Is(err, ErrBoardWriteBodyNotTerminated) {
		t.Fatalf("unterminated body error=%v", err)
	}
}

func TestPlanApplyBoardWriteAppendsSequenceAndAtomicReceipt(t *testing.T) {
	s := boardWriteStateFixture()
	now := time.Date(2026, 9, 9, 12, 34, 56, 123, time.FixedZone("KST", 9*60*60))
	payload, err := BoardWritePayloadFromLines(0, "새 제목", []string{"첫 본문", "둘째 본문"})
	if err != nil {
		t.Fatal(err)
	}
	allocatorCalls := 0
	proposal, err := s.PlanBoardWriteWithOptions("alice", payload, BoardWriteOptions{
		Now: now,
		AllocateNumber: func(boardID, nextNumber int) (int, error) {
			allocatorCalls++
			if boardID != 100 || nextNumber != 2 {
				t.Fatalf("allocator args board=%d number=%d", boardID, nextNumber)
			}
			return nextNumber, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocatorCalls != 1 || proposal.BoardID != 100 || proposal.Payload.BoardID != 100 || proposal.Post.Number != 2 || proposal.Post.Author != "Alice" || proposal.LineCount != 2 {
		t.Fatalf("proposal=%+v allocatorCalls=%d", proposal, allocatorCalls)
	}
	if !proposal.Post.CreatedAt.Equal(now.UTC()) || proposal.Post.CreatedAt.Location() != time.UTC {
		t.Fatalf("timestamp=%v", proposal.Post.CreatedAt)
	}
	if proposal.Post.ReadCount != 0 || proposal.Post.Deleted {
		t.Fatalf("new post flags=%+v", proposal.Post)
	}
	if got := len(s.Boards.Boards[100].Posts); got != 1 {
		t.Fatalf("planning mutated source posts=%d", got)
	}

	next, result, err := s.ApplyBoardWrite(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != BoardWrite || !result.Changed || !result.Broadcast || result.Response != BoardWriteResponse || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if result.Event.ExcludeActorID != "alice" || !strings.Contains(result.Event.Text, "Alice님이 게시판에 글을 씁니다") {
		t.Fatalf("event=%+v", result.Event)
	}
	posts := next.Boards.Boards[100].Posts
	if len(posts) != 2 || !reflect.DeepEqual(posts[1], proposal.Post) || posts[1].Number != 2 {
		t.Fatalf("next posts=%+v", posts)
	}
	if len(s.Boards.Boards[100].Posts) != 1 {
		t.Fatal("apply mutated source state")
	}
	if _, _, err := next.ApplyBoardWrite(proposal); !errors.Is(err, ErrBoardWriteStaleProposal) {
		t.Fatalf("replayed proposal error=%v", err)
	}
}

func TestBoardWriteRejectsMismatchedBoardAndDoesNotCallAllocator(t *testing.T) {
	s := boardWriteStateFixture()
	payload, err := BoardWritePayloadFromLines(101, "제목", []string{"본문"})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = s.PlanBoardWriteWithOptions("alice", payload, BoardWriteOptions{
		Now: time.Unix(1, 0),
		AllocateNumber: func(int, int) (int, error) {
			called = true
			return 2, nil
		},
	})
	if !errors.Is(err, ErrBoardWriteBoardMismatch) || called {
		t.Fatalf("error=%v allocatorCalled=%v", err, called)
	}

	for name, payload := range map[string]BoardWritePayload{
		"unknown board": {BoardID: 999, Title: "제목", Body: "본문\n"},
		"invalid title": {BoardID: 100, Title: "", Body: "본문\n"},
		"invalid body":  {BoardID: 100, Title: "제목", Body: "본문"},
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			_, err := s.PlanBoardWriteWithOptions("alice", payload, BoardWriteOptions{
				Now: time.Unix(1, 0),
				AllocateNumber: func(int, int) (int, error) {
					called = true
					return 2, nil
				},
			})
			if err == nil || called {
				t.Fatalf("error=%v allocatorCalled=%v", err, called)
			}
		})
	}
}

func TestBoardWriteRequiresCanonicalObjectAndRejectsStaleSnapshots(t *testing.T) {
	s := boardWriteStateFixture()
	payload, err := BoardWritePayloadFromLines(100, "제목", []string{"본문"})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := s.PlanBoardWrite("alice", payload, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}

	missingObject := s.clone()
	missingObject.Rooms[1].Items.Items = map[string]Item{}
	missingObject.Rooms[1].Items.Inventory = nil
	if _, _, err := missingObject.ApplyBoardWrite(proposal); !errors.Is(err, ErrBoardObjectMissing) {
		t.Fatalf("missing object error=%v", err)
	}

	changedBoard := s.clone()
	changedBoard.Boards.Boards[100] = Board{Posts: append(changedBoard.Boards.Boards[100].Posts, BoardPost{
		Number: 2, Author: "Carol", Title: "동시 글", Body: "동시 본문\n", CreatedAt: time.Unix(101, 0),
	})}
	if _, _, err := changedBoard.ApplyBoardWrite(proposal); !errors.Is(err, ErrBoardWriteStaleProposal) {
		t.Fatalf("changed board error=%v", err)
	}

	changedActor := s.clone()
	actor := changedActor.Players["alice"]
	actor.Body.RoomID = 2
	changedActor.Players["alice"] = actor
	if _, _, err := changedActor.ApplyBoardWrite(proposal); err == nil {
		t.Fatal("changed actor was accepted")
	}
}

func TestBoardWriteAllocatorMustReturnNextSequence(t *testing.T) {
	s := boardWriteStateFixture()
	payload, err := BoardWritePayloadFromLines(100, "제목", []string{"본문"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PlanBoardWriteWithOptions("alice", payload, BoardWriteOptions{
		Now:            time.Unix(100, 0),
		AllocateNumber: func(int, int) (int, error) { return 99, nil },
	})
	if !errors.Is(err, ErrBoardWriteSequence) {
		t.Fatalf("wrong number error=%v", err)
	}
	_, err = s.PlanBoardWriteWithOptions("alice", payload, BoardWriteOptions{
		Now:            time.Unix(100, 0),
		AllocateNumber: func(int, int) (int, error) { return 0, errors.New("db sequence unavailable") },
	})
	if !errors.Is(err, ErrBoardWriteNumberAllocator) {
		t.Fatalf("allocator error=%v", err)
	}
}
