package world

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	groupTalkDMInvisibleFlag = 10 // PDMINV
	groupTalkIgnoreFlag      = 35 // PIGNOR
	groupTalkSilentFlag      = 44 // PSILNC
	groupTalkCaretakerClass  = 10 // CARETAKER
)

// GroupTalkEvent is one committed recipient projection. Recipient IDs come
// from the authoritative mixed first_fol order; no display-name lookup or
// eavesdrop recipient is invented here.
type GroupTalkEvent struct {
	RecipientKind string `json:"recipient_kind"`
	RecipientID   string `json:"recipient_id"`
	RecipientName string `json:"recipient_name"`
	ActorID       string `json:"actor_id"`
	ActorName     string `json:"actor_name"`
	Message       string `json:"message"`
	Text          string `json:"text"`
}

// GroupTalkResult is the deterministic, read-only result for command10.c's
// gtalk. Notices are sender-local warnings for ignored recipients. Events are
// ordered follower events followed by the leader event, matching C's loop.
// The current Go model has no durable eavesdrop/ignore lists, so this result
// uses only canonical creature flags and does not add guessed persistence.
type GroupTalkResult struct {
	Action    string           `json:"action"`
	ActorID   string           `json:"actor_id"`
	LeaderID  string           `json:"leader_id"`
	Message   string           `json:"message"`
	Response  string           `json:"response"`
	Notices   []string         `json:"notices,omitempty"`
	Events    []GroupTalkEvent `json:"events,omitempty"`
	Broadcast bool             `json:"broadcast"`
}

// PlanGroupTalk validates and projects a group message without changing the
// world snapshot. It follows C's leader selection: a follower addresses its
// leader's mixed first_fol list, while a leader addresses its own list. The
// canonical FollowerRefs slice is authoritative whenever present; if an old
// player-only snapshot has no mixed slice, FollowerIDs remain deterministic,
// but unresolved player/NPC interleaving fails closed.
func (s State) PlanGroupTalk(actorID, message string) (GroupTalkResult, error) {
	if err := s.Validate(); err != nil {
		return GroupTalkResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return GroupTalkResult{}, fmt.Errorf("online group-talk actor absent")
	}
	leaderID := actorID
	if actor.FollowingID != "" {
		leaderID = actor.FollowingID
	}
	leader, ok := s.Players[leaderID]
	if !ok || !leader.Online {
		return GroupTalkResult{}, fmt.Errorf("group leader absent")
	}
	refs, err := groupTalkFollowerRefs(leader)
	if err != nil {
		return GroupTalkResult{}, err
	}
	if len(refs) == 0 {
		return GroupTalkResult{
			Action: "group-talk", ActorID: actorID, LeaderID: leaderID,
			Response: "당신은 그룹에 속해있지 않습니다.\r\n",
		}, nil
	}

	message = strings.TrimSpace(message)
	if message == "" {
		return GroupTalkResult{
			Action: "group-talk", ActorID: actorID, LeaderID: leaderID,
			Response: "그룹원들에게 무슨말을 하시려구요?\r\n",
		}, nil
	}
	if err := validGroupTalkMessage(message); err != nil {
		return GroupTalkResult{}, err
	}
	if flag(actor.Body.Flags[:], groupTalkSilentFlag) {
		return GroupTalkResult{
			Action: "group-talk", ActorID: actorID, LeaderID: leaderID, Message: message,
			Response: "입이 막혀 말이 나오질 않습니다.\r\n",
		}, nil
	}

	result := GroupTalkResult{
		Action: "group-talk", ActorID: actorID, LeaderID: leaderID, Message: message,
		Response: "예. 좋습니다.\r\n",
	}
	// C's found bit records any non-PDMINV follower even when that follower
	// rejects the sender. Keep that distinction from actual delivered events.
	found := false
	for _, ref := range refs {
		name, flags, err := s.groupTalkTarget(ref)
		if err != nil {
			return GroupTalkResult{}, err
		}
		if flag(flags[:], groupTalkDMInvisibleFlag) {
			continue
		}
		found = true
		if flag(flags[:], groupTalkIgnoreFlag) && actor.Body.Class < groupTalkCaretakerClass {
			result.Notices = append(result.Notices, fmt.Sprintf("%s는 이야기 듣기 거부 상태입니다.\r\n", name))
			continue
		}
		result.Events = append(result.Events, groupTalkEvent(ref, name, actorID, actor, message))
	}
	if !found {
		result.Response = "당신은 그룹에 속해있지 않습니다.\r\n"
		return result, nil
	}

	// The source sends the leader after the follower loop. PDMINV still makes
	// that leader invisible; unlike a follower it does not affect found, which
	// has already been established above.
	leaderRef := EntityRef{Kind: "player", ID: leaderID}
	if !flag(leader.Body.Flags[:], groupTalkDMInvisibleFlag) {
		if flag(leader.Body.Flags[:], groupTalkIgnoreFlag) && actor.Body.Class < groupTalkCaretakerClass {
			result.Notices = append(result.Notices, fmt.Sprintf("%s님은 이야기 듣기 거부 상태입니다.\r\n", leader.Body.Name))
		} else {
			result.Events = append(result.Events, groupTalkEvent(leaderRef, leader.Body.Name, actorID, actor, message))
		}
	}
	result.Broadcast = len(result.Events) != 0
	return result, nil
}

// GroupTalk is a concise alias for callers that do not need the Plan prefix.
func (s State) GroupTalk(actorID, message string) (GroupTalkResult, error) {
	return s.PlanGroupTalk(actorID, message)
}

// GroupTalkEvents returns only ordered recipient events. It is useful to
// transports that keep the durable actor response separate from fan-out.
func (s State) GroupTalkEvents(actorID, message string) ([]GroupTalkEvent, error) {
	result, err := s.PlanGroupTalk(actorID, message)
	if err != nil {
		return nil, err
	}
	return append([]GroupTalkEvent(nil), result.Events...), nil
}

func groupTalkFollowerRefs(leader PlayerState) ([]EntityRef, error) {
	if leader.FollowerRefs != nil {
		return append([]EntityRef(nil), leader.FollowerRefs...), nil
	}
	if len(leader.NPCFollowerIDs) != 0 {
		return nil, fmt.Errorf("mixed group follower order unresolved")
	}
	refs := make([]EntityRef, 0, len(leader.FollowerIDs))
	for _, id := range leader.FollowerIDs {
		if id == "" {
			return nil, fmt.Errorf("empty group follower identity")
		}
		refs = append(refs, EntityRef{Kind: "player", ID: id})
	}
	return refs, nil
}

func (s State) groupTalkTarget(ref EntityRef) (string, [8]byte, error) {
	switch ref.Kind {
	case "player":
		player, ok := s.Players[ref.ID]
		if !ok || !player.Online || player.Body.Name == "" {
			return "", [8]byte{}, fmt.Errorf("group player follower absent")
		}
		return player.Body.Name, player.Body.Flags, nil
	case "npc":
		npc, ok := s.NPCs[ref.ID]
		if !ok || npc.Body.Name == "" {
			return "", [8]byte{}, fmt.Errorf("group NPC follower absent")
		}
		return npc.Body.Name, npc.Body.Flags, nil
	default:
		return "", [8]byte{}, fmt.Errorf("unknown group follower kind")
	}
}

func groupTalkEvent(ref EntityRef, recipientName, actorID string, actor PlayerState, message string) GroupTalkEvent {
	return GroupTalkEvent{
		RecipientKind: ref.Kind,
		RecipientID:   ref.ID,
		RecipientName: recipientName,
		ActorID:       actorID,
		ActorName:     actor.Body.Name,
		Message:       message,
		Text:          fmt.Sprintf("%s이 그룹원들에게 \"%s\"라고 말합니다.\r\n", actor.Body.Name, message),
	}
}

func validGroupTalkMessage(message string) error {
	if !utf8.ValidString(message) || len(message) > 255 {
		return fmt.Errorf("invalid group-talk message")
	}
	for _, r := range message {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return fmt.Errorf("invalid group-talk message")
		}
	}
	return nil
}
