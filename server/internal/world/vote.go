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
	// A nil State.Votes means the legacy player/vote/<name>_v domain has not
	// been imported.  Reducers never fall back to that file, because doing so
	// would make duplicate detection and replacement non-atomic.
	ErrVoteStateUnresolved = errors.New("canonical vote ballot state is unresolved")
	ErrVoteStateInvalid    = errors.New("canonical vote ballot state is invalid")
	ErrVoteBallotInvalid   = errors.New("canonical vote ballot is invalid")
	ErrVoteHistoryInvalid  = errors.New("canonical vote history is invalid")
	ErrVoteChoicesRequired = errors.New("vote choices are required")
	ErrVoteChoicesInvalid  = errors.New("vote choices are invalid")
	ErrVoteNoBallot        = errors.New("canonical vote ballot is absent")
	ErrVoteStaleProposal   = errors.New("stale vote proposal")
	ErrVoteInvalidProposal = errors.New("invalid vote proposal")
)

// Source-oriented aliases keep the missing ballot boundary discoverable to
// adapters that use the legacy file terminology.
var (
	ErrVoteHistoryUnresolved  = ErrVoteStateUnresolved
	ErrVoteBallotUnresolved   = ErrVoteStateUnresolved
	ErrVoteStateMissing       = ErrVoteStateUnresolved
	ErrVoteIssueUnavailable   = ErrVoteCatalogUnavailable
	ErrVoteIssueInvalid       = ErrVoteCatalogInvalid
	ErrVoteActorUnavailable   = ErrVoteActorAbsent
	ErrVoteNotElectionRoom    = ErrVoteRoom
	ErrVoteBallotStateInvalid = ErrVoteStateInvalid
	ErrVoteBallotInvalidState = ErrVoteStateInvalid
)

// These are the only actor-facing denial strings emitted by vote() before it
// touches the ISSUE file.  A success prompt is intentionally not provided:
// the source first checks a per-player vote file, whose authority is not in
// State, so emitting the "already voted" or first-choice prompt would be a
// fabricated branch.
const (
	VoteTooYoungResponse = "당신은 투표할 나이가 아닙니다.\n"
	VoteRoomResponse     = "투표소가 아닙니다.\n"
	// This is the source's case-3 response.  It is emitted only after the
	// canonical ballot transaction has produced the new state.
	VoteSubmittedResponse = "투표를 하였습니다.\n"
)

// VoteOperation names the state-level meaning of the legacy vote file
// accesses.  Read observes an existing ballot, Write creates one, Rewrite
// replaces one, and Delete removes one.  Rewrite is represented as one
// atomic canonical operation even though C performs unlink before collecting
// the replacement choices; this avoids losing an old ballot on a dropped
// connection while retaining the same final active-file result.
type VoteOperation string

// VoteAction is a descriptive alias for callers that name the operation as
// an action in a command/event envelope.
type VoteAction = VoteOperation

const (
	VoteOperationRead    VoteOperation = "read"
	VoteOperationWrite   VoteOperation = "write"
	VoteOperationRewrite VoteOperation = "rewrite"
	VoteOperationDelete  VoteOperation = "delete"

	// Descriptive aliases keep source/schema adapters from depending on one
	// spelling while all values remain the same closed set.
	VoteReadOperation    = VoteOperationRead
	VoteWriteOperation   = VoteOperationWrite
	VoteRewriteOperation = VoteOperationRewrite
	VoteDeleteOperation  = VoteOperationDelete
)

// VoteBallot is the canonical replacement for one player/vote/<name>_v
// active file.  The actor ID is the map key in VoteState.Ballots; keeping the
// digest with the raw A..G choices binds a migrated ballot to the exact ISSUE
// snapshot whose options were displayed.  The legacy file has no issue ID,
// so import must resolve that relationship before this state can be used.
type VoteBallot struct {
	CatalogDigest string `json:"catalog_digest"`
	Choices       []byte `json:"choices"`
}

// VoteHistoryEntry is the append-only state projection of vote file
// replacement.  It intentionally has no wall-clock or command ID: neither
// is present in command11.c's file, and those fields belong to the later
// mud_commands/mud_state_events schema.  Sequence is deterministic within a
// canonical VoteState and lets a DB adapter map each entry to an event row.
type VoteHistoryEntry struct {
	Sequence              uint64        `json:"sequence"`
	Operation             VoteOperation `json:"operation"`
	ActorID               string        `json:"actor_id"`
	CatalogDigest         string        `json:"catalog_digest"`
	PreviousCatalogDigest string        `json:"previous_catalog_digest,omitempty"`
	PreviousChoices       []byte        `json:"previous_choices,omitempty"`
	Choices               []byte        `json:"choices,omitempty"`
}

// VoteHistory is the concise source/schema spelling for a history entry.
type VoteHistory = VoteHistoryEntry

// VoteState is the explicit migration boundary for the legacy vote domain.
// A nil *VoteState on State means source authority is unresolved.  Once
// imported, Ballots and History must both be non-nil (empty is meaningful),
// so a missing map/slice cannot be mistaken for an empty vote population.
// Ballots is the current active projection; History is append-only and is
// checked against it whenever entries are present.
type VoteState struct {
	Ballots map[string]VoteBallot `json:"ballots"`
	History []VoteHistoryEntry    `json:"history"`
}

// VoteBallotState is a descriptive alias for callers that name the aggregate
// after its persisted row rather than the source command.
type VoteBallotState = VoteState

func cloneVoteChoices(choices []byte) []byte {
	if choices == nil {
		return nil
	}
	return append([]byte(nil), choices...)
}

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

func validVoteCatalogDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validateVoteChoices(choices []byte, required bool) error {
	if len(choices) == 0 {
		if required {
			return ErrVoteChoicesRequired
		}
		return nil
	}
	if len(choices) > VoteMaxSelections {
		return fmt.Errorf("%w: count %d", ErrVoteChoicesInvalid, len(choices))
	}
	for index, choice := range choices {
		if choice < 'A' || choice > 'G' {
			return fmt.Errorf("%w: choice %d", ErrVoteChoicesInvalid, index+1)
		}
	}
	return nil
}

// normalizeVoteChoices mirrors vote_cmnd's low()/up() handling. User input
// may use lowercase a..g, while every canonical ballot/history row stores
// uppercase bytes so replay and digest-bound receipts have one encoding.
func normalizeVoteChoices(choices []byte, required bool) ([]byte, error) {
	if len(choices) == 0 {
		if required {
			return nil, ErrVoteChoicesRequired
		}
		return nil, nil
	}
	if len(choices) > VoteMaxSelections {
		return nil, fmt.Errorf("%w: count %d", ErrVoteChoicesInvalid, len(choices))
	}
	normalized := cloneVoteChoices(choices)
	for index, choice := range normalized {
		if choice >= 'a' && choice <= 'g' {
			normalized[index] = choice - ('a' - 'A')
			continue
		}
		if choice < 'A' || choice > 'G' {
			return nil, fmt.Errorf("%w: choice %d", ErrVoteChoicesInvalid, index+1)
		}
	}
	return normalized, nil
}

func validateVoteBallot(ballot VoteBallot) error {
	if !validVoteCatalogDigest(ballot.CatalogDigest) {
		return fmt.Errorf("%w: issue digest", ErrVoteBallotInvalid)
	}
	if err := validateVoteChoices(ballot.Choices, true); err != nil {
		return fmt.Errorf("%w: %v", ErrVoteBallotInvalid, err)
	}
	return nil
}

func validateVoteHistoryEntry(entry VoteHistoryEntry, expectedSequence uint64) error {
	if entry.Sequence != expectedSequence {
		return fmt.Errorf("%w: sequence=%d expected=%d", ErrVoteHistoryInvalid, entry.Sequence, expectedSequence)
	}
	if entry.ActorID == "" || !validVoteCatalogDigest(entry.CatalogDigest) {
		return fmt.Errorf("%w: missing actor or issue digest", ErrVoteHistoryInvalid)
	}
	if entry.PreviousCatalogDigest != "" && !validVoteCatalogDigest(entry.PreviousCatalogDigest) {
		return fmt.Errorf("%w: invalid previous issue digest", ErrVoteHistoryInvalid)
	}
	switch entry.Operation {
	case VoteOperationWrite:
		if entry.PreviousCatalogDigest != "" || len(entry.PreviousChoices) != 0 {
			return fmt.Errorf("%w: write contains previous choices", ErrVoteHistoryInvalid)
		}
		if err := validateVoteChoices(entry.Choices, true); err != nil {
			return fmt.Errorf("%w: %v", ErrVoteHistoryInvalid, err)
		}
	case VoteOperationRewrite:
		if err := validateVoteChoices(entry.PreviousChoices, true); err != nil {
			return fmt.Errorf("%w: previous choices: %v", ErrVoteHistoryInvalid, err)
		}
		if err := validateVoteChoices(entry.Choices, true); err != nil {
			return fmt.Errorf("%w: %v", ErrVoteHistoryInvalid, err)
		}
	case VoteOperationDelete:
		if err := validateVoteChoices(entry.PreviousChoices, true); err != nil {
			return fmt.Errorf("%w: previous choices: %v", ErrVoteHistoryInvalid, err)
		}
		if len(entry.Choices) != 0 {
			return fmt.Errorf("%w: delete contains new choices", ErrVoteHistoryInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported operation %q", ErrVoteHistoryInvalid, entry.Operation)
	}
	return nil
}

// Validate checks the imported vote aggregate independently of State.  It
// treats nil Ballots/History as an unresolved import marker and requires
// contiguous history when history rows are present.  An imported snapshot may
// legitimately have no history yet (for example, a first canonical export
// containing active rows), but a non-empty history must replay exactly to the
// active map so duplicate or lost replacement rows cannot hide in JSON.
func (v VoteState) Validate() error {
	if v.Ballots == nil || v.History == nil {
		return ErrVoteStateUnresolved
	}
	for actorID, ballot := range v.Ballots {
		if actorID == "" {
			return fmt.Errorf("%w: empty ballot actor", ErrVoteStateInvalid)
		}
		if err := validateVoteBallot(ballot); err != nil {
			return fmt.Errorf("%w: actor %q: %v", ErrVoteStateInvalid, actorID, err)
		}
	}
	if len(v.History) == 0 {
		return nil
	}
	active := make(map[string]VoteBallot, len(v.Ballots))
	for index, entry := range v.History {
		sequence := uint64(index + 1)
		if err := validateVoteHistoryEntry(entry, sequence); err != nil {
			return err
		}
		previous, exists := active[entry.ActorID]
		switch entry.Operation {
		case VoteOperationWrite:
			if exists {
				return fmt.Errorf("%w: duplicate write for actor %q", ErrVoteHistoryInvalid, entry.ActorID)
			}
		case VoteOperationRewrite:
			if !exists || !reflect.DeepEqual(previous.Choices, entry.PreviousChoices) {
				return fmt.Errorf("%w: rewrite predecessor mismatch for actor %q", ErrVoteHistoryInvalid, entry.ActorID)
			}
		case VoteOperationDelete:
			if !exists || !reflect.DeepEqual(previous.Choices, entry.PreviousChoices) {
				return fmt.Errorf("%w: delete predecessor mismatch for actor %q", ErrVoteHistoryInvalid, entry.ActorID)
			}
		}
		if entry.Operation == VoteOperationDelete {
			delete(active, entry.ActorID)
		} else {
			active[entry.ActorID] = VoteBallot{CatalogDigest: entry.CatalogDigest, Choices: cloneVoteChoices(entry.Choices)}
		}
	}
	if !reflect.DeepEqual(active, v.Ballots) {
		return fmt.Errorf("%w: history does not match active ballots", ErrVoteHistoryInvalid)
	}
	return nil
}

// Clone returns an independent vote aggregate while preserving nil import
// markers. It deliberately does not validate; State.clone is used only after
// the source snapshot has already passed validation.
func (v VoteState) Clone() VoteState {
	next := VoteState{}
	if v.Ballots != nil {
		next.Ballots = make(map[string]VoteBallot, len(v.Ballots))
		for actorID, ballot := range v.Ballots {
			ballot.Choices = cloneVoteChoices(ballot.Choices)
			next.Ballots[actorID] = ballot
		}
	}
	if v.History != nil {
		next.History = make([]VoteHistoryEntry, len(v.History))
		for index, entry := range v.History {
			entry.PreviousChoices = cloneVoteChoices(entry.PreviousChoices)
			entry.Choices = cloneVoteChoices(entry.Choices)
			next.History[index] = entry
		}
	}
	return next
}

// WithVoteState installs a fully imported canonical vote aggregate into a
// world snapshot. It is the state-level migration seam for a future legacy
// vote exporter; callers must provide the complete active map and explicit
// history marker, and all ballot actors must already resolve to State.Players.
func (s State) WithVoteState(votes VoteState) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if err := votes.Validate(); err != nil {
		return State{}, err
	}
	next := s.clone()
	copy := votes.Clone()
	next.Votes = &copy
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// WithVotes is the concise alias used by state adapters.
func (s State) WithVotes(votes VoteState) (State, error) {
	return s.WithVoteState(votes)
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
// proves the actor's age/room gates and snapshots the current issue. When
// BallotStateResolved is false it does not claim that the actor has or has
// not voted; that is the nil-State.Votes migration boundary. When true,
// HasBallot and PreviousChoices are an exact active-ballot projection.
type VoteIssueProjection struct {
	Action              string    `json:"action"`
	ActorID             string    `json:"actor_id"`
	ActorName           string    `json:"actor_name"`
	RoomID              int16     `json:"room_id"`
	Issue               VoteIssue `json:"issue"`
	CatalogDigest       string    `json:"catalog_digest"`
	BallotStateResolved bool      `json:"ballot_state_resolved"`
	HasBallot           bool      `json:"has_ballot"`
	PreviousChoices     []byte    `json:"previous_choices,omitempty"`
	Changed             bool      `json:"changed"`
}

// VoteProjection and VoteIssueResult are descriptive aliases for callers
// that use query/result terminology.
type VoteProjection = VoteIssueProjection
type VoteIssueResult = VoteIssueProjection

// VoteProposal is a snapshot-bound candidate for one vote-file operation.
// PlanVote creates a read/projection proposal; PlanVoteWithChoices (or
// WithChoices) creates a write/rewrite proposal; PlanVoteDelete creates the
// explicit unlink operation. Private expectations make all three operations
// stale-safe and prevent a caller from changing a write into a delete by
// editing exported fields after planning.
type VoteProposal struct {
	Action                string
	Operation             VoteOperation
	ActorID               string
	ActorName             string
	RoomID                int16
	Issue                 VoteIssue
	CatalogDigest         string
	BallotStateResolved   bool
	HasBallot             bool
	PreviousCatalogDigest string
	PreviousChoices       []byte
	Choices               []byte
	Changed               bool

	before                 State
	expectedActor          PlayerState
	expectedRoom           RoomState
	expectedCatalog        VoteCatalog
	expectedBallot         VoteBallot
	expectedHasBallot      bool
	expectedResolved       bool
	expectedOperation      VoteOperation
	expectedPreviousDigest string
	expectedPrevious       []byte
	expectedChoices        []byte
}

// VoteResult is the deterministic actor-facing outcome of one canonical
// ballot operation. Choices and PreviousChoices are copied, so a receipt
// serializer cannot alias the mutable State snapshot.
type VoteResult struct {
	Action                string        `json:"action"`
	Operation             VoteOperation `json:"operation"`
	ActorID               string        `json:"actor_id"`
	ActorName             string        `json:"actor_name"`
	RoomID                int16         `json:"room_id"`
	Issue                 VoteIssue     `json:"issue"`
	CatalogDigest         string        `json:"catalog_digest"`
	BallotStateResolved   bool          `json:"ballot_state_resolved"`
	HadBallot             bool          `json:"had_ballot"`
	PreviousCatalogDigest string        `json:"previous_catalog_digest,omitempty"`
	PreviousChoices       []byte        `json:"previous_choices,omitempty"`
	Choices               []byte        `json:"choices,omitempty"`
	HistorySequence       uint64        `json:"history_sequence,omitempty"`
	Changed               bool          `json:"changed"`
	Response              string        `json:"response,omitempty"`
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
// issue/options snapshot. If Votes is imported it also performs the legacy
// case-0 ballot read against the canonical active map. If Votes is nil the
// proposal remains useful as a gate/projection, but BallotStateResolved is
// false and ApplyVote will fail closed before writing anything.
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
	before := s.clone()
	resolved := before.Votes != nil
	var ballot VoteBallot
	var hasBallot bool
	if resolved {
		ballot, hasBallot = before.Votes.Ballots[actorID]
	}
	operation := VoteOperationRead
	previousDigest := ""
	previous := []byte(nil)
	if hasBallot {
		previousDigest = ballot.CatalogDigest
		previous = cloneVoteChoices(ballot.Choices)
	}
	return VoteProposal{
		Action:                 "vote",
		Operation:              operation,
		ActorID:                actorID,
		ActorName:              actor.Body.Name,
		RoomID:                 room.Resource.ID,
		Issue:                  canonicalCatalog.Issue,
		CatalogDigest:          digest,
		BallotStateResolved:    resolved,
		HasBallot:              hasBallot,
		PreviousCatalogDigest:  previousDigest,
		PreviousChoices:        previous,
		Changed:                false,
		before:                 before,
		expectedActor:          before.Players[actorID],
		expectedRoom:           before.Rooms[room.Resource.ID],
		expectedCatalog:        canonicalCatalog,
		expectedBallot:         ballot,
		expectedHasBallot:      hasBallot,
		expectedResolved:       resolved,
		expectedOperation:      operation,
		expectedPreviousDigest: previousDigest,
		expectedPrevious:       cloneVoteChoices(previous),
	}, nil
}

// PlanVoteIssue is the source-name alias for PlanVote.
func (s State) PlanVoteIssue(actorID string, catalog VoteCatalog) (VoteProposal, error) {
	return s.PlanVote(actorID, catalog)
}

// WithChoices binds the final A..G sequence collected by VoteContinuation to
// a previously planned issue. It does not consult files or mutate State. A
// resolved existing ballot becomes Rewrite; an explicitly imported empty map
// becomes Write. An unresolved plan retains the deterministic choice payload
// but ApplyVote will reject it with ErrVoteStateUnresolved.
func (p VoteProposal) WithChoices(choices []byte) (VoteProposal, error) {
	if p.Action != "vote" || p.Issue.Number < 1 || len(p.Issue.Options) != p.Issue.Number || p.CatalogDigest == "" {
		return VoteProposal{}, ErrVoteInvalidProposal
	}
	normalized, err := normalizeVoteChoices(choices, true)
	if err != nil {
		return VoteProposal{}, err
	}
	if len(normalized) != p.Issue.Number {
		return VoteProposal{}, fmt.Errorf("%w: count=%d expected=%d", ErrVoteChoicesInvalid, len(normalized), p.Issue.Number)
	}
	next := p
	next.Choices = normalized
	next.Changed = false
	next.expectedChoices = cloneVoteChoices(normalized)
	if p.BallotStateResolved {
		if p.HasBallot {
			next.Operation = VoteOperationRewrite
		} else {
			next.Operation = VoteOperationWrite
		}
	} else {
		// This value is descriptive only until the State.Votes import marker is
		// resolved; ApplyVote checks the marker before any mutation.
		next.Operation = VoteOperationWrite
	}
	next.expectedOperation = next.Operation
	return next, nil
}

// BindChoices is a descriptive alias for WithChoices used by continuation
// owners that model the final user input as a payload binding step.
func (p VoteProposal) BindChoices(choices []byte) (VoteProposal, error) {
	return p.WithChoices(choices)
}

// PlanVoteWithChoices plans the source case-3 write after all continuation
// choices have been collected.  The catalog and active ballot are read from
// the same State snapshot, so a changed ISSUE or player ballot is stale at
// apply time rather than silently overwriting another vote.
func (s State) PlanVoteWithChoices(actorID string, catalog VoteCatalog, choices []byte) (VoteProposal, error) {
	proposal, err := s.PlanVote(actorID, catalog)
	if err != nil {
		return VoteProposal{}, err
	}
	return proposal.WithChoices(choices)
}

// PlanVoteBallot is the explicit aggregate spelling for
// PlanVoteWithChoices.
func (s State) PlanVoteBallot(actorID string, catalog VoteCatalog, choices []byte) (VoteProposal, error) {
	return s.PlanVoteWithChoices(actorID, catalog, choices)
}

// PlanVoteDelete plans the source unlink operation. It is primarily useful to
// an adapter that needs to represent the y-confirmation boundary separately;
// ordinary replacement should use PlanVoteWithChoices so delete+write is one
// canonical transaction. Missing active ballots are a deterministic no-op at
// ApplyVote, matching unlink on an already absent file.
func (s State) PlanVoteDelete(actorID string, catalog VoteCatalog) (VoteProposal, error) {
	proposal, err := s.PlanVote(actorID, catalog)
	if err != nil {
		return VoteProposal{}, err
	}
	proposal.Operation = VoteOperationDelete
	proposal.Choices = nil
	proposal.expectedChoices = nil
	proposal.expectedOperation = VoteOperationDelete
	return proposal, nil
}

// PlanVoteRewrite is a descriptive alias for the atomic replacement plan.
func (s State) PlanVoteRewrite(actorID string, catalog VoteCatalog, choices []byte) (VoteProposal, error) {
	proposal, err := s.PlanVoteWithChoices(actorID, catalog, choices)
	if err != nil {
		return VoteProposal{}, err
	}
	if proposal.BallotStateResolved && !proposal.HasBallot {
		return VoteProposal{}, ErrVoteNoBallot
	}
	return proposal, nil
}

// CurrentVoteBallot returns an independent active-ballot projection and an
// explicit presence bit. A nil State.Votes is unresolved rather than an empty
// result, so callers cannot accidentally treat a legacy file miss as a vote
// absence.
func (s State) CurrentVoteBallot(actorID string, catalog VoteCatalog) (VoteBallot, bool, error) {
	proposal, err := s.PlanVote(actorID, catalog)
	if err != nil {
		return VoteBallot{}, false, err
	}
	if !proposal.BallotStateResolved {
		return VoteBallot{}, false, ErrVoteStateUnresolved
	}
	if !proposal.HasBallot {
		return VoteBallot{}, false, nil
	}
	return VoteBallot{CatalogDigest: proposal.expectedBallot.CatalogDigest, Choices: cloneVoteChoices(proposal.expectedBallot.Choices)}, true, nil
}

// VoteBallot is the descriptive method spelling for CurrentVoteBallot.
func (s State) VoteBallot(actorID string, catalog VoteCatalog) (VoteBallot, bool, error) {
	return s.CurrentVoteBallot(actorID, catalog)
}

// VoteHistoryFor returns a stable, actor-filtered copy of the canonical
// history. It is a projection helper only; callers still need PlanVote for
// actor/room/ISSUE authorization before presenting a result.
func (s State) VoteHistoryFor(actorID string) ([]VoteHistoryEntry, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.Votes == nil {
		return nil, ErrVoteStateUnresolved
	}
	if actorID == "" {
		return nil, ErrVoteActorAbsent
	}
	history := make([]VoteHistoryEntry, 0)
	for _, entry := range s.Votes.History {
		if entry.ActorID != actorID {
			continue
		}
		entry.PreviousChoices = cloneVoteChoices(entry.PreviousChoices)
		entry.Choices = cloneVoteChoices(entry.Choices)
		history = append(history, entry)
	}
	return history, nil
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
		Action:              "vote-issue",
		ActorID:             proposal.ActorID,
		ActorName:           proposal.ActorName,
		RoomID:              proposal.RoomID,
		Issue:               proposal.Issue,
		CatalogDigest:       proposal.CatalogDigest,
		BallotStateResolved: proposal.BallotStateResolved,
		HasBallot:           proposal.HasBallot,
		PreviousChoices:     cloneVoteChoices(proposal.PreviousChoices),
		Changed:             false,
	}, nil
}

// VoteIssue is the descriptive query alias used by some command adapters.
func (s State) VoteIssue(actorID string, catalog VoteCatalog) (VoteIssueProjection, error) {
	return s.ProjectVoteIssue(actorID, catalog)
}

func voteResultFromProposal(proposal VoteProposal, operation VoteOperation, hadBallot bool, previousDigest string, previousChoices, choices []byte, sequence uint64, changed bool) VoteResult {
	return VoteResult{
		Action:                "vote",
		Operation:             operation,
		ActorID:               proposal.ActorID,
		ActorName:             proposal.ActorName,
		RoomID:                proposal.RoomID,
		Issue:                 cloneVoteIssue(proposal.Issue),
		CatalogDigest:         proposal.CatalogDigest,
		BallotStateResolved:   proposal.BallotStateResolved,
		HadBallot:             hadBallot,
		PreviousCatalogDigest: previousDigest,
		PreviousChoices:       cloneVoteChoices(previousChoices),
		Choices:               cloneVoteChoices(choices),
		HistorySequence:       sequence,
		Changed:               changed,
	}
}

func cloneVoteIssue(issue VoteIssue) VoteIssue {
	issue.Options = append([]string(nil), issue.Options...)
	return issue
}

func nextVoteHistorySequence(history []VoteHistoryEntry) (uint64, error) {
	if len(history) == 0 {
		return 1, nil
	}
	last := history[len(history)-1].Sequence
	if last == ^uint64(0) {
		return 0, fmt.Errorf("%w: sequence overflow", ErrVoteHistoryInvalid)
	}
	return last + 1, nil
}

func appendVoteHistory(votes *VoteState, entry VoteHistoryEntry) (uint64, error) {
	sequence, err := nextVoteHistorySequence(votes.History)
	if err != nil {
		return 0, err
	}
	entry.Sequence = sequence
	entry.PreviousChoices = cloneVoteChoices(entry.PreviousChoices)
	entry.Choices = cloneVoteChoices(entry.Choices)
	votes.History = append(votes.History, entry)
	return sequence, nil
}

// ApplyVote verifies a proposal against the same canonical snapshot and
// applies the explicit read/delete/write/rewrite operation. A nil State.Votes
// is the unresolved legacy-file boundary and remains fail-closed even when a
// proposal includes choices. With an imported VoteState, case-0 read returns a
// deterministic no-change result; case-1 y is represented by Delete when an
// adapter needs the source unlink separately, while the ordinary replacement
// path is one atomic Rewrite.
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
	if proposal.BallotStateResolved != proposal.expectedResolved || proposal.HasBallot != proposal.expectedHasBallot || proposal.PreviousCatalogDigest != proposal.expectedPreviousDigest || !reflect.DeepEqual(proposal.PreviousChoices, proposal.expectedPrevious) || !reflect.DeepEqual(proposal.Choices, proposal.expectedChoices) || proposal.Operation != proposal.expectedOperation {
		return State{}, VoteResult{}, ErrVoteInvalidProposal
	}
	if s.Votes == nil {
		return State{}, VoteResult{}, ErrVoteStateUnresolved
	}
	current, hasBallot := s.Votes.Ballots[proposal.ActorID]
	if hasBallot != proposal.HasBallot || (hasBallot && !reflect.DeepEqual(current, proposal.expectedBallot)) {
		return State{}, VoteResult{}, ErrVoteStaleProposal
	}
	if proposal.Operation == VoteOperationRead {
		var choices []byte
		if hasBallot {
			choices = current.Choices
		}
		previousDigest := ""
		if hasBallot {
			previousDigest = current.CatalogDigest
		}
		result := voteResultFromProposal(proposal, VoteOperationRead, hasBallot, previousDigest, choices, nil, 0, false)
		return s, result, nil
	}
	next := s.clone()
	if next.Votes == nil || next.Votes.Ballots == nil || next.Votes.History == nil {
		return State{}, VoteResult{}, ErrVoteStateUnresolved
	}
	switch proposal.Operation {
	case VoteOperationDelete:
		if !hasBallot {
			result := voteResultFromProposal(proposal, VoteOperationDelete, false, "", nil, nil, 0, false)
			return s, result, nil
		}
		delete(next.Votes.Ballots, proposal.ActorID)
		sequence, err := appendVoteHistory(next.Votes, VoteHistoryEntry{
			Operation:             VoteOperationDelete,
			ActorID:               proposal.ActorID,
			CatalogDigest:         proposal.CatalogDigest,
			PreviousCatalogDigest: current.CatalogDigest,
			PreviousChoices:       current.Choices,
		})
		if err != nil {
			return State{}, VoteResult{}, err
		}
		result := voteResultFromProposal(proposal, VoteOperationDelete, true, current.CatalogDigest, current.Choices, nil, sequence, true)
		if err := next.Validate(); err != nil {
			return State{}, VoteResult{}, err
		}
		return next, result, nil
	case VoteOperationWrite, VoteOperationRewrite:
		if err := validateVoteChoices(proposal.Choices, true); err != nil || len(proposal.Choices) != proposal.Issue.Number {
			return State{}, VoteResult{}, ErrVoteInvalidProposal
		}
		if proposal.Operation == VoteOperationWrite && hasBallot {
			return State{}, VoteResult{}, ErrVoteStaleProposal
		}
		if proposal.Operation == VoteOperationRewrite && !hasBallot {
			return State{}, VoteResult{}, ErrVoteStaleProposal
		}
		previous := []byte(nil)
		if hasBallot {
			previous = current.Choices
		}
		next.Votes.Ballots[proposal.ActorID] = VoteBallot{CatalogDigest: proposal.CatalogDigest, Choices: cloneVoteChoices(proposal.Choices)}
		previousDigest := ""
		if hasBallot {
			previousDigest = current.CatalogDigest
		}
		history := VoteHistoryEntry{
			Operation:             proposal.Operation,
			ActorID:               proposal.ActorID,
			CatalogDigest:         proposal.CatalogDigest,
			PreviousCatalogDigest: previousDigest,
			PreviousChoices:       previous,
			Choices:               proposal.Choices,
		}
		sequence, err := appendVoteHistory(next.Votes, history)
		if err != nil {
			return State{}, VoteResult{}, err
		}
		result := voteResultFromProposal(proposal, proposal.Operation, hasBallot, previousDigest, previous, proposal.Choices, sequence, true)
		result.Response = VoteSubmittedResponse
		if err := next.Validate(); err != nil {
			return State{}, VoteResult{}, err
		}
		return next, result, nil
	default:
		return State{}, VoteResult{}, ErrVoteInvalidProposal
	}
}

// ApplyVoteIssue is the descriptive alias for ApplyVote.
func (s State) ApplyVoteIssue(proposal VoteProposal) (State, VoteResult, error) {
	return s.ApplyVote(proposal)
}

// VoteContinuation describes only connection-local progress through
// vote_cmnd's case 1/case 2 prompts. It is deliberately not embedded in
// State, a receipt, or a catalog: user input is not authority and a dropped
// connection discards this value. A caller must bind the final choice through
// PlanVoteWithChoices before case 3 can be applied.
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
// caller must bind the sequence to a snapshot-bound proposal before writing.
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
