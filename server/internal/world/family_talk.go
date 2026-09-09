package world

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// These indices are the legacy PFAMIL/PSILNC bits from mtype.h.  Family
	// talk is a read-only projection: membership and money mutations belong to
	// the separate family mutation reducer.
	familyTalkMemberFlag = FamilyMemberFlag
	familyTalkSilentFlag = 44 // PSILNC
)

var (
	ErrFamilyTalkActorAbsent    = errors.New("online family-talk actor absent")
	ErrFamilyTalkNotMember      = errors.New("family-talk actor is not a member")
	ErrFamilyTalkSilent         = errors.New("family-talk actor is silent")
	ErrFamilyTalkMessageEmpty   = errors.New("family-talk message is empty")
	ErrFamilyTalkMessageInvalid = errors.New("invalid family-talk message")
)

// FamilyTalkEvent is one committed recipient projection.  Recipient order is
// room membership order followed by sorted residual online identities because
// State does not retain C's descriptor table order.  The order is persisted in
// the receipt so a replay never re-enumerates current connections.
type FamilyTalkEvent struct {
	RecipientID   string `json:"recipient_id"`
	RecipientName string `json:"recipient_name"`
	ActorID       string `json:"actor_id"`
	ActorName     string `json:"actor_name"`
	FamilyID      int16  `json:"family_id"`
	Message       string `json:"message"`
	Text          string `json:"text"`
}

// FamilyTalkResult is a no-state-change receipt for command11.c:family_talk.
// The actor receives Response and the transport fans out Events once after a
// first commit.  An empty/silent/no-members result is still persisted so the
// command ID remains idempotent.
type FamilyTalkResult struct {
	Action     string            `json:"action"`
	ActorID    string            `json:"actor_id"`
	FamilyID   int16             `json:"family_id,omitempty"`
	FamilyName string            `json:"family_name,omitempty"`
	Message    string            `json:"message,omitempty"`
	Response   string            `json:"response"`
	Events     []FamilyTalkEvent `json:"events,omitempty"`
	Broadcast  bool              `json:"broadcast"`
}

// PlanFamilyTalk validates and projects a family message without mutating the
// canonical snapshot.  It follows C's PFAMIL membership and PSILNC gate and
// does not apply PDMINV filtering: family_talk iterates family members
// directly.  A server-owned catalog is required before a numeric family ID is
// rendered, preventing a guessed family name from reaching clients.
func (s State) PlanFamilyTalk(actorID, message string, catalog FamilyCatalog) (FamilyTalkResult, error) {
	if err := s.Validate(); err != nil {
		return FamilyTalkResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validFamilyText(actor.Body.Name) {
		return FamilyTalkResult{}, ErrFamilyTalkActorAbsent
	}
	result := FamilyTalkResult{Action: "family-talk", ActorID: actorID}
	if !familyActive(actor.Body) {
		result.Response = "당신은 패거리에 속해있지 않습니다.\r\n"
		return result, nil
	}
	familyID := familyIDOf(actor.Body)
	family, err := catalog.lookup(familyID)
	if err != nil {
		return FamilyTalkResult{}, err
	}
	result.FamilyID, result.FamilyName = familyID, family.Name
	if flag(actor.Body.Flags[:], familyTalkSilentFlag) {
		result.Response = "입이 막혀 말이 나오질 않습니다.\r\n"
		return result, nil
	}
	message = strings.TrimSpace(message)
	if message == "" {
		result.Response = "패거리원들에게 무슨 말을 하시려고요?\r\n"
		return result, nil
	}
	if err := validateFamilyTalkMessage(message); err != nil {
		return FamilyTalkResult{}, err
	}
	result.Message = message
	for _, id := range familyTalkOnlineOrder(s, actor.Body.RoomID) {
		player, ok := s.Players[id]
		if !ok || !player.Online || !familyActive(player.Body) || familyIDOf(player.Body) != familyID {
			continue
		}
		result.Events = append(result.Events, FamilyTalkEvent{
			RecipientID: id, RecipientName: player.Body.Name, ActorID: actorID,
			ActorName: actor.Body.Name, FamilyID: familyID, Message: message,
			Text: fmt.Sprintf("\n%s>>> %s\r\n", actor.Body.Name, message),
		})
	}
	if len(result.Events) == 0 {
		// A stale PFAMIL bit with no online canonical member is not converted
		// into a fabricated recipient.  Keep the source's ordinary no-family
		// response while retaining the valid catalog projection.
		result.Response = "당신은 패거리에 속해있지 않습니다.\r\n"
		return result, nil
	}
	result.Response = "예. 좋습니다.\r\n"
	result.Broadcast = true
	return result, nil
}

// FamilyTalk is the concise compatibility spelling used by callers that do
// not need to distinguish planning from projection.
func (s State) FamilyTalk(actorID, message string, catalog FamilyCatalog) (FamilyTalkResult, error) {
	return s.PlanFamilyTalk(actorID, message, catalog)
}

func familyTalkOnlineOrder(s State, roomID int16) []string {
	seen := make(map[string]struct{}, len(s.Players))
	ordered := make([]string, 0, len(s.Players))
	if room, ok := s.Rooms[roomID]; ok {
		for _, id := range room.PlayerIDs {
			if _, exists := seen[id]; exists {
				continue
			}
			if player, exists := s.Players[id]; exists && player.Online {
				seen[id] = struct{}{}
				ordered = append(ordered, id)
			}
		}
	}
	residual := make([]string, 0, len(s.Players))
	for id, player := range s.Players {
		if !player.Online {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		residual = append(residual, id)
	}
	sort.Strings(residual)
	return append(ordered, residual...)
}

func validateFamilyTalkMessage(message string) error {
	if !utf8.ValidString(message) || len(message) > MaxBroadcastTextBytes {
		return ErrFamilyTalkMessageInvalid
	}
	for _, r := range message {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return ErrFamilyTalkMessageInvalid
		}
	}
	return nil
}
