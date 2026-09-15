package world

import (
	"fmt"
	"strings"
)

const (
	playerHiddenStateFlag = 1
	playerSilentStateFlag = 44
	playerLocalEchoFlag   = 46
)

type SayResult struct {
	Response  string
	Broadcast bool
}

type SayEvent struct {
	RoomID    int16
	ActorID   string
	ActorName string
	Text      string
}

// PlanSay ports the durable state boundary of command2.c:say. A successful
// utterance reveals a hidden player before the receipt is committed; silence
// suppresses room fan-out but remains a normal, replayable command response.
func (s State) PlanSay(actorID, text string) (State, SayResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, SayResult{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return State{}, SayResult{}, fmt.Errorf("online speaker absent")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return s, SayResult{Response: "뭘 말하고 싶으세요?\r\n"}, nil
	}
	if flag(p.Body.Flags[:], playerSilentStateFlag) {
		return s, SayResult{Response: "말을 해 보았지만 이 방 밖의 사람들은 들리지 않는듯 하군요.\r\n"}, nil
	}
	next := s.clone()
	p = next.Players[actorID]
	p.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[actorID] = p
	response := "예. 좋습니다.\r\n"
	if flag(p.Body.Flags[:], playerLocalEchoFlag) {
		response = fmt.Sprintf("당신은 \"%s\"라고 말합니다.\r\n", text)
	}
	return next, SayResult{Response: response, Broadcast: true}, nil
}

// RoomSayEvent derives the committed room event after a successful receipt.
// It rechecks the persisted silence bit so a replay or a concurrent state
// transition cannot fan out a message that the authoritative snapshot muted.
func (s State) RoomSayEvent(actorID, text string) (SayEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return SayEvent{}, false, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return SayEvent{}, false, fmt.Errorf("online speaker absent")
	}
	text = strings.TrimSpace(text)
	if text == "" || flag(p.Body.Flags[:], playerSilentStateFlag) {
		return SayEvent{}, false, nil
	}
	return SayEvent{RoomID: p.Body.RoomID, ActorID: actorID, ActorName: p.Body.Name, Text: text}, true, nil
}
