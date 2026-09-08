package world

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	trackTimerIndex      = 4 // LT_TRACK
	trackRangerClass     = 7 // RANGER
	trackAuthorizedClass = 9 // INVINCIBLE and above
)

// TrackProposal is the deterministic candidate for command4.c:track. The
// room trace is read from the authoritative room snapshot; no client-provided
// direction is accepted.
type TrackProposal struct {
	ActorID     string
	RoomID      int16
	Now         int32
	Chance      int
	Interval    int32
	WaitSeconds int32
	Cooldown    bool
	NoOp        bool
	ClearHidden bool
	Broadcast   bool
	Response    string
}

// TrackResult is the durable actor response. RoomTrackEvent derives the
// asynchronous room projection only after this result commits.
type TrackResult struct {
	Response  string
	Broadcast bool
}

type TrackEvent struct {
	RoomID         int16
	ExcludeActorID string
	Text           string
}

func trackBonus(body LegacyMonster) (int, error) {
	if body.Stats[1] > 63 {
		return 0, fmt.Errorf("track dexterity outside legacy bonus table")
	}
	return legacyStatBonus[body.Stats[1]], nil
}

func validTrackText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == '\r' || r == '\n' {
			return false
		}
	}
	return strings.TrimSpace(text) == text
}

func trackWaitResponse(seconds int32) string {
	if seconds == 1 {
		return "1초만 기다리세요.\r\n"
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds)
}

// PlanTrack ports the player/NPC-independent part of command4.c:track. The
// original command clears PHIDDN before checking cooldown and blindness, then
// writes LT_TRACK before the blind/chance branches. That order is represented
// by ClearHidden and the timer fields in the proposal. Object/exit tracking is
// not inferred here; an invalid resource or random source fails closed.
func (s State) PlanTrack(actorID string, now int32, roll func(int, int) int) (TrackProposal, error) {
	if err := s.Validate(); err != nil {
		return TrackProposal{}, err
	}
	if actorID == "" || now < 0 {
		return TrackProposal{}, fmt.Errorf("invalid track actor or clock")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return TrackProposal{}, fmt.Errorf("online tracker absent")
	}
	if actor.Body.Class != trackRangerClass && actor.Body.Class < trackAuthorizedClass {
		return TrackProposal{ActorID: actorID, RoomID: actor.Body.RoomID, Now: now, NoOp: true, Response: "포졸만 쓸수 있는 명령입니다.\r\n"}, nil
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !validTrackText(room.Resource.Track) {
		return TrackProposal{}, fmt.Errorf("track room or trace invalid")
	}
	dexBonus, err := trackBonus(actor.Body)
	if err != nil {
		return TrackProposal{}, err
	}
	interval := int32(5 - dexBonus)
	timer := actor.Body.Timers[trackTimerIndex]
	if timer.LastTime < 0 {
		return TrackProposal{}, fmt.Errorf("track timer outside legacy range")
	}
	proposal := TrackProposal{
		ActorID:     actorID,
		RoomID:      actor.Body.RoomID,
		Now:         now,
		Interval:    interval,
		ClearHidden: true,
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(deadline - int64(now))
		if proposal.WaitSeconds < 1 {
			return TrackProposal{}, fmt.Errorf("track cooldown overflow")
		}
		proposal.Response = trackWaitResponse(proposal.WaitSeconds)
		return proposal, nil
	}
	if flag(actor.Body.Flags[:], playerBlindFlag) {
		proposal.Response = "당신은 눈이 멀어 있습니다. 도저히 추적을 할 수 없습니다.\r\n"
		return proposal, nil
	}
	chance := 25 + (dexBonus+((int(actor.Body.Level)+3)/4))*5
	proposal.Chance = chance
	if roll == nil {
		return TrackProposal{}, fmt.Errorf("missing track random source")
	}
	value := roll(1, 100)
	if value < 1 || value > 100 {
		return TrackProposal{}, fmt.Errorf("track random value outside 1..100")
	}
	if value > chance {
		proposal.Response = "추적 실패!\r\n"
		return proposal, nil
	}
	if room.Resource.Track == "" {
		proposal.Response = "아무런 흔적이 남아있지 않습니다.\r\n"
		return proposal, nil
	}
	proposal.Broadcast = true
	proposal.Response = fmt.Sprintf("%s쪽으로 흔적이 나 있습니다.\r\n", room.Resource.Track)
	return proposal, nil
}

// ApplyTrack applies the proposal atomically and rejects a stale room/player
// snapshot instead of mutating only the hidden flag or timer.
func (s State) ApplyTrack(proposal TrackProposal) (State, TrackResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, TrackResult{}, err
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.RoomID != proposal.RoomID {
		return State{}, TrackResult{}, fmt.Errorf("track actor changed")
	}
	if proposal.Now < 0 || proposal.Response == "" {
		return State{}, TrackResult{}, fmt.Errorf("invalid track proposal")
	}
	if proposal.NoOp {
		if actor.Body.Class == trackRangerClass || actor.Body.Class >= trackAuthorizedClass || proposal.ClearHidden || proposal.Cooldown || proposal.Broadcast {
			return State{}, TrackResult{}, fmt.Errorf("stale track authorization proposal")
		}
		return s, TrackResult{Response: proposal.Response}, nil
	}
	if actor.Body.Class != trackRangerClass && actor.Body.Class < trackAuthorizedClass {
		return State{}, TrackResult{}, fmt.Errorf("track authorization changed")
	}
	room, ok := s.Rooms[proposal.RoomID]
	if !ok || !validTrackText(room.Resource.Track) {
		return State{}, TrackResult{}, fmt.Errorf("track room changed")
	}
	dexBonus, err := trackBonus(actor.Body)
	if err != nil {
		return State{}, TrackResult{}, err
	}
	interval := int32(5 - dexBonus)
	if proposal.Interval != interval || !proposal.ClearHidden || proposal.NoOp {
		return State{}, TrackResult{}, fmt.Errorf("stale track interval proposal")
	}
	timer := actor.Body.Timers[trackTimerIndex]
	if timer.LastTime < 0 {
		return State{}, TrackResult{}, fmt.Errorf("track timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	next := s.clone()
	actor = next.Players[proposal.ActorID]
	actor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	if proposal.Cooldown {
		wait := deadline - int64(proposal.Now)
		if wait < 1 || proposal.WaitSeconds != int32(wait) || proposal.Broadcast || proposal.Chance != 0 {
			return State{}, TrackResult{}, fmt.Errorf("stale track cooldown proposal")
		}
		if proposal.Response != trackWaitResponse(proposal.WaitSeconds) {
			return State{}, TrackResult{}, fmt.Errorf("track cooldown response mismatch")
		}
	} else {
		if int64(proposal.Now) < deadline || proposal.WaitSeconds != 0 {
			return State{}, TrackResult{}, fmt.Errorf("track cooldown changed")
		}
		blind := flag(actor.Body.Flags[:], playerBlindFlag)
		if blind {
			if proposal.Chance != 0 || proposal.Broadcast || proposal.Response != "당신은 눈이 멀어 있습니다. 도저히 추적을 할 수 없습니다.\r\n" {
				return State{}, TrackResult{}, fmt.Errorf("track blind response mismatch")
			}
		} else {
			chance := 25 + (dexBonus+((int(actor.Body.Level)+3)/4))*5
			if proposal.Chance != chance {
				return State{}, TrackResult{}, fmt.Errorf("stale track chance proposal")
			}
			switch {
			case proposal.Response == "추적 실패!\r\n":
				if proposal.Broadcast {
					return State{}, TrackResult{}, fmt.Errorf("track failure broadcast mismatch")
				}
			case room.Resource.Track == "":
				if proposal.Broadcast || proposal.Response != "아무런 흔적이 남아있지 않습니다.\r\n" {
					return State{}, TrackResult{}, fmt.Errorf("track empty-trace response mismatch")
				}
			default:
				if !proposal.Broadcast || proposal.Response != fmt.Sprintf("%s쪽으로 흔적이 나 있습니다.\r\n", room.Resource.Track) {
					return State{}, TrackResult{}, fmt.Errorf("track success response mismatch")
				}
			}
		}
		actor.Body.Timers[trackTimerIndex].LastTime = proposal.Now
		actor.Body.Timers[trackTimerIndex].Interval = interval
	}
	next.Players[proposal.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, TrackResult{}, err
	}
	return next, TrackResult{Response: proposal.Response, Broadcast: proposal.Broadcast}, nil
}

// RoomTrackEvent is derived from the committed snapshot and is never called
// for a replayed receipt. The actor receives the durable response itself.
func (s State) RoomTrackEvent(actorID string) (TrackEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return TrackEvent{}, false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return TrackEvent{}, false, fmt.Errorf("online tracker absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !validTrackText(room.Resource.Track) || room.Resource.Track == "" {
		return TrackEvent{}, false, nil
	}
	return TrackEvent{
		RoomID:         actor.Body.RoomID,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n%s님이 적이 지나간 흔적을 찾았습니다.\r\n", actor.Body.Name),
	}, true, nil
}
