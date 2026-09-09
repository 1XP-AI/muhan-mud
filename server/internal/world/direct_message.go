package world

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	playerIgnoreAllSendFlag = 35 // PIGNOR
	// command4.c stores the complete input in a 256-byte buffer and reserves
	// the final byte for NUL. Direct-message text keeps that source boundary.
	MaxDirectMessageTextBytes = 255
)

// DirectMessageProposal is the pure candidate for command4.c:sendman. The
// canonical Go player model has no serialized first_ignore/talksend fields,
// so this proposal intentionally carries no durable last-message mutation.
// Selector is retained so ApplyDirectMessage can re-resolve the same target
// and reject a stale candidate if the online/visibility set changed.
type DirectMessageProposal struct {
	SenderID   string
	Selector   string
	TargetID   string
	SenderName string
	TargetName string
	Text       string
	Response   string
	Delivered  bool
}

// DirectMessageResult is the deterministic actor response and post-commit
// recipient projection. Event is persisted in the command receipt by the
// session boundary; it is not added to State or broadcast during reduction.
type DirectMessageResult struct {
	Response  string              `json:"response"`
	Delivered bool                `json:"delivered"`
	Event     *DirectMessageEvent `json:"event,omitempty"`
}

// DirectMessageEvent is a recipient-specific projection. Text is already the
// fixed-format target message, so a future transport does not need to rerun
// target selection or invent a display formatter on replay.
type DirectMessageEvent struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
	Text       string `json:"text"`
}

// ValidateDirectMessageText checks the line-oriented payload before JSON
// marshaling. Invalid/control text must fail closed instead of becoming a
// terminal or protocol injection through the recipient event.
func ValidateDirectMessageText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("invalid direct message text")
	}
	if len(text) > MaxDirectMessageTextBytes {
		return fmt.Errorf("direct message text exceeds the byte limit")
	}
	for _, r := range text {
		if invalidDirectMessageRune(r) {
			return fmt.Errorf("invalid direct message text")
		}
	}
	return nil
}

func validDirectMessageName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if invalidDirectMessageRune(r) {
			return false
		}
	}
	return true
}

func invalidDirectMessageRune(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

func validDirectMessageSelector(selector string) bool {
	return selector != "" && utf8.ValidString(selector) && strings.TrimSpace(selector) == selector && validDirectMessageName(selector)
}

// directMessageTargetVisible applies the source sendman gates that depend on
// the sender. DM-invisible candidates are filtered during lookup for ordinary
// senders; ordinary invisibility is checked after exact/prefix selection so a
// selected hidden target returns the same generic unavailable response.
func directMessageTargetVisible(sender, target PlayerState) bool {
	if sender.Body.Class < playerDMClass && flag(target.Body.Flags[:], playerInvisibleFlag) && !flag(sender.Body.Flags[:], playerDetectFlag) {
		return false
	}
	return true
}

func directMessageTargetSort(s State, ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		left, right := s.Players[ids[i]].Body.Name, s.Players[ids[j]].Body.Name
		leftFold, rightFold := strings.ToLower(left), strings.ToLower(right)
		if leftFold != rightFold {
			return leftFold < rightFold
		}
		if left != right {
			return left < right
		}
		return ids[i] < ids[j]
	})
}

// selectDirectMessageTarget resolves an online player globally, as sendman
// does, with exact-name precedence and deterministic prefix fallback. The
// original descriptor scan is not part of State, so equal candidates are
// ordered by folded display name and canonical identity rather than map order.
func (s State) selectDirectMessageTarget(senderID, selector string) (string, error) {
	sender, ok := s.Players[senderID]
	if !ok || !sender.Online || sender.Body.Type != 0 {
		return "", fmt.Errorf("online direct-message sender absent")
	}
	exact := make([]string, 0)
	prefix := make([]string, 0)
	for id, player := range s.Players {
		if !player.Online || player.Body.Type != 0 || !validDirectMessageName(player.Body.Name) {
			continue
		}
		if sender.Body.Class < playerDMClass && flag(player.Body.Flags[:], playerDMInvisibleFlag) {
			continue
		}
		switch {
		case strings.EqualFold(player.Body.Name, selector):
			exact = append(exact, id)
		case equalFoldPrefix(player.Body.Name, selector):
			prefix = append(prefix, id)
		}
	}
	if len(exact) > 0 {
		directMessageTargetSort(s, exact)
		return exact[0], nil
	}
	if len(prefix) > 0 {
		directMessageTargetSort(s, prefix)
		return prefix[0], nil
	}
	return "", fmt.Errorf("direct-message target absent")
}

func directMessageResponse(sender, target PlayerState, text string) string {
	if flag(sender.Body.Flags[:], playerLocalEchoFlag) {
		return fmt.Sprintf("당신은 %s님에게 \"%s\"라고 이야기합니다.\r\n", target.Body.Name, text)
	}
	return fmt.Sprintf("%s님에게 말을 전달하였습니다.\r\n", target.Body.Name)
}

// PlanDirectMessage resolves all authority and listener gates without mutating
// the snapshot. Empty text, receiver listen rejection, and sender silence are
// successful no-op candidates so their source-compatible responses can be
// replayed through the same durable command boundary.
func (s State) PlanDirectMessage(senderID, selector, text string) (DirectMessageProposal, error) {
	if err := s.Validate(); err != nil {
		return DirectMessageProposal{}, err
	}
	if !validDirectMessageSelector(selector) {
		return DirectMessageProposal{}, fmt.Errorf("direct-message target required")
	}
	if err := ValidateDirectMessageText(text); err != nil {
		return DirectMessageProposal{}, err
	}
	sender, ok := s.Players[senderID]
	if !ok || !sender.Online || sender.Body.Type != 0 || !validDirectMessageName(sender.Body.Name) {
		return DirectMessageProposal{}, fmt.Errorf("online direct-message sender absent")
	}
	targetID, err := s.selectDirectMessageTarget(senderID, selector)
	if err != nil {
		return DirectMessageProposal{}, err
	}
	target := s.Players[targetID]
	if !directMessageTargetVisible(sender, target) {
		return DirectMessageProposal{}, fmt.Errorf("direct-message target unavailable")
	}
	proposal := DirectMessageProposal{
		SenderID:   senderID,
		Selector:   selector,
		TargetID:   targetID,
		SenderName: sender.Body.Name,
		TargetName: target.Body.Name,
		Text:       strings.TrimSpace(text),
	}
	if sender.Body.Class < playerDMClass && flag(target.Body.Flags[:], playerIgnoreAllSendFlag) {
		proposal.Response = fmt.Sprintf("%s님은 이야기 듣기 거부 상태입니다.\r\n", target.Body.Name)
		return proposal, nil
	}
	if proposal.Text == "" {
		proposal.Response = "무슨 말을 전하시려구요?\r\n"
		return proposal, nil
	}
	if flag(sender.Body.Flags[:], playerSilentStateFlag) {
		proposal.Response = "당신은 말을 할 수 없습니다.\r\n"
		return proposal, nil
	}
	proposal.Response = directMessageResponse(sender, target, proposal.Text)
	proposal.Delivered = true
	return proposal, nil
}

func directMessageEventFromProposal(s State, proposal DirectMessageProposal) (DirectMessageEvent, error) {
	sender, senderOK := s.Players[proposal.SenderID]
	target, targetOK := s.Players[proposal.TargetID]
	if !senderOK || !targetOK || !sender.Online || !target.Online || sender.Body.Name != proposal.SenderName || target.Body.Name != proposal.TargetName {
		return DirectMessageEvent{}, fmt.Errorf("stale direct-message event target")
	}
	return DirectMessageEvent{
		SenderID:   proposal.SenderID,
		SenderName: sender.Body.Name,
		TargetID:   proposal.TargetID,
		TargetName: target.Body.Name,
		Text:       fmt.Sprintf("\n%s님이 당신에게 \"%s\"라고 이야기합니다.\r\n", sender.Body.Name, proposal.Text),
	}, nil
}

// DirectMessageEvent derives the recipient projection from a committed
// snapshot. It re-plans the proposal to reject stale visibility/listen state;
// replay callers should use the event already carried by DirectMessageResult
// rather than emitting it a second time.
func (s State) DirectMessageEvent(proposal DirectMessageProposal) (DirectMessageEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return DirectMessageEvent{}, false, err
	}
	fresh, err := s.PlanDirectMessage(proposal.SenderID, proposal.Selector, proposal.Text)
	if err != nil {
		return DirectMessageEvent{}, false, err
	}
	if fresh != proposal {
		return DirectMessageEvent{}, false, fmt.Errorf("stale direct-message proposal")
	}
	if !proposal.Delivered {
		return DirectMessageEvent{}, false, nil
	}
	event, err := directMessageEventFromProposal(s, proposal)
	if err != nil {
		return DirectMessageEvent{}, false, err
	}
	return event, true, nil
}

// ApplyDirectMessage rechecks the pure proposal atomically. Since the current
// model has no durable last-message field, the returned State is unchanged;
// only the command receipt and its deterministic event projection are saved by
// the caller.
func (s State) ApplyDirectMessage(proposal DirectMessageProposal) (State, DirectMessageResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, DirectMessageResult{}, err
	}
	fresh, err := s.PlanDirectMessage(proposal.SenderID, proposal.Selector, proposal.Text)
	if err != nil {
		return State{}, DirectMessageResult{}, err
	}
	if fresh != proposal {
		return State{}, DirectMessageResult{}, fmt.Errorf("stale direct-message proposal")
	}
	result := DirectMessageResult{Response: proposal.Response, Delivered: proposal.Delivered}
	if proposal.Delivered {
		event, err := directMessageEventFromProposal(s, proposal)
		if err != nil {
			return State{}, DirectMessageResult{}, err
		}
		result.Event = &event
	}
	return s, result, nil
}
