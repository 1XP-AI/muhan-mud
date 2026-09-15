package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The legacy noteedit buffer keeps at most 79 bytes before adding its newline.
// The Go projection stores complete lines without their newline terminator so
// JSON cannot contain an ambiguous partial line.
const (
	MaxNotepadLineBytes = 79
	MaxNotepadLines     = 100000
	MaxNotepadBytes     = 1 << 20

	NotepadHeaderLine = "            === DM Notepad ==="
	NotepadHeader     = NotepadHeaderLine + "\n\n"

	NotepadAppendPrompt          = "DM notepad:\n->"
	NotepadAppendContinuePrompt  = "->"
	NotepadAppendResponse        = "Message appended.\n"
	NotepadClearResponse         = "Clearing DM notepad\n"
	NotepadInvalidOptionResponse = "invalid option.\n"
	NotepadMissingResponse       = "화일을 읽을 수 없습니다.\n"
)

var (
	ErrNotepadStateUnresolved    = errors.New("notepad state migration is incomplete")
	ErrNotepadStateInvalid       = errors.New("invalid notepad state")
	ErrNotepadActorAbsent        = errors.New("online canonical notepad actor absent")
	ErrNotepadUnauthorized       = errors.New("notepad requires caretaker authorization")
	ErrNotepadInvalidVerb        = errors.New("invalid notepad verb")
	ErrNotepadInvalidOption      = errors.New("invalid notepad option")
	ErrNotepadAppendContinuation = errors.New("notepad append requires a connection-local continuation")
	ErrNotepadLineInvalid        = errors.New("invalid notepad line")
	ErrNotepadLineTooLong        = errors.New("notepad line exceeds the byte limit")
	ErrNotepadLimit              = errors.New("notepad exceeds the byte limit")
	ErrNotepadStaleProposal      = errors.New("stale notepad proposal")
	ErrNotepadInvalidProposal    = errors.New("invalid notepad proposal")
)

// Source-oriented aliases keep the bounded adapter discoverable without
// introducing a second state representation.
var (
	ErrNotepadContinuationRequired = ErrNotepadAppendContinuation
	ErrNotepadLineTooLarge         = ErrNotepadLineTooLong
)

type NotepadAction string

const (
	NotepadUnknown NotepadAction = "unknown"
	NotepadView    NotepadAction = "view"
	NotepadAppend  NotepadAction = "append"
	NotepadClear   NotepadAction = "clear"
	NotepadInvalid NotepadAction = "invalid"
)

// NotepadResult is the durable receipt projection. Lines are copied from the
// canonical State and contain no client path, file descriptor, or credential.
type NotepadResult struct {
	Action   NotepadAction `json:"action"`
	ActorID  string        `json:"actor_id"`
	Verb     string        `json:"verb"`
	Option   string        `json:"option,omitempty"`
	Lines    []string      `json:"lines,omitempty"`
	Body     string        `json:"body,omitempty"`
	Response string        `json:"response"`
	Changed  bool          `json:"changed"`
}

// NotepadProposal binds one view/clear/append to one complete State snapshot.
// The private fields prevent a caller from manufacturing authority or
// replacing a concurrent append after planning.
type NotepadProposal struct {
	Action    NotepadAction
	ActorID   string
	ActorName string
	Verb      string
	Option    string
	Lines     []string
	Body      string
	Response  string
	Changed   bool

	before        State
	expectedActor PlayerState
	inputLines    []string
	afterLines    []string
}

func NotepadUnknownResponse(verb string) string {
	return fmt.Sprintf("\"%s\": 이런 명령어는 없네요.", verb)
}

func notepadVerbOK(verb string) bool {
	return verb == "*notepad" || verb == "*메모"
}

func validNotepadLine(line string) error {
	if !utf8.ValidString(line) || strings.ContainsRune(line, '\n') || strings.ContainsRune(line, '\r') {
		return ErrNotepadLineInvalid
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrNotepadLineInvalid
		}
	}
	return nil
}

// ValidateNotepadLine checks one connection-local line. Lines longer than the
// legacy buffer are valid input and are truncated by TruncateNotepadLine,
// matching noteedit's strncpy(outstr, str, 79) boundary.
func ValidateNotepadLine(line string) error {
	return validNotepadLine(line)
}

// TruncateNotepadLine applies the legacy byte limit without splitting a UTF-8
// sequence. The source can split bytes, but the canonical JSON boundary must
// remain valid UTF-8, so the longest valid prefix is retained.
func TruncateNotepadLine(line string) string {
	if !utf8.ValidString(line) {
		return ""
	}
	if len(line) <= MaxNotepadLineBytes {
		return line
	}
	for i := MaxNotepadLineBytes; i >= 0; i-- {
		if utf8.ValidString(line[:i]) {
			return line[:i]
		}
	}
	return ""
}

func copyNotepadLines(lines []string) []string {
	if lines == nil {
		return nil
	}
	return append([]string{}, lines...)
}

func notepadBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line) + 1
	}
	return total
}

func validateNotepadLines(lines []string) error {
	if len(lines) > MaxNotepadLines || notepadBytes(lines) > MaxNotepadBytes {
		return ErrNotepadLimit
	}
	for _, line := range lines {
		if err := validNotepadLine(line); err != nil {
			return err
		}
		if len(line) > MaxNotepadLineBytes {
			return ErrNotepadLineTooLong
		}
	}
	return nil
}

// validateNotepad is called from State.Validate. Nil remains the explicit
// pre-migration marker, while an empty nonnil slice means the canonical file
// is absent and can be created by the next append.
func (s State) validateNotepad() error {
	if s.Notepad == nil {
		return nil
	}
	if err := validateNotepadLines(s.Notepad); err != nil {
		return fmt.Errorf("%w: %v", ErrNotepadStateInvalid, err)
	}
	return nil
}

func renderNotepad(lines []string) string {
	if len(lines) == 0 {
		return NotepadMissingResponse
	}
	return strings.Join(lines, "\n") + "\n"
}

func validNotepadActorName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func notepadActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, fmt.Errorf("%w: %v", ErrNotepadStateInvalid, err)
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validNotepadActorName(actor.Body.Name) {
		return PlayerState{}, ErrNotepadActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrNotepadActorAbsent
	}
	return actor, nil
}

func notepadOption(options []string) (string, error) {
	if len(options) > 1 {
		return "", ErrNotepadInvalidOption
	}
	if len(options) == 0 || options[0] == "" {
		return "", nil
	}
	if err := validNotepadLine(options[0]); err != nil || strings.IndexFunc(options[0], unicode.IsSpace) >= 0 {
		return "", ErrNotepadInvalidOption
	}
	return options[0], nil
}

func notepadOptionRune(option string) rune {
	if option == "" {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(option)
	if r >= 'A' && r <= 'Z' {
		r += 'a' - 'A'
	}
	return r
}

func notepadBaseProposal(s State, actor PlayerState, actorID, verb, option string) NotepadProposal {
	return NotepadProposal{
		ActorID: actorID, ActorName: actor.Body.Name, Verb: verb, Option: option,
		before: s.clone(), expectedActor: actor,
	}
}

// PlanNotepad is the non-interactive post.c:notepad branch. The variadic
// option accepts no option for view or one legacy option; a callers' `a`
// branch is intentionally only a continuation start and performs no write.
func (s State) PlanNotepad(actorID, verb string, options ...string) (NotepadProposal, error) {
	if !notepadVerbOK(verb) {
		return NotepadProposal{}, ErrNotepadInvalidVerb
	}
	actor, err := notepadActor(s, actorID)
	if err != nil {
		return NotepadProposal{}, err
	}
	option, err := notepadOption(options)
	if err != nil {
		return NotepadProposal{}, err
	}
	proposal := notepadBaseProposal(s, actor, actorID, verb, option)
	if actor.Body.Class < playerCaretakerClass {
		proposal.Action = NotepadUnknown
		proposal.Lines = []string{}
		proposal.Response = NotepadUnknownResponse(verb)
		return proposal, nil
	}
	if s.Notepad == nil {
		return NotepadProposal{}, ErrNotepadStateUnresolved
	}
	proposal.Lines = copyNotepadLines(s.Notepad)
	proposal.Body = renderNotepad(s.Notepad)
	switch notepadOptionRune(option) {
	case 0:
		proposal.Action = NotepadView
		proposal.Response = proposal.Body
	case 'a':
		proposal.Action = NotepadAppend
		proposal.Response = NotepadAppendPrompt
	case 'd':
		proposal.Action = NotepadClear
		proposal.Response = NotepadClearResponse
		proposal.Changed = len(s.Notepad) != 0
		proposal.afterLines = []string{}
	default:
		proposal.Action = NotepadInvalid
		proposal.Response = NotepadInvalidOptionResponse
	}
	return proposal, nil
}

// AuthorizeNotepad is the connection-local start gate. It resolves the
// canonical actor from the loaded world snapshot and never accepts a client
// class, path, or credential as an authority input.
func (s State) AuthorizeNotepad(actorID string) error {
	actor, err := notepadActor(s, actorID)
	if err != nil {
		return err
	}
	if actor.Body.Class < playerCaretakerClass {
		return ErrNotepadUnauthorized
	}
	if s.Notepad == nil {
		return ErrNotepadStateUnresolved
	}
	return nil
}

func planNotepadAppend(s State, actorID, verb string, lines []string) (NotepadProposal, error) {
	if !notepadVerbOK(verb) {
		return NotepadProposal{}, ErrNotepadInvalidVerb
	}
	actor, err := notepadActor(s, actorID)
	if err != nil {
		return NotepadProposal{}, err
	}
	proposal := notepadBaseProposal(s, actor, actorID, verb, "a")
	if actor.Body.Class < playerCaretakerClass {
		proposal.Action = NotepadUnknown
		proposal.Lines = []string{}
		proposal.Response = NotepadUnknownResponse(verb)
		return proposal, nil
	}
	if s.Notepad == nil {
		return NotepadProposal{}, ErrNotepadStateUnresolved
	}
	canonicalLines := make([]string, len(lines))
	for i, line := range lines {
		if err := ValidateNotepadLine(line); err != nil {
			return NotepadProposal{}, err
		}
		canonicalLines[i] = TruncateNotepadLine(line)
	}
	after := copyNotepadLines(s.Notepad)
	if len(canonicalLines) != 0 {
		if len(after) == 0 {
			after = append(after, NotepadHeaderLine, "")
		}
		after = append(after, canonicalLines...)
	}
	if err := validateNotepadLines(after); err != nil {
		return NotepadProposal{}, err
	}
	proposal.Action = NotepadAppend
	proposal.Lines = after
	proposal.Body = renderNotepad(after)
	proposal.Response = NotepadAppendResponse
	proposal.Changed = len(canonicalLines) != 0
	proposal.inputLines = copyNotepadLines(canonicalLines)
	proposal.afterLines = copyNotepadLines(after)
	return proposal, nil
}

// PlanNotepadAppend plans the entire connection-local editor buffer at the
// durable command boundary. An empty buffer preserves C's successful `.`
// response without creating a missing file.
func (s State) PlanNotepadAppend(actorID string, lines []string) (NotepadProposal, error) {
	return planNotepadAppend(s, actorID, "*notepad", lines)
}

// PlanNotepadAppendWithVerb binds the exact alias used to start the editor.
func (s State) PlanNotepadAppendWithVerb(actorID, verb string, lines []string) (NotepadProposal, error) {
	return planNotepadAppend(s, actorID, verb, lines)
}

func notepadProposalMatches(a, b NotepadProposal) bool {
	return a.Action == b.Action && a.ActorID == b.ActorID && a.ActorName == b.ActorName &&
		a.Verb == b.Verb && a.Option == b.Option && reflect.DeepEqual(a.Lines, b.Lines) &&
		a.Body == b.Body && a.Response == b.Response && a.Changed == b.Changed &&
		reflect.DeepEqual(a.inputLines, b.inputLines) && reflect.DeepEqual(a.afterLines, b.afterLines) &&
		reflect.DeepEqual(a.expectedActor, b.expectedActor)
}

func notepadResult(p NotepadProposal) NotepadResult {
	return NotepadResult{
		Action: p.Action, ActorID: p.ActorID, Verb: p.Verb, Option: p.Option,
		Lines: copyNotepadLines(p.Lines), Body: p.Body, Response: p.Response, Changed: p.Changed,
	}
}

// ApplyNotepad rechecks the exact planning snapshot and applies one clear or
// one complete append. Read-only and invalid/unauthorized branches remain
// unchanged receipts; retries are handled by the command store before this
// reducer is called again.
func (s State) ApplyNotepad(proposal NotepadProposal) (State, NotepadResult, error) {
	if proposal.ActorID == "" || proposal.before.Version == 0 {
		return State{}, NotepadResult{}, ErrNotepadInvalidProposal
	}
	if !reflect.DeepEqual(s, proposal.before) {
		return State{}, NotepadResult{}, ErrNotepadStaleProposal
	}
	var fresh NotepadProposal
	var err error
	switch proposal.Action {
	case NotepadAppend:
		if proposal.Response == NotepadAppendPrompt {
			return State{}, NotepadResult{}, ErrNotepadInvalidProposal
		}
		if proposal.inputLines == nil {
			return State{}, NotepadResult{}, ErrNotepadInvalidProposal
		}
		fresh, err = s.PlanNotepadAppendWithVerb(proposal.ActorID, proposal.Verb, proposal.inputLines)
	case NotepadView, NotepadClear, NotepadInvalid, NotepadUnknown:
		if proposal.Option == "" {
			fresh, err = s.PlanNotepad(proposal.ActorID, proposal.Verb)
		} else {
			fresh, err = s.PlanNotepad(proposal.ActorID, proposal.Verb, proposal.Option)
		}
	default:
		return State{}, NotepadResult{}, ErrNotepadInvalidProposal
	}
	if err != nil {
		return State{}, NotepadResult{}, err
	}
	if !notepadProposalMatches(proposal, fresh) {
		return State{}, NotepadResult{}, ErrNotepadStaleProposal
	}
	if !proposal.Changed {
		return s, notepadResult(proposal), nil
	}
	next := s.clone()
	next.Notepad = copyNotepadLines(proposal.afterLines)
	if err := next.Validate(); err != nil {
		return State{}, NotepadResult{}, err
	}
	return next, notepadResult(proposal), nil
}

// AppendNotepad is the pure convenience form used by import and unit callers.
func (s State) AppendNotepad(actorID string, lines []string) (State, NotepadResult, error) {
	proposal, err := s.PlanNotepadAppend(actorID, lines)
	if err != nil {
		return State{}, NotepadResult{}, err
	}
	return s.ApplyNotepad(proposal)
}

// WithNotepad installs a complete imported line projection. A nil slice is
// rejected so callers cannot accidentally turn a canonical empty file back
// into the unresolved migration marker.
func (s State) WithNotepad(lines []string) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if lines == nil {
		return State{}, ErrNotepadStateUnresolved
	}
	if err := validateNotepadLines(lines); err != nil {
		return State{}, err
	}
	next := s.clone()
	next.Notepad = copyNotepadLines(lines)
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// NotepadText renders a previously validated canonical projection. It is
// intentionally response-only and never opens or names a client file.
func NotepadText(lines []string) string {
	return renderNotepad(lines)
}
