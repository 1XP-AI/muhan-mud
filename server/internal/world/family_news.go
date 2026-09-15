package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// FamilyNewsAction is the bounded subset of post.c:family_news admitted by
// the current canonical State. View is the no-argument file projection;
// Append is one newsedit line; Delete is the boss-only unlink branch.
type FamilyNewsAction string

const (
	FamilyNewsView    FamilyNewsAction = "view"
	FamilyNewsAppend  FamilyNewsAction = "append"
	FamilyNewsDelete  FamilyNewsAction = "delete"
	FamilyNewsInvalid FamilyNewsAction = "invalid"

	MaxFamilyNewsLineBytes = 79
	MaxFamilyNewsBytes     = 64 * 1024

	// FamilyNewsHeader is the first write of C newsedit when family_news_<n>
	// does not yet exist: 22 spaces, the title, then two newlines.
	FamilyNewsHeader = "                      === 패거리 공지 ===\n\n"

	FamilyNewsNotMemberResponse     = "당신은 패거리에 가입되어 있지 않습니다."
	FamilyNewsMissingResponse       = "패거리의 공지사항이 없습니다."
	FamilyNewsDeletedResponse       = "공지 내용을 지웠습니다.\n"
	FamilyNewsInvalidOptionResponse = "잘못된 옵션입니다.\n"
	FamilyNewsNotBossResponse       = "삭제는 두목만 가능합니다."
	FamilyNewsAppendPrompt          = "패거리 공지:\n->"
	FamilyNewsAppendContinuePrompt  = "->"
	FamilyNewsAppendDoneResponse    = "공지를 남겼습니다.\n"
	FamilyNewsLimitResponse         = "공지가 너무 깁니다.\n"
	FamilyNewsLineInvalidResponse   = "잘못된 내용입니다.\n"
	FamilyNewsAppendRetryResponse   = "공지를 저장하지 못했습니다. 다시 시도해 주세요.\r\n"
)

var (
	ErrFamilyNewsUnresolved         = errors.New("family news state unresolved")
	ErrFamilyNewsInvalid            = errors.New("invalid family news state")
	ErrFamilyNewsActorAbsent        = errors.New("online canonical family-news actor absent")
	ErrFamilyNewsIdentityUnresolved = errors.New("family-news canonical identity unresolved")
	ErrFamilyNewsStateInvalid       = errors.New("invalid family-news membership state")
	ErrFamilyNewsLineInvalid        = errors.New("invalid family-news line")
	ErrFamilyNewsLimit              = errors.New("family news exceeds the byte limit")
	ErrFamilyNewsStaleProposal      = errors.New("stale family news proposal")
	ErrFamilyNewsInvalidProposal    = errors.New("invalid family news proposal")
)

// FamilyNewsState is the imported family_news_<n> aggregate. A nil pointer on
// State is pre-migration and fail-closed. A non-nil value with a missing
// family key is C's missing file; a present empty string is an empty file.
type FamilyNewsState struct {
	Bodies map[int16]string `json:"bodies"`
}

func (n FamilyNewsState) Validate() error {
	if n.Bodies == nil {
		return ErrFamilyNewsUnresolved
	}
	for familyID, body := range n.Bodies {
		if familyID < 1 || familyID > FamilyMaxID {
			return fmt.Errorf("%w: family id %d", ErrFamilyNewsInvalid, familyID)
		}
		if err := validateFamilyNewsBody(body); err != nil {
			return fmt.Errorf("%w: family %d: %v", ErrFamilyNewsInvalid, familyID, err)
		}
	}
	return nil
}

func (n FamilyNewsState) Clone() FamilyNewsState {
	if n.Bodies == nil {
		return FamilyNewsState{}
	}
	next := FamilyNewsState{Bodies: make(map[int16]string, len(n.Bodies))}
	for familyID, body := range n.Bodies {
		next.Bodies[familyID] = body
	}
	return next
}

func validateFamilyNewsBody(body string) error {
	if !utf8.ValidString(body) {
		return ErrFamilyNewsInvalid
	}
	if len(body) > MaxFamilyNewsBytes {
		return ErrFamilyNewsLimit
	}
	for _, r := range body {
		if r == '\n' {
			continue
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrFamilyNewsLineInvalid
		}
	}
	return nil
}

func truncateFamilyNewsLine(line string) string {
	if len(line) <= MaxFamilyNewsLineBytes {
		return line
	}
	for i := MaxFamilyNewsLineBytes; i >= 0; i-- {
		if utf8.ValidString(line[:i]) {
			return line[:i]
		}
	}
	return ""
}

func validateFamilyNewsLine(line string) error {
	if !utf8.ValidString(line) || strings.ContainsRune(line, '\n') {
		return ErrFamilyNewsLineInvalid
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrFamilyNewsLineInvalid
		}
	}
	return nil
}

// FamilyNewsResult is the durable receipt projection for family_news. View
// and non-mutating gates persist Changed=false so command IDs stay
// idempotent. Append/delete never emit room events: C only writes the file.
type FamilyNewsResult struct {
	Action     FamilyNewsAction `json:"action"`
	ActorID    string           `json:"actor_id"`
	ActorName  string           `json:"actor_name,omitempty"`
	FamilyID   int16            `json:"family_id,omitempty"`
	FamilyName string           `json:"family_name,omitempty"`
	Body       string           `json:"body,omitempty"`
	Response   string           `json:"response"`
	Changed    bool             `json:"changed"`
}

// FamilyNewsProposal binds one news mutation to one complete snapshot.
type FamilyNewsProposal struct {
	Action        FamilyNewsAction
	ActorID       string
	ActorName     string
	FamilyID      int16
	FamilyName    string
	Line          string
	BeforeBody    string
	AfterBody     string
	BeforePresent bool
	AfterPresent  bool
	Response      string
	Changed       bool

	before          State
	expectedActor   PlayerState
	expectedNews    FamilyNewsState
	afterNews       FamilyNewsState
	expectedCatalog FamilyCatalog
}

func familyNewsActor(s State, actorID string) (PlayerState, error) {
	player, ok := s.Players[actorID]
	if actorID == "" || !ok || !player.Online || player.Body.Type != 0 {
		return PlayerState{}, ErrFamilyNewsActorAbsent
	}
	if !validFamilyText(player.Body.Name) {
		return PlayerState{}, ErrFamilyNewsIdentityUnresolved
	}
	canonical, err := identity.CanonicalName(player.Body.Name)
	if err != nil || canonical != player.Body.Name {
		return PlayerState{}, ErrFamilyNewsIdentityUnresolved
	}
	room, ok := s.Rooms[player.Body.RoomID]
	if !ok || room.Resource.ID != player.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrFamilyNewsActorAbsent
	}
	return player, nil
}

func familyNewsImported(s State) (FamilyNewsState, error) {
	if s.FamilyNews == nil {
		return FamilyNewsState{}, ErrFamilyNewsUnresolved
	}
	if err := s.FamilyNews.Validate(); err != nil {
		return FamilyNewsState{}, err
	}
	return s.FamilyNews.Clone(), nil
}

func familyNewsMember(s State, actorID string, catalog FamilyCatalog) (PlayerState, FamilyDefinition, FamilyNewsState, FamilyNewsResult, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyNewsState{}, FamilyNewsResult{}, err
	}
	news, err := familyNewsImported(s)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyNewsState{}, FamilyNewsResult{}, err
	}
	actor, err := familyNewsActor(s, actorID)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyNewsState{}, FamilyNewsResult{}, err
	}
	result := FamilyNewsResult{ActorID: actorID, ActorName: actor.Body.Name}
	active, pending, familyID, err := familyMutationMembership(actor)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyNewsState{}, FamilyNewsResult{}, fmt.Errorf("%w: %v", ErrFamilyNewsStateInvalid, err)
	}
	if !active || pending {
		result.Action = FamilyNewsView
		result.Response = FamilyNewsNotMemberResponse
		return actor, FamilyDefinition{}, news, result, nil
	}
	family, err := familyMutationCatalog(catalog, familyID)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyNewsState{}, FamilyNewsResult{}, err
	}
	result.FamilyID, result.FamilyName = family.ID, family.Name
	return actor, family, news, result, nil
}

func familyNewsIsBoss(actor PlayerState, family FamilyDefinition) bool {
	return flag(actor.Body.Flags[:], FamilyBossFlag) && family.Boss == actor.Body.Name && familyIDOf(actor.Body) == family.ID
}

func familyNewsResult(p FamilyNewsProposal) FamilyNewsResult {
	body := p.AfterBody
	if !p.AfterPresent {
		body = ""
	}
	return FamilyNewsResult{
		Action: p.Action, ActorID: p.ActorID, ActorName: p.ActorName,
		FamilyID: p.FamilyID, FamilyName: p.FamilyName, Body: body,
		Response: p.Response, Changed: p.Changed,
	}
}

// PlanFamilyNewsView is post.c:family_news with cmnd->num != 2.
func (s State) PlanFamilyNewsView(actorID string, catalog FamilyCatalog) (FamilyNewsResult, error) {
	actor, family, news, result, err := familyNewsMember(s, actorID, catalog)
	if err != nil {
		return FamilyNewsResult{}, err
	}
	_ = actor
	result.Action = FamilyNewsView
	if result.Response == FamilyNewsNotMemberResponse {
		return result, nil
	}
	body, ok := news.Bodies[family.ID]
	if !ok {
		result.Response = FamilyNewsMissingResponse
		return result, nil
	}
	result.Body = body
	result.Response = body
	return result, nil
}

// PlanFamilyNewsAppend is one newsedit write against the imported news body.
func (s State) PlanFamilyNewsAppend(actorID, line string, catalog FamilyCatalog) (FamilyNewsProposal, error) {
	if err := validateFamilyNewsLine(line); err != nil {
		return FamilyNewsProposal{}, err
	}
	actor, family, news, gated, err := familyNewsMember(s, actorID, catalog)
	if err != nil {
		return FamilyNewsProposal{}, err
	}
	if gated.Response == FamilyNewsNotMemberResponse {
		return FamilyNewsProposal{
			Action: FamilyNewsAppend, ActorID: actorID, ActorName: actor.Body.Name,
			Response: FamilyNewsNotMemberResponse, Changed: false,
			before: s.clone(), expectedActor: actor, expectedNews: news, afterNews: news,
			expectedCatalog: cloneFamilyCatalog(catalog),
		}, nil
	}
	truncated := truncateFamilyNewsLine(line)
	before, present := news.Bodies[family.ID]
	after := before
	if !present {
		after = FamilyNewsHeader
	}
	after += truncated + "\n"
	if err := validateFamilyNewsBody(after); err != nil {
		return FamilyNewsProposal{}, err
	}
	nextNews := news.Clone()
	nextNews.Bodies[family.ID] = after
	return FamilyNewsProposal{
		Action: FamilyNewsAppend, ActorID: actorID, ActorName: actor.Body.Name,
		FamilyID: family.ID, FamilyName: family.Name, Line: truncated,
		BeforeBody: before, AfterBody: after, BeforePresent: present, AfterPresent: true,
		Response: FamilyNewsAppendContinuePrompt, Changed: true,
		before: s.clone(), expectedActor: actor, expectedNews: news, afterNews: nextNews,
		expectedCatalog: cloneFamilyCatalog(catalog),
	}, nil
}

// PlanFamilyNewsDelete is the boss-only unlink branch of family_news.
func (s State) PlanFamilyNewsDelete(actorID string, catalog FamilyCatalog) (FamilyNewsProposal, error) {
	actor, family, news, gated, err := familyNewsMember(s, actorID, catalog)
	if err != nil {
		return FamilyNewsProposal{}, err
	}
	if gated.Response == FamilyNewsNotMemberResponse {
		return FamilyNewsProposal{
			Action: FamilyNewsDelete, ActorID: actorID, ActorName: actor.Body.Name,
			Response: FamilyNewsNotMemberResponse, Changed: false,
			before: s.clone(), expectedActor: actor, expectedNews: news, afterNews: news,
			expectedCatalog: cloneFamilyCatalog(catalog),
		}, nil
	}
	before, present := news.Bodies[family.ID]
	proposal := FamilyNewsProposal{
		Action: FamilyNewsDelete, ActorID: actorID, ActorName: actor.Body.Name,
		FamilyID: family.ID, FamilyName: family.Name,
		BeforeBody: before, AfterBody: before, BeforePresent: present, AfterPresent: present,
		Response: FamilyNewsDeletedResponse, Changed: false,
		before: s.clone(), expectedActor: actor, expectedNews: news, afterNews: news,
		expectedCatalog: cloneFamilyCatalog(catalog),
	}
	if !familyNewsIsBoss(actor, family) {
		proposal.Response = FamilyNewsNotBossResponse
		return proposal, nil
	}
	if !present {
		return proposal, nil
	}
	nextNews := news.Clone()
	delete(nextNews.Bodies, family.ID)
	proposal.AfterBody = ""
	proposal.AfterPresent = false
	proposal.Changed = true
	proposal.afterNews = nextNews
	proposal.Response = FamilyNewsDeletedResponse
	return proposal, nil
}

func familyNewsProposalMatches(actual, expected FamilyNewsProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID && actual.ActorName == expected.ActorName &&
		actual.FamilyID == expected.FamilyID && actual.FamilyName == expected.FamilyName && actual.Line == expected.Line &&
		actual.BeforeBody == expected.BeforeBody && actual.AfterBody == expected.AfterBody &&
		actual.BeforePresent == expected.BeforePresent && actual.AfterPresent == expected.AfterPresent &&
		actual.Response == expected.Response && actual.Changed == expected.Changed
}

// ApplyFamilyNews atomically applies a snapshot-bound append or delete.
// Unchanged gates (not-member, not-boss, already-empty delete) are receipts
// without Apply; calling Apply on them is rejected as an invalid proposal.
func (s State) ApplyFamilyNews(proposal FamilyNewsProposal) (State, FamilyNewsResult, error) {
	if proposal.ActorID == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, FamilyNewsResult{}, ErrFamilyNewsStaleProposal
	}
	if proposal.expectedCatalog.Families == nil || !proposal.Changed {
		return State{}, FamilyNewsResult{}, ErrFamilyNewsInvalidProposal
	}
	var fresh FamilyNewsProposal
	var err error
	switch proposal.Action {
	case FamilyNewsAppend:
		fresh, err = s.PlanFamilyNewsAppend(proposal.ActorID, proposal.Line, proposal.expectedCatalog)
	case FamilyNewsDelete:
		fresh, err = s.PlanFamilyNewsDelete(proposal.ActorID, proposal.expectedCatalog)
	default:
		return State{}, FamilyNewsResult{}, ErrFamilyNewsInvalidProposal
	}
	if err != nil {
		return State{}, FamilyNewsResult{}, ErrFamilyNewsStaleProposal
	}
	if !familyNewsProposalMatches(proposal, fresh) || !reflect.DeepEqual(proposal.expectedActor, fresh.expectedActor) ||
		!reflect.DeepEqual(proposal.expectedNews, fresh.expectedNews) || !reflect.DeepEqual(proposal.afterNews, fresh.afterNews) ||
		!reflect.DeepEqual(proposal.expectedCatalog, fresh.expectedCatalog) {
		return State{}, FamilyNewsResult{}, ErrFamilyNewsStaleProposal
	}
	next := s.clone()
	news := proposal.afterNews.Clone()
	next.FamilyNews = &news
	if err := next.Validate(); err != nil {
		return State{}, FamilyNewsResult{}, err
	}
	return next, familyNewsResult(proposal), nil
}

// WithFamilyNews installs a complete family-news aggregate into a valid
// snapshot. It is the only state-level migration seam for family_news_<n>.
func (s State) WithFamilyNews(news FamilyNewsState) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if err := news.Validate(); err != nil {
		return State{}, err
	}
	next := s.clone()
	copy := news.Clone()
	next.FamilyNews = &copy
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}
