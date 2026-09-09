package world

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"unicode"
	"unicode/utf8"
)

// These values are the source-backed vote gate from command11.c and
// mtype.h.  The C command derives a player's age from the accumulated
// LT_HOURS interval (starting at 18), and only permits ordinary players to
// vote at 21 or older.  INVINCIBLE and higher classes bypass that age check.
const (
	VoteElectionRoomFlag = 34 // RELECT
	VoteHoursTimerIndex  = 28 // LT_HOURS in the canonical timer array
	VoteInvincibleClass  = 9  // INVINCIBLE
	VoteBaseAge          = 18
	VoteMinimumAge       = 21
	VoteMaxSelections    = 79 // tempstr[1][0] = MIN(79, number)
	VoteMaxOptions       = 7  // vote_cmnd accepts only a..g
	VoteChoiceCount      = VoteMaxOptions
	VoteMaxTextBytes     = 1023 // vote_cmnd reads issue lines with char tmp[1024]
)

var (
	ErrVoteCatalogUnavailable = errors.New("vote issue catalog is unavailable")
	ErrVoteCatalogInvalid     = errors.New("vote issue catalog is invalid")
	ErrVoteActorAbsent        = errors.New("online canonical vote actor absent")
	ErrVoteAge                = errors.New("actor is below the vote age")
	ErrVoteRoom               = errors.New("actor is not in an election room")
	ErrVoteNumeric            = errors.New("vote age state is outside the canonical range")
	// State deliberately has no canonical per-player ballot/history field. A
	// successful vote would otherwise have to consult player/vote/<name>_v,
	// which is a legacy file authority and must not be guessed by this runtime.
	ErrVoteStateUnresolved = errors.New("canonical vote ballot state is unresolved")
	ErrVoteStaleProposal   = errors.New("stale vote proposal")
	ErrVoteInvalidProposal = errors.New("invalid vote proposal")
)

// Source-oriented aliases keep the missing ballot boundary discoverable to
// adapters that use the legacy file terminology.
var (
	ErrVoteHistoryUnresolved = ErrVoteStateUnresolved
	ErrVoteBallotUnresolved  = ErrVoteStateUnresolved
	ErrVoteStateMissing      = ErrVoteStateUnresolved
	ErrVoteIssueUnavailable  = ErrVoteCatalogUnavailable
	ErrVoteIssueInvalid      = ErrVoteCatalogInvalid
	ErrVoteActorUnavailable  = ErrVoteActorAbsent
	ErrVoteNotElectionRoom   = ErrVoteRoom
)

// These are the only actor-facing denial strings emitted by vote() before it
// touches the ISSUE file.  A success prompt is intentionally not provided:
// the source first checks a per-player vote file, whose authority is not in
// State, so emitting the "already voted" or first-choice prompt would be a
// fabricated branch.
const (
	VoteTooYoungResponse = "당신은 투표할 나이가 아닙니다.\n"
	VoteRoomResponse     = "투표소가 아닙니다.\n"
)

// VoteIssue is the structured equivalent of the current ISSUE resource:
// the first line's number is Number, the second line is Prompt, and the
// following Number lines are Options in source order. Option letters are not
// stored in the catalog; vote_cmnd derives A..G from this order and accepts
// exactly those letters from a continuation.
type VoteIssue struct {
	Number  int      `json:"number"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
}

// VoteCatalog is a server-owned immutable snapshot of the one current issue
// read from the legacy ISSUE resource. It is passed by value to reducers, and
// PlanVote copies its option slice before binding a proposal. A zero catalog
// means the resource has not been imported; it is never interpreted as an
// empty issue.
type VoteCatalog struct {
	Issue VoteIssue `json:"issue"`
}

// VoteOptionKey returns the source's accepted option letter for a zero-based
// option index. The zero byte denotes an index outside vote_cmnd's a..g
// input range.
func VoteOptionKey(index int) byte {
	if index < 0 || index >= VoteChoiceCount {
		return 0
	}
	return byte('A' + index)
}

func validVoteText(value string, required bool) bool {
	if !utf8.ValidString(value) || len(value) > VoteMaxTextBytes {
		return false
	}
	if required && value == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// Validate checks the complete server-owned issue/options resource. The
// legacy handler accepts only a..g for each choice and clamps the number of
// choice prompts to 79. Number must agree with the imported option lines;
// silently truncating a catalog would change the ballot question.
func (c VoteCatalog) Validate() error {
	issue := c.Issue
	if issue.Number < 1 || issue.Number > VoteMaxSelections || len(issue.Options) == 0 {
		if issue.Number == 0 && len(issue.Options) == 0 && issue.Prompt == "" {
			return ErrVoteCatalogUnavailable
		}
		return fmt.Errorf("%w: option count %d", ErrVoteCatalogInvalid, issue.Number)
	}
	if len(issue.Options) != issue.Number {
		return fmt.Errorf("%w: number=%d options=%d", ErrVoteCatalogInvalid, issue.Number, len(issue.Options))
	}
	if !validVoteText(issue.Prompt, true) {
		return fmt.Errorf("%w: invalid prompt", ErrVoteCatalogInvalid)
	}
	for index, option := range issue.Options {
		if !validVoteText(option, true) {
			return fmt.Errorf("%w: invalid option %d", ErrVoteCatalogInvalid, index+1)
		}
	}
	return nil
}

// cloneVoteCatalog validates and deep-copies the immutable catalog. Keeping
// this copy private prevents the caller's slice from changing a proposal
// after it has been planned.
func cloneVoteCatalog(catalog VoteCatalog) (VoteCatalog, error) {
	if err := catalog.Validate(); err != nil {
		return VoteCatalog{}, err
	}
	clone := catalog
	clone.Issue.Options = append([]string(nil), catalog.Issue.Options...)
	return clone, nil
}

// Clone returns an independent catalog copy for a connector or continuation
// owner that wants to retain the server-owned issue across a request.
func (c VoteCatalog) Clone() (VoteCatalog, error) { return cloneVoteCatalog(c) }

// Digest returns a stable identity for the exact validated issue/options
// snapshot. It is a request/continuation consistency token, not a ballot
// authority and is never accepted from a terminal client as permission.
func (c VoteCatalog) Digest() (string, error) {
	clone, err := cloneVoteCatalog(c)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// VoteIssueProjection is the read-only, source-backed portion of vote(). It
// proves the actor's age/room gates and snapshots the current issue. It does
// not claim that the actor has or has not voted, and it deliberately carries
// no actor-facing prompt until the ballot authority is migrated.
type VoteIssueProjection struct {
	Action        string    `json:"action"`
	ActorID       string    `json:"actor_id"`
	ActorName     string    `json:"actor_name"`
	RoomID        int16     `json:"room_id"`
	Issue         VoteIssue `json:"issue"`
	CatalogDigest string    `json:"catalog_digest"`
	Changed       bool      `json:"changed"`
}

// VoteProjection and VoteIssueResult are descriptive aliases for callers
// that use query/result terminology.
type VoteProjection = VoteIssueProjection
type VoteIssueResult = VoteIssueProjection

// VoteProposal is a snapshot-bound candidate for the part of vote() that can
// be proven from State. ApplyVote still fails closed because State has no
// per-player ballot/history field and therefore cannot atomically implement
// vote_cmnd's file existence, replacement, and final write branches.
type VoteProposal struct {
	Action        string
	ActorID       string
	ActorName     string
	RoomID        int16
	Issue         VoteIssue
	CatalogDigest string
	Changed       bool

	before          State
	expectedActor   PlayerState
	expectedRoom    RoomState
	expectedCatalog VoteCatalog
}

// VoteResult is kept as a typed shape for future ballot migration. No
// successful VoteResult is produced by ApplyVote in this bounded slice.
type VoteResult struct {
	Action        string    `json:"action"`
	ActorID       string    `json:"actor_id"`
	ActorName     string    `json:"actor_name"`
	RoomID        int16     `json:"room_id"`
	Issue         VoteIssue `json:"issue"`
	CatalogDigest string    `json:"catalog_digest"`
	Changed       bool      `json:"changed"`
	Response      string    `json:"response,omitempty"`
}

func (s State) voteActor(actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, RoomState{}, ErrVoteActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, ErrVoteActorAbsent
	}
	interval := int64(actor.Body.Timers[VoteHoursTimerIndex].Interval)
	if interval < 0 {
		return PlayerState{}, RoomState{}, ErrVoteNumeric
	}
	age := int64(VoteBaseAge) + interval/86400
	if actor.Body.Class < VoteInvincibleClass && age < VoteMinimumAge {
		return PlayerState{}, RoomState{}, ErrVoteAge
	}
	if !flag(room.Resource.Flags[:], VoteElectionRoomFlag) {
		return PlayerState{}, RoomState{}, ErrVoteRoom
	}
	return actor, room, nil
}

// PlanVote validates the source's deterministic gates and binds one immutable
// issue/options snapshot. It does not read any filesystem and does not infer
// prior ballot state from a player name.
func (s State) PlanVote(actorID string, catalog VoteCatalog) (VoteProposal, error) {
	actor, room, err := s.voteActor(actorID)
	if err != nil {
		return VoteProposal{}, err
	}
	canonicalCatalog, err := cloneVoteCatalog(catalog)
	if err != nil {
		return VoteProposal{}, err
	}
	digest, err := canonicalCatalog.Digest()
	if err != nil {
		return VoteProposal{}, err
	}
	return VoteProposal{
		Action:          "vote",
		ActorID:         actorID,
		ActorName:       actor.Body.Name,
		RoomID:          room.Resource.ID,
		Issue:           canonicalCatalog.Issue,
		CatalogDigest:   digest,
		Changed:         false,
		before:          s,
		expectedActor:   actor,
		expectedRoom:    room,
		expectedCatalog: canonicalCatalog,
	}, nil
}

// PlanVoteIssue is the source-name alias for PlanVote.
func (s State) PlanVoteIssue(actorID string, catalog VoteCatalog) (VoteProposal, error) {
	return s.PlanVote(actorID, catalog)
}

// ProjectVoteIssue exposes only the proof-backed read projection. This is
// useful to a connection-local continuation owner that wants to hold the
// issue/options snapshot while it asks the user for input; it is not itself a
// ballot and must never be treated as evidence that no old vote exists.
func (s State) ProjectVoteIssue(actorID string, catalog VoteCatalog) (VoteIssueProjection, error) {
	proposal, err := s.PlanVote(actorID, catalog)
	if err != nil {
		return VoteIssueProjection{}, err
	}
	return VoteIssueProjection{
		Action:        "vote-issue",
		ActorID:       proposal.ActorID,
		ActorName:     proposal.ActorName,
		RoomID:        proposal.RoomID,
		Issue:         proposal.Issue,
		CatalogDigest: proposal.CatalogDigest,
		Changed:       false,
	}, nil
}

// VoteIssue is the descriptive query alias used by some command adapters.
func (s State) VoteIssue(actorID string, catalog VoteCatalog) (VoteIssueProjection, error) {
	return s.ProjectVoteIssue(actorID, catalog)
}

// ApplyVote verifies a proposal against the same canonical snapshot, then
// rejects the mutation. The rejection is intentional: command11.c's final
// vote is a read/delete/write operation against player/vote/<name>_v, while
// State has no ballot/history field. Returning a successful no-op here would
// allow duplicate votes or silently lose an existing vote.
func (s State) ApplyVote(proposal VoteProposal) (State, VoteResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, VoteResult{}, err
	}
	if proposal.Action != "vote" || proposal.ActorID == "" || proposal.before.Version == 0 {
		return State{}, VoteResult{}, ErrVoteInvalidProposal
	}
	if !reflect.DeepEqual(s, proposal.before) {
		return State{}, VoteResult{}, ErrVoteStaleProposal
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !reflect.DeepEqual(actor, proposal.expectedActor) {
		return State{}, VoteResult{}, ErrVoteStaleProposal
	}
	room, ok := s.Rooms[proposal.RoomID]
	if !ok || !reflect.DeepEqual(room, proposal.expectedRoom) {
		return State{}, VoteResult{}, ErrVoteStaleProposal
	}
	catalog, err := cloneVoteCatalog(proposal.expectedCatalog)
	if err != nil {
		return State{}, VoteResult{}, ErrVoteInvalidProposal
	}
	digest, err := catalog.Digest()
	if err != nil || digest != proposal.CatalogDigest || !reflect.DeepEqual(catalog.Issue, proposal.Issue) || proposal.Changed {
		return State{}, VoteResult{}, ErrVoteInvalidProposal
	}
	return State{}, VoteResult{}, ErrVoteStateUnresolved
}

// ApplyVoteIssue is the descriptive alias for ApplyVote.
func (s State) ApplyVoteIssue(proposal VoteProposal) (State, VoteResult, error) {
	return s.ApplyVote(proposal)
}

// VoteContinuation describes only connection-local progress through
// vote_cmnd's case 1/case 2 prompts. It is deliberately not embedded in
// State, a receipt, or a catalog: user input is not authority and a dropped
// connection discards this value. A future ballot adapter must persist the
// final choice through a separate canonical field before case 3 can be
// enabled.
type VoteContinuation struct {
	CatalogDigest string
	IssueNumber   int
	NextOption    int
	Choices       []byte
}

// NewVoteContinuation starts at the first source option. It accepts only an
// issue projection already proven by PlanVote and copies all user-visible
// option progress into connection-local memory.
func NewVoteContinuation(projection VoteIssueProjection) (VoteContinuation, error) {
	if projection.Action != "vote-issue" || projection.CatalogDigest == "" || projection.Issue.Number < 1 || len(projection.Issue.Options) != projection.Issue.Number || projection.Issue.Number > VoteMaxSelections {
		return VoteContinuation{}, ErrVoteInvalidProposal
	}
	return VoteContinuation{
		CatalogDigest: projection.CatalogDigest,
		IssueNumber:   projection.Issue.Number,
		NextOption:    1,
		Choices:       make([]byte, 0, projection.Issue.Number),
	}, nil
}

// Choose records one source a..g choice without mutating State. done becomes
// true when the number of choices in ISSUE has been collected; even then the
// caller must not write a ballot until the canonical State field exists.
func (c VoteContinuation) Choose(input string) (next VoteContinuation, done bool, err error) {
	if c.CatalogDigest == "" || c.IssueNumber < 1 || c.IssueNumber > VoteMaxSelections || c.NextOption < 1 || c.NextOption > c.IssueNumber || len(c.Choices) != c.NextOption-1 {
		return VoteContinuation{}, false, ErrVoteInvalidProposal
	}
	if len(input) != 1 {
		return VoteContinuation{}, false, ErrVoteInvalidProposal
	}
	choice := input[0]
	if choice >= 'a' && choice <= 'g' {
		choice -= 'a' - 'A'
	}
	if choice < 'A' || choice > 'G' {
		return VoteContinuation{}, false, ErrVoteInvalidProposal
	}
	next = VoteContinuation{
		CatalogDigest: c.CatalogDigest,
		IssueNumber:   c.IssueNumber,
		NextOption:    c.NextOption + 1,
		Choices:       append(append([]byte(nil), c.Choices...), choice),
	}
	return next, c.NextOption == c.IssueNumber, nil
}
