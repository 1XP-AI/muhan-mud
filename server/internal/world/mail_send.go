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

const (
	// MaxMailSendLineBytes is the content budget used by postedit before it
	// writes a newline.  The legacy prompt says 80 characters, but the C code
	// copies at most 79 bytes and then appends the line separator.
	MaxMailSendLineBytes = 79

	// MailSendResponse is the actor-only completion text printed by postedit
	// after the line editor receives its terminating dot.
	MailSendResponse = "편지를 보냈습니다.\n"
)

var (
	ErrMailSendPayload            = errors.New("invalid canonical mail send payload")
	ErrMailSendRecipientRequired  = errors.New("mail recipient is required")
	ErrMailSendRecipientAbsent    = errors.New("mail recipient character is absent")
	ErrMailSendRecipientAmbiguous = errors.New("mail recipient name is ambiguous")
	ErrMailSendMailboxUnimported  = errors.New("mailbox migration is incomplete")
	ErrMailSendMailboxFull        = errors.New("mail recipient mailbox is full")
	ErrMailSendMessageIDAllocator = errors.New("mail message ID allocator is unavailable")
	ErrMailSendMessageIDInvalid   = errors.New("mail message ID is invalid")
	ErrMailSendMessageIDConflict  = errors.New("mail message ID already exists")
	ErrMailSendStaleProposal      = errors.New("stale mail send proposal")
	ErrMailSendInvalidProposal    = errors.New("invalid mail send proposal")
	ErrMailSendBodyLineTooLong    = errors.New("mail body line exceeds the legacy limit")
	ErrMailSendTimestampInvalid   = errors.New("mail send timestamp is invalid")
)

// MailMessageIDAllocator is owned by the persistence boundary.  It must
// allocate a globally unique, durable ID for the command and must not derive
// identity from a client-provided value.  PlanMailSend calls it only after all
// actor, recipient, room, body, migration and mailbox-capacity checks pass;
// ApplyMailSend never calls it.
type MailMessageIDAllocator func() (string, error)

// MailSendPayload is the canonical input produced by a future terminal
// editor. RecipientID is a canonical character ID, not a display name. An
// adapter that still receives the legacy name must first call
// State.ResolveMailRecipientID and put the resolved ID here. Body contains
// editor lines without the terminating dot; its final newline is normalized
// by PlanMailSend. Timestamp is injected by the command owner for deterministic
// tests and replay, rather than read from a global clock inside the reducer.
type MailSendPayload struct {
	RecipientID string    `json:"recipient_id"`
	Body        string    `json:"body"`
	Timestamp   time.Time `json:"timestamp"`
}

// MailSendResult is the durable actor receipt projection for one successful
// append. The body is deliberately not copied into the result so an adapter
// cannot accidentally publish private mail contents to an audit/event stream;
// the canonical body remains in State.Mailboxes and the committed snapshot.
type MailSendResult struct {
	Action        MailAction `json:"action"`
	ActorID       string     `json:"actor_id"`
	SenderID      string     `json:"sender_id"`
	SenderName    string     `json:"sender_name"`
	RecipientID   string     `json:"recipient_id"`
	RecipientName string     `json:"recipient_name"`
	MessageID     string     `json:"message_id"`
	Timestamp     time.Time  `json:"timestamp"`
	Changed       bool       `json:"changed"`
	Response      string     `json:"response"`
}

// MailSendProposal binds one canonical send to the exact actor, recipient,
// room and mailbox snapshot observed by PlanMailSend. Public payload fields
// are repeated for diagnostics, while private copies make tampering with a
// proposal fail closed in ApplyMailSend.
type MailSendProposal struct {
	Action      MailAction `json:"action"`
	ActorID     string     `json:"actor_id"`
	RecipientID string     `json:"recipient_id"`
	MessageID   string     `json:"message_id"`
	Body        string     `json:"body"`
	Timestamp   time.Time  `json:"timestamp"`

	expectedActor      PlayerState
	expectedRecipient  PlayerState
	expectedRoomFlags  [8]byte
	expectedMailboxes  bool
	expectedMailboxKey bool
	expectedMailbox    []MailMessage
	message            MailMessage
}

// CanonicalizeMailSendBody validates the editor payload and makes its storage
// representation deterministic. Each line is measured in bytes because the
// source uses strncpy(79), not a Unicode rune count. The immediate-dot source
// flow may send a header with no content; that case is represented by one empty
// canonical line ("\n") so it remains compatible with MailMessage validation.
func CanonicalizeMailSendBody(body string) (string, error) {
	if !utf8.ValidString(body) {
		return "", fmt.Errorf("%w: invalid UTF-8", ErrMailSendPayload)
	}
	if len(body) > MaxMailBodyBytes {
		return "", fmt.Errorf("%w: body exceeds %d bytes", ErrMailSendPayload, MaxMailBodyBytes)
	}
	for _, r := range body {
		if r == '\n' {
			continue
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", fmt.Errorf("%w: body contains a control character", ErrMailSendPayload)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if len(line) > MaxMailSendLineBytes {
			return "", fmt.Errorf("%w: line is %d bytes", ErrMailSendBodyLineTooLong, len(line))
		}
	}
	if body == "" {
		return "\n", nil
	}
	if !strings.HasSuffix(body, "\n") {
		if len(body) == MaxMailBodyBytes {
			return "", fmt.Errorf("%w: final newline exceeds body limit", ErrMailSendPayload)
		}
		body += "\n"
	}
	if err := ValidateMailBody(body); err != nil {
		return "", fmt.Errorf("%w: %v", ErrMailSendPayload, err)
	}
	return body, nil
}

// ValidateMailSendBody is the error-only form used by command adapters.
func ValidateMailSendBody(body string) error {
	_, err := CanonicalizeMailSendBody(body)
	return err
}

// ResolveMailRecipientID resolves the exact legacy target name to one
// canonical player ID. A canonical ID is accepted directly. Name lookup is
// retained for adapters porting postsend's name-based command, but duplicate
// display names are rejected instead of depending on Go map iteration order.
// NPCs, malformed characters and empty identifiers are never valid recipients.
func (s State) ResolveMailRecipientID(identifier string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	if !validMailToken(identifier, MaxMailMessageIDBytes) {
		return "", ErrMailSendRecipientRequired
	}
	if player, ok := s.Players[identifier]; ok {
		if player.Body.Type != 0 || !validMailDisplayName(player.Body.Name) {
			return "", ErrMailSendRecipientAbsent
		}
		return identifier, nil
	}
	resolved := ""
	for id, player := range s.Players {
		if player.Body.Type != 0 || player.Body.Name != identifier || !validMailDisplayName(player.Body.Name) {
			continue
		}
		if resolved != "" {
			return "", ErrMailSendRecipientAmbiguous
		}
		resolved = id
	}
	if resolved == "" {
		return "", ErrMailSendRecipientAbsent
	}
	return resolved, nil
}

// resolveMailSendRecipient requires a canonical target after resolving the
// legacy adapter's name. It is deliberately separate from actor validation so
// a client can never choose a sender or recipient by mutating a MailMessage.
func (s State) resolveMailSendRecipient(identifier string) (string, PlayerState, error) {
	id, err := s.ResolveMailRecipientID(identifier)
	if err != nil {
		return "", PlayerState{}, err
	}
	player := s.Players[id]
	if player.Body.Type != 0 || !validMailDisplayName(player.Body.Name) {
		return "", PlayerState{}, ErrMailSendRecipientAbsent
	}
	return id, player, nil
}

func normalizeMailSendTimestamp(timestamp time.Time) (time.Time, error) {
	if timestamp.IsZero() || timestamp.Before(time.Unix(0, 0)) {
		return time.Time{}, ErrMailSendTimestampInvalid
	}
	// Round(0) drops a process-local monotonic reading; UTC gives all command
	// owners the same serialized representation regardless of their location.
	return timestamp.Round(0).UTC(), nil
}

func (s State) mailMessageIDExists(id string) bool {
	for _, messages := range s.Mailboxes {
		for _, message := range messages {
			if message.ID == id {
				return true
			}
		}
	}
	return false
}

// PlanMailSend admits the canonical equivalent of postsend + postedit. It
// only accepts an already imported mailbox map: nil is an explicit migration
// marker and cannot be silently replaced, because doing so could hide legacy
// post/<name> contents. The future DB/session adapter must import that mailbox
// before exposing the compose flow.
func (s State) PlanMailSend(actorID string, payload MailSendPayload, allocate MailMessageIDAllocator) (MailSendProposal, error) {
	actor, room, err := s.mailActor(actorID)
	if err != nil {
		return MailSendProposal{}, err
	}
	if s.Mailboxes == nil {
		return MailSendProposal{}, ErrMailSendMailboxUnimported
	}
	if payload.RecipientID == "" {
		return MailSendProposal{}, ErrMailSendRecipientRequired
	}
	recipientID, recipient, err := s.resolveMailSendRecipient(payload.RecipientID)
	if err != nil {
		return MailSendProposal{}, err
	}
	body, err := CanonicalizeMailSendBody(payload.Body)
	if err != nil {
		return MailSendProposal{}, err
	}
	timestamp, err := normalizeMailSendTimestamp(payload.Timestamp)
	if err != nil {
		return MailSendProposal{}, err
	}
	mailbox, present := s.currentMailbox(recipientID)
	if len(mailbox) >= MaxMailboxSize {
		return MailSendProposal{}, ErrMailSendMailboxFull
	}
	if !present && len(s.Mailboxes) >= MaxMailboxes {
		return MailSendProposal{}, fmt.Errorf("%w: mailbox count limit", ErrMailSendMailboxFull)
	}
	if allocate == nil {
		return MailSendProposal{}, ErrMailSendMessageIDAllocator
	}
	messageID, err := allocate()
	if err != nil {
		return MailSendProposal{}, fmt.Errorf("%w: %w", ErrMailSendMessageIDAllocator, err)
	}
	if !validMailToken(messageID, MaxMailMessageIDBytes) {
		return MailSendProposal{}, ErrMailSendMessageIDInvalid
	}
	if s.mailMessageIDExists(messageID) {
		return MailSendProposal{}, ErrMailSendMessageIDConflict
	}
	message := MailMessage{
		ID:        messageID,
		SenderID:  actorID,
		Body:      body,
		Timestamp: timestamp,
	}
	if err := s.validateMailMessage(recipientID, message, make(map[string]bool)); err != nil {
		return MailSendProposal{}, fmt.Errorf("%w: %v", ErrMailSendPayload, err)
	}
	return MailSendProposal{
		Action:             MailSend,
		ActorID:            actorID,
		RecipientID:        recipientID,
		MessageID:          messageID,
		Body:               body,
		Timestamp:          timestamp,
		expectedActor:      actor,
		expectedRecipient:  recipient,
		expectedRoomFlags:  room.Resource.Flags,
		expectedMailboxes:  true,
		expectedMailboxKey: present,
		expectedMailbox:    copyMailMessages(mailbox),
		message:            message,
	}, nil
}

// PlanMailSendByName is the legacy-name convenience. It resolves the target
// with the same exact/ambiguous rules and then delegates to the canonical ID
// payload. The name is not stored as an authority field in the proposal.
func (s State) PlanMailSendByName(actorID, recipientName, body string, timestamp time.Time, allocate MailMessageIDAllocator) (MailSendProposal, error) {
	recipientID, err := s.ResolveMailRecipientID(recipientName)
	if err != nil {
		return MailSendProposal{}, err
	}
	return s.PlanMailSend(actorID, MailSendPayload{RecipientID: recipientID, Body: body, Timestamp: timestamp}, allocate)
}

func (s State) staleMailSendProposal(proposal MailSendProposal) (PlayerState, PlayerState, RoomState, []MailMessage, error) {
	if proposal.Action != MailSend || proposal.ActorID == "" || proposal.RecipientID == "" || proposal.MessageID == "" || proposal.message.ID == "" {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendInvalidProposal
	}
	actor, room, err := s.mailActor(proposal.ActorID)
	if err != nil {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, err
	}
	if s.Mailboxes == nil || !proposal.expectedMailboxes {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendMailboxUnimported
	}
	recipient, ok := s.Players[proposal.RecipientID]
	if !ok || recipient.Body.Type != 0 || !validMailDisplayName(recipient.Body.Name) {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendRecipientAbsent
	}
	if !reflect.DeepEqual(proposal.expectedActor, actor) ||
		!reflect.DeepEqual(proposal.expectedRecipient, recipient) ||
		proposal.expectedRoomFlags != room.Resource.Flags {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendStaleProposal
	}
	if proposal.message.SenderID != proposal.ActorID || proposal.message.Sender != "" ||
		proposal.message.ID != proposal.MessageID || proposal.message.Body != proposal.Body ||
		!proposal.message.Timestamp.Equal(proposal.Timestamp) || proposal.message.Timestamp.Location() != time.UTC {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendInvalidProposal
	}
	body, present := s.currentMailbox(proposal.RecipientID)
	if present != proposal.expectedMailboxKey || !reflect.DeepEqual(body, proposal.expectedMailbox) {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendStaleProposal
	}
	if _, err := CanonicalizeMailSendBody(proposal.Body); err != nil {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, err
	}
	timestamp, err := normalizeMailSendTimestamp(proposal.Timestamp)
	if err != nil || !timestamp.Equal(proposal.Timestamp) {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendInvalidProposal
	}
	if !validMailToken(proposal.MessageID, MaxMailMessageIDBytes) {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendMessageIDInvalid
	}
	if s.mailMessageIDExists(proposal.MessageID) {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendMessageIDConflict
	}
	if len(body) >= MaxMailboxSize {
		return PlayerState{}, PlayerState{}, RoomState{}, nil, ErrMailSendMailboxFull
	}
	return actor, recipient, room, body, nil
}

// ApplyMailSend appends exactly one immutable message to the recipient's
// ordered mailbox. It rechecks every authorization and snapshot condition and
// returns a cloned candidate. There is no allocator or clock call here, so a
// caller can safely retain the proposal while reconciling a durable receipt;
// the engine's command ID remains the replay/idempotency authority.
func (s State) ApplyMailSend(proposal MailSendProposal) (State, MailSendResult, error) {
	actor, recipient, _, mailbox, err := s.staleMailSendProposal(proposal)
	if err != nil {
		return State{}, MailSendResult{}, err
	}
	next := s.clone()
	if next.Mailboxes == nil {
		return State{}, MailSendResult{}, ErrMailSendMailboxUnimported
	}
	next.Mailboxes[proposal.RecipientID] = append(next.Mailboxes[proposal.RecipientID], proposal.message)
	if err := next.Validate(); err != nil {
		return State{}, MailSendResult{}, err
	}
	if len(next.Mailboxes[proposal.RecipientID]) != len(mailbox)+1 {
		return State{}, MailSendResult{}, ErrMailSendStaleProposal
	}
	return next, MailSendResult{
		Action:        MailSend,
		ActorID:       proposal.ActorID,
		SenderID:      proposal.ActorID,
		SenderName:    actor.Body.Name,
		RecipientID:   proposal.RecipientID,
		RecipientName: recipient.Body.Name,
		MessageID:     proposal.MessageID,
		Timestamp:     proposal.Timestamp,
		Changed:       true,
		Response:      MailSendResponse,
	}, nil
}

// SendMail is the direct pure-domain convenience for callers that do not
// need to retain a proposal. Durable command/session adapters should prefer
// PlanMailSend + ApplyMailSend so they can persist one receipt atomically.
func (s State) SendMail(actorID string, payload MailSendPayload, allocate MailMessageIDAllocator) (State, MailSendResult, error) {
	proposal, err := s.PlanMailSend(actorID, payload, allocate)
	if err != nil {
		return State{}, MailSendResult{}, err
	}
	return s.ApplyMailSend(proposal)
}
