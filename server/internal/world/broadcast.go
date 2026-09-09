package world

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// BroadcastKind is the two public C broadcast formats. The receiver flag is
// part of the committed event so transport cannot accidentally deliver a
// cheer to players who disabled ordinary cheer output (or vice versa).
type BroadcastKind string

const (
	BroadcastChat  BroadcastKind = "chat"
	BroadcastCheer BroadcastKind = "cheer"
)

const (
	broadcastDailyIndex  = 0 // DL_BROAD
	broadcastNoChatFlag  = 3 // PNOBRD
	broadcastSilentFlag  = 44
	broadcastLimitFlag   = 49 // PBRSND
	broadcastNoCheerFlag = 50 // PNOBR2
	broadcastCaretaker   = 10 // CARETAKER
	broadcastSubDM       = 11 // SUB_DM

	// command4.c uses a 256-byte fullstr buffer and reserves its final byte
	// for NUL. The Go terminal is UTF-8, so the limit is still measured in
	// bytes and validation rejects an invalid sequence instead of replacing it.
	MaxBroadcastTextBytes = 255
)

var broadcastDiscountTable = [...]int16{
	60, 57, 54, 51, 48, 45, 42, 40, 38, 36,
	34, 32, 30, 28, 26, 24, 22, 20, 18, 16,
	14, 12, 10, 8, 7, 6, 5, 4, 3, 2, 2,
}

// BroadcastOptions contains descriptor/process-local timing. It is included
// in the command request and receipt computation, but is not added to the
// player snapshot: C's broad_time/all_broad_time are runtime fields too.
type BroadcastOptions struct {
	Now      int32
	LastAt   int32
	GlobalAt int32
	Local    *time.Location
}

type BroadcastEvent struct {
	Kind         BroadcastKind `json:"kind"`
	ActorID      string        `json:"actor_id"`
	ActorName    string        `json:"actor_name"`
	ReceiverFlag uint          `json:"receiver_flag"`
	Text         string        `json:"text"`
}

type BroadcastResult struct {
	Response  string          `json:"response"`
	Broadcast bool            `json:"broadcast"`
	Kind      BroadcastKind   `json:"kind"`
	Text      string          `json:"text,omitempty"`
	Discount  int16           `json:"discount,omitempty"`
	DailyUsed bool            `json:"daily_used,omitempty"`
	Event     *BroadcastEvent `json:"event,omitempty"`
}

func (k BroadcastKind) receiverFlag() (uint, bool) {
	switch k {
	case BroadcastChat:
		return broadcastNoChatFlag, true
	case BroadcastCheer:
		return broadcastNoCheerFlag, true
	default:
		return 0, false
	}
}

// PlayerFlagSet exposes the bounded legacy bit lookup needed by transport
// projections. Keeping the bounds check here prevents a malformed migrated
// flag number from turning a notification into an out-of-range panic.
func PlayerFlagSet(body LegacyMonster, bit uint) bool {
	byteIndex := bit / 8
	if byteIndex >= uint(len(body.Flags)) {
		return false
	}
	return body.Flags[byteIndex]&(1<<(bit%8)) != 0
}

func ValidateBroadcastText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("invalid broadcast text")
	}
	if len(text) > MaxBroadcastTextBytes {
		return fmt.Errorf("broadcast text exceeds byte limit")
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("invalid broadcast text")
		}
	}
	return nil
}

func broadcastResponse(kind BroadcastKind, actorName, text string) string {
	if kind == BroadcastCheer {
		return fmt.Sprintf("\n%s님이 \"%s\"라고 환호를 합니다.\r\n", actorName, text)
	}
	return fmt.Sprintf("\n%s> %s\r\n", actorName, text)
}

func broadcastDiscount(now, lastAt int32) int16 {
	delta := int64(now) - int64(lastAt)
	if delta < 0 || delta > int64(len(broadcastDiscountTable)-1) {
		return 2
	}
	return broadcastDiscountTable[delta]
}

func dailyBroadcastUse(before LegacyDaily, now int32, location *time.Location) (LegacyDaily, bool) {
	if location == nil {
		location = time.Local
	}
	out := before
	current := time.Unix(int64(now), 0).In(location)
	last := time.Unix(int64(before.LastTime), 0).In(location)
	if current.YearDay() != last.YearDay() {
		out.Current = out.Max
		out.LastTime = now
	}
	if out.Current == 0 {
		return out, false
	}
	out.Current--
	return out, true
}

// PlanBroadcast ports command4.c:broadsend/broadsend2. Runtime timing remains
// descriptor-local, while HP and daily usage are committed with the command
// so a retry cannot charge them twice. C checks the daily quota before its
// later silence/level/HP gates; returning the cloned candidate on those
// failures preserves that observable mutation.
func (s State) PlanBroadcast(actorID string, kind BroadcastKind, text string, options BroadcastOptions) (State, BroadcastResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, BroadcastResult{}, err
	}
	receiverFlag, ok := kind.receiverFlag()
	if !ok {
		return State{}, BroadcastResult{}, fmt.Errorf("unknown broadcast kind %q", kind)
	}
	if err := ValidateBroadcastText(text); err != nil {
		return State{}, BroadcastResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || !validDirectMessageName(actor.Body.Name) {
		return State{}, BroadcastResult{}, fmt.Errorf("online broadcaster absent")
	}
	text = strings.TrimSpace(text)
	result := BroadcastResult{Kind: kind, Text: text}
	if text == "" {
		result.Response = "무슨 말을 하시려구요?\r\n"
		return s, result, nil
	}

	next := s
	cloned := false
	dailyUsed := false
	if actor.Body.Class < broadcastSubDM && int64(options.Now)-int64(options.GlobalAt) > 10 && flag(actor.Body.Flags[:], broadcastLimitFlag) {
		daily := actor.Body.Daily[broadcastDailyIndex]
		updated, allowed := dailyBroadcastUse(daily, options.Now, options.Local)
		next = s.clone()
		cloned = true
		actor = next.Players[actorID]
		actor.Body.Daily[broadcastDailyIndex] = updated
		next.Players[actorID] = actor
		if !allowed {
			result.Response = "당신은 오늘 잡담의 한계를 넘겼습니다.\r\n"
			return next, result, nil
		}
		dailyUsed = true
	}

	if actor.Body.Class < broadcastSubDM && flag(actor.Body.Flags[:], broadcastSilentFlag) {
		if kind == BroadcastChat {
			result.Response = "당신의 목소리가 너무 작아 잡담을 할 수 없습니다.\r\n"
		} else {
			result.Response = "당신의 목소리가 너무 작아 환호를 할 수 없습니다.\r\n"
		}
		result.DailyUsed = dailyUsed
		return next, result, nil
	}
	if actor.Body.Class < broadcastCaretaker && actor.Body.Level < 20 {
		if kind == BroadcastChat {
			result.Response = "당신의 레벨로는 잡담을 할 수 없습니다.\r\n"
		} else {
			result.Response = "당신의 레벨로는 환호를 할 수 없습니다.\r\n"
		}
		result.DailyUsed = dailyUsed
		return next, result, nil
	}

	discount := broadcastDiscount(options.Now, options.LastAt)
	if actor.Body.Class >= 9 && actor.Body.Class < broadcastSubDM {
		extra := int(discount) * int(actor.Body.Level) / 60
		discount = int16(int(discount) + extra)
	}
	result.Discount = discount
	if actor.Body.Class < broadcastSubDM && actor.Body.HPCurrent <= discount {
		if kind == BroadcastChat {
			result.Response = "당신의 목숨이 위태로워 잡담을 할 수 없습니다.\r\n"
		} else {
			result.Response = "당신의 목숨이 위태로워 환호를 할 수 없습니다.\r\n"
		}
		result.DailyUsed = dailyUsed
		return next, result, nil
	}

	if !cloned {
		next = s.clone()
	}
	actor = next.Players[actorID]
	if actor.Body.Class < broadcastSubDM {
		actor.Body.HPCurrent -= discount
	}
	next.Players[actorID] = actor
	eventText := broadcastResponse(kind, actor.Body.Name, text)
	event := &BroadcastEvent{Kind: kind, ActorID: actorID, ActorName: actor.Body.Name, ReceiverFlag: receiverFlag, Text: eventText}
	result.Broadcast = true
	result.DailyUsed = dailyUsed
	result.Event = event
	if !flag(actor.Body.Flags[:], receiverFlag) {
		result.Response = eventText
	}
	return next, result, nil
}
