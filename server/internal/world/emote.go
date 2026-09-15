package world

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// EmoteResult is the actor-facing part of an action.c receipt.  Most legacy
// action branches only print and broadcast text; the one durable state change
// in this slice is clearing PHIDDN (the hidden flag) before the silence check.
// Event is a committed-state projection and is intentionally not sent to the
// network from this package.
type EmoteResult struct {
	Response  string
	Broadcast bool
	TargetID  string
	Event     *EmoteEvent
}

// EmoteEvent contains the two recipient-specific projections of OUT2/OUTj2
// and the room projection.  TargetText is delivered only to TargetID;
// Text is delivered to room occupants other than ActorID and TargetID.
// For a targetless action TargetID and TargetText are empty and Text excludes
// only ActorID.
type EmoteEvent struct {
	RoomID          int16
	ActorID         string
	ActorName       string
	TargetID        string
	TargetName      string
	ExcludeActorID  string
	ExcludeTargetID string
	Text            string
	TargetText      string
}

type emoteParticle uint8

const (
	emoteNoParticle emoteParticle = iota
	emoteObjectParticle
	emoteWithParticle
)

// emoteSpec is a deliberately closed table.  The strings below are the
// source-backed action.c templates, with the C formatter's %M/%j values
// materialized by renderEmoteEvent.  An empty targetActor means this action
// is not admitted with an explicit player target in this bounded slice.
type emoteSpec struct {
	targetlessOnly bool
	particle       emoteParticle
	soloActor      string
	soloRoom       string
	targetActor    string
	targetTarget   string
	targetRoom     string
}

// These aliases are the action() entries in src/global.c (action command 100)
// except for "보아", which the requested general-player slice deliberately
// leaves for the later target-inspection boundary.  Alias groups retain the
// exact action.c branch shared by their aliases.
var emoteSpecs = func() map[string]emoteSpec {
	specs := map[string]emoteSpec{
		"감정표현": {
			targetlessOnly: true,
			soloActor:      "당신은 '감정표현 도움'이라 치는게 좋을겁니다.",
			soloRoom:       "%s이 감정표현을 연구합니다.",
		},
		"노려봐": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 허공을 뚫어져라 노려 봅니다.",
			soloRoom:     "%s이 허공을 뚫어져라 노려 봅니다.",
			targetActor:  "당신은 %s 뱁새눈을 하고 노려 봅니다.",
			targetTarget: "%s이 당신을 뱁새눈을 하고 노려 봅니다.",
			targetRoom:   "%s이 %s 뱁새눈을 하고 노려 봅니다.",
		},
		"끄덕": {
			soloActor:    "당신은 고개를 끄덕거립니다.",
			soloRoom:     "%s이 고개를 끄덕거립니다.",
			targetActor:  "당신은 %s에게 고개를 끄덕거립니다.",
			targetTarget: "%s이 당신에게 고개를 끄덕거립니다.",
			targetRoom:   "%s이 %s에게 고개를 끄덕거립니다.",
		},
		"아니": {
			soloActor:    "당신은 고개를 가로젓습니다.",
			soloRoom:     "%s이 고개를 가로젓습니다.",
			targetActor:  "당신은 %s에게 고개를 가로 젓습니다.",
			targetTarget: "%s이 당신에게 고개를 가로젓습니다.",
			targetRoom:   "%s이 %s에게 고개를 가로젓습니다.",
		},
		"감": {
			soloActor:    "당신은 진심으로 감사해 합니다.",
			soloRoom:     "%s이 진심으로 감사해 합니다.",
			targetActor:  "당신은 %s에게 진심으로 감사해 합니다.",
			targetTarget: "%s이 당신에게 진심으로 감사해 합니다.",
			targetRoom:   "%s이 %s에게 진심으로 감사해 합니다.",
		},
		"미소": {
			soloActor:    "당신은 밝은 미소를 짓습니다.",
			soloRoom:     "%s이 밝은 미소를 짓습니다.",
			targetActor:  "당신은 %s에게 미소를 보냅니다.",
			targetTarget: "%s이 당신에게 미소를 짓습니다.",
			targetRoom:   "%s이 %s에게 미소를 짓습니다.",
		},
		"청혼": {
			soloActor:    "당신은 혼자서 청혼을 합니다.",
			soloRoom:     "%s이 혼자서 청혼을 합니다.",
			targetActor:  "당신은 %s에게 청혼을 합니다.",
			targetTarget: "%s이 당신에게 청혼을 합니다.",
			targetRoom:   "%s이 %s에게 청혼을 합니다.",
		},
		"떨어": {
			soloActor:    "당신은 무서워서 벌벌 떱니다.",
			soloRoom:     "%s이 무서워서 벌벌 떱니다.",
			targetActor:  "당신은 %s을 보고 무서워서 벌벌 떱니다.",
			targetTarget: "%s이 당신을 보고 무서워서 벌벌 떱니다.",
			targetRoom:   "%s이 %s를 보고 무서워서 벌벌 떱니다.",
		},
		"해": {
			soloActor:    "당신은 심심해 합니다.",
			soloRoom:     "%s이 심심해 합니다.",
			targetActor:  "당신은 %s을 보며 심심해 합니다.",
			targetTarget: "%s이 당신을 보며 심심해 합니다.",
			targetRoom:   "%s이 %s을 보며 심심해 합니다.",
		},
		"하품": {
			soloActor:    "당신은 자지러지게 하품을 합니다. 아함~",
			soloRoom:     "%s이 자지러지게 하품을 합니다. 아함~",
			targetActor:  "당신은 %s을 보고 하품을 합니다. 아함~",
			targetTarget: "%s이 당신을 보고 하품을 합니다. 아함~",
			targetRoom:   "%s이 %s를 보고 하품을 합니다. 아함~",
		},
		"웃어": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 생긋 웃습니다.",
			soloRoom:     "%s이 생긋 웃습니다.",
			targetActor:  "당신은 %s 보며 생긋 웃습니다.",
			targetTarget: "%s이 당신을 보며 생긋 웃습니다.",
			targetRoom:   "%s이 %s 보며 생긋 웃습니다.",
		},
		"미안": {
			soloActor:    "당신은 미안해 합니다.",
			soloRoom:     "%s이 미안해 합니다.",
			targetActor:  "당신은 %s에게 미안해 합니다.",
			targetTarget: "%s이 당신에게 미안해 합니다.",
			targetRoom:   "%s이 %s에게 미안해 합니다.",
		},
		"악수": {
			particle:     emoteWithParticle,
			soloActor:    "당신은 손을 내밀어 악수를 청합니다.",
			soloRoom:     "%s이 손을 내밀어 악수를 청합니다.",
			targetActor:  "당신은 %s 악수를 합니다.",
			targetTarget: "%s이 당신과 악수를 합니다.",
			targetRoom:   "%s이 %s 악수를 합니다.",
		},
		"하이파이브": {
			particle:     emoteWithParticle,
			soloActor:    "당신은 손을 들어 허공에다 휘젓습니다.",
			soloRoom:     "%s이 손을 들어 허공에다 휘젓습니다.",
			targetActor:  "당신은 %s 손을 높이들어 하이파이브를 합니다.",
			targetTarget: "%s이 당신과 손을 높이들어 하이파이브를 합니다.",
			targetRoom:   "%s이 %s 손을 높이들어 하이파이브를 합니다.",
		},
		"박수": {
			soloActor:    "당신은 박수를 칩니다. 짝짝~",
			soloRoom:     "%s이 박수를 칩니다. 짝짝~",
			targetActor:  "당신은 %s에게 박수를 칩니다. 짝짝~",
			targetTarget: "%s이 당신에게 박수를 칩니다. 짝짝~",
			targetRoom:   "%s이 %s에게 박수를 칩니다. 짝짝~",
		},
		"흡연": {
			soloActor:    "당신은 담배를 뻐금뻐금 피웁니다. 푸우~~~~",
			soloRoom:     "%s이 담배를 뻐금뻐금 피웁니다. 푸우~~",
			targetActor:  "당신은 %s에게 담배를 권합니다.",
			targetTarget: "%s이 당신에게 담배를 권합니다.",
			targetRoom:   "%s이 %s에게 담배를 권합니다.",
		},
		"절": {
			soloActor:    "당신은 공손히 절을 합니다.",
			soloRoom:     "%s이 공손히 절을 합니다.",
			targetActor:  "당신은 %s에게 공손히 절을 합니다.",
			targetTarget: "%s이 당신에게 공손히 절을 합니다.",
			targetRoom:   "%s이 %s에게 공손히 절을 합니다.",
		},
		"찔러": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 손을 올려 허공을 쿡쿡 찌릅니다.",
			soloRoom:     "%s이 손을 올려 허공을 쿡쿡 찌릅니다.",
			targetActor:  "당신은 %s 쿡쿡 찌릅니다.",
			targetTarget: "%s이 당신을 쿡쿡 찌릅니다.",
			targetRoom:   "%s이 %s 쿡쿡 찌릅니다.",
		},
		"춤": {
			particle:     emoteWithParticle,
			soloActor:    "당신은 혼자서 신나게 춤을 춥니다. '대구.부산.찍고~광주.턴!'",
			soloRoom:     "%s이 혼자서 신나게 춤을 춥니다. '대구.부산.찍고~광주.턴!'",
			targetActor:  "당신은 %s 신나게 춤을 춥니다. '대구.부산.찍고~광주.턴!'",
			targetTarget: "%s이 당신과 신나게 춤을 춥니다. '대구.부산.찍고~광주.턴!'",
			targetRoom:   "%s이 %s 신나게 춤을 춥니다. '대구.부산.찍고~광주.턴!'",
		},
		"노래": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 즐겁게 노래를 부릅니다.",
			soloRoom:     "%s이 즐겁게 노래를 부릅니다.",
			targetActor:  "당신은 %s 위해 즐겁게 노래를 부릅니다.",
			targetTarget: "%s이 당신을 위해 즐겁게 노래를 부릅니다.",
			targetRoom:   "%s이 %s 위해 즐겁게 노래를 부릅니다.",
		},
		"울어": {
			soloActor:    "당신은 슬프게 웁니다. 아앙~",
			soloRoom:     "%s이 슬프게 웁니다. 아앙~",
			targetActor:  "당신은 %s에게 눈물을 보입니다.",
			targetTarget: "%s이 당신에게 눈물을 보입니다.",
			targetRoom:   "%s이 %s에게 눈물을 보입니다.",
		},
		"달래": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 자신을 달래 줍니다.",
			soloRoom:     "%s이 자신을 달래 줍니다.",
			targetActor:  "당신은 %s 달래 줍니다.",
			targetTarget: "%s이 당신을 달래 줍니다.",
			targetRoom:   "%s이 %s 달래 줍니다.",
		},
		"당황": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 당황해 합니다.",
			soloRoom:     "%s이 당황해 합니다.",
			targetActor:  "당신은 %s 보고 당황해 합니다.",
			targetTarget: "%s이 당신을 보고 당황해합니다.",
			targetRoom:   "%s이 %s 보고 당황해 합니다.",
		},
		"생각": {
			targetlessOnly: true,
			soloActor:      "당신은 조심스럽게 생각합니다.",
			soloRoom:       "%s이 조심스럽게 생각합니다.",
		},
		"부끄러": {
			targetlessOnly: true,
			soloActor:      "당신은 얼굴이 빨개져 부끄러워 합니다.",
			soloRoom:       "%s이 얼굴이 빨개져 부끄러워 합니다.",
		},
		"놀려": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 약오르게 놀립니다. 메롱메롱~~",
			soloRoom:     "%s이 약오르게 놀립니다. 메롱메롱~~",
			targetActor:  "당신은 %s 약오르게 놀립니다. 메롱메롱~~",
			targetTarget: "%s이 당신을 약오르게 놀립니다. 메롱메롱~~",
			targetRoom:   "%s이 %s 보고 약오르게 놀립니다. 메롱메롱~~",
		},
		"설레": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 마음이 설레입니다.",
			soloRoom:     "%s이 마음이 설레입니다.",
			targetActor:  "당신은 %s 보고 마음이 설레입니다.",
			targetTarget: "%s이 당신을 보고 당황해합니다.",
			targetRoom:   "%s이 %s 보고 마음이 설레입니다.",
		},
		"바이": {
			soloActor:    "당신은 작별인사를 합니다.",
			soloRoom:     "%s이 작별인사를 합니다.",
			targetActor:  "당신은 %s에게 작별인사를 합니다.",
			targetTarget: "%s이 당신에게 작별인사를 합니다.",
			targetRoom:   "%s이 %s에게 작별인사를 합니다.",
		},
		"안녕": {
			soloActor:    "당신은 인사를 합니다. \"안녕하세요~\"",
			soloRoom:     "%s이 인사를 합니다. \"안녕하세요~\"",
			targetActor:  "당신은 %s에게 인사를 합니다. \"안녕하세요~\"",
			targetTarget: "%s이 당신에게 인사를 합니다. \"안녕하세요~\"",
			targetRoom:   "%s이 %s에게 인사를 합니다. \"안녕하세요~\"",
		},
		"뽀뽀": {
			soloActor:    "당신은 자기 손바닥에다 뽀뽀를 합니다.",
			soloRoom:     "%s이 자기 손바닥에다 뽀뽀를 합니다.",
			targetActor:  "당신은 %s에게 뽀뽀를 합니다.",
			targetTarget: "%s이 당신에게 뽀뽀를 합니다.",
			targetRoom:   "%s이 %s에게 뽀뽀를 합니다.",
		},
		"윙크": {
			soloActor:    "당신은 윙크를 합니다.",
			soloRoom:     "%s이 윙크를 합니다.",
			targetActor:  "당신은 %s에게 윙크를 합니다.",
			targetTarget: "%s이 당신에게 윙크를 합니다.",
			targetRoom:   "%s이 %s에게 윙크를 합니다.",
		},
		"구걸": {
			soloActor:    "당신은 바닥에 엎드려 구걸합니다. \"한푼줍쇼~~\"",
			soloRoom:     "%s이 바닥에 엎드려 구걸합니다. \"한푼줍쇼~~\"",
			targetActor:  "당신은 %s에게 구걸합니다. \"한푼줍쇼~~\"",
			targetTarget: "%s이 당신에게 구걸합니다. \"한푼줍쇼~~\"",
			targetRoom:   "%s이 %s에게 구걸합니다. \"한푼줍쇼~~\"",
		},
		"구박": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 스스로를 마구 구박합니다.",
			soloRoom:     "%s이 스스로를 마구 구박합니다.",
			targetActor:  "당신은 %s 마구 구박합니다.",
			targetTarget: "%s이 당신을 마구 구박합니다.",
			targetRoom:   "%s이 %s 마구 구박합니다.",
		},
		"안아": {
			particle:     emoteObjectParticle,
			soloActor:    "당신은 허공을 껴안으려 애를 씁니다.",
			soloRoom:     "%s이 허공을 껴안으려 애를 쓰고 있습니다.",
			targetActor:  "당신은 %s 꼭 껴안습니다.",
			targetTarget: "%s이 당신을 꼭 껴안습니다.",
			targetRoom:   "%s이 %s 꼭 껴안습니다.",
		},
	}

	// Aliases share the action.c branch and therefore share the exact
	// template.  Keep the canonical entries above readable and explicit.
	for alias, canonical := range map[string]string{
		"응":   "끄덕",
		"감사":  "감",
		"담배":  "흡연",
		"잘가":  "바이",
		"껴안아": "안아",
	} {
		specs[alias] = specs[canonical]
	}
	return specs
}()

var emoteAliasOrder = []string{
	"감정표현", "노려봐", "끄덕", "응", "아니", "감", "감사", "미소", "청혼", "떨어", "해", "하품", "웃어", "미안", "악수", "하이파이브", "박수", "흡연", "담배", "절", "찔러", "춤", "노래", "울어", "달래", "당황", "생각", "부끄러", "놀려", "설레", "잘가", "바이", "안녕", "뽀뽀", "윙크", "구걸", "구박", "안아", "껴안아",
}

// EmoteAliases returns the closed action alias set in stable source order.
// It is useful to command-boundary code without exposing the mutable table.
func EmoteAliases() []string {
	return append([]string(nil), emoteAliasOrder...)
}

// IsEmoteAlias reports whether alias is one of the exact command-table
// entries admitted by this slice. Prefix/occurrence matching remains a
// parser concern and is intentionally not inferred here.
func IsEmoteAlias(alias string) bool {
	_, ok := emoteSpecs[strings.TrimSpace(alias)]
	return ok
}

// PlanEmote applies the action.c durable boundary for one exact alias and an
// optional same-room player target. C clears PHIDDN before checking PSILNC;
// this reducer preserves that ordering, including for a silenced actor.
//
// Target resolution is deliberately narrower than legacy find_crt: only an
// exact, online same-room player is admitted. NPC targets, occurrence
// numbers, prefixes, and extra action arguments have not been proven against
// the canonical Go identity graph and therefore fail closed.
func (s State) PlanEmote(actorID, alias, targetName string) (State, EmoteResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, EmoteResult{}, err
	}
	spec, ok := emoteSpecs[strings.TrimSpace(alias)]
	if !ok {
		return State{}, EmoteResult{}, fmt.Errorf("unsupported emote alias %q", alias)
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return State{}, EmoteResult{}, fmt.Errorf("online emote actor absent")
	}
	targetName = strings.TrimSpace(targetName)
	if spec.targetlessOnly && targetName != "" {
		return State{}, EmoteResult{}, fmt.Errorf("emote %q does not accept a target", alias)
	}

	targetID := ""
	if targetName != "" {
		var err error
		targetID, err = s.SelectPlayerInRoom(actorID, targetName)
		if err != nil {
			return State{}, EmoteResult{}, fmt.Errorf("emote target is not an exact same-room player: %w", err)
		}
		if !emoteTargetVisible(actor, s.Players[targetID]) {
			return State{}, EmoteResult{}, fmt.Errorf("emote target is not visible to actor")
		}
	}

	next := s.clone()
	actor = next.Players[actorID]
	actor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[actorID] = actor
	result := EmoteResult{TargetID: targetID}
	if flag(actor.Body.Flags[:], playerSilentStateFlag) {
		result.Response = "한마디도 할수 없습니다!\r\n"
		return next, result, nil
	}
	event, ok, err := next.RoomEmoteEvent(actorID, strings.TrimSpace(alias), targetID)
	if err != nil {
		return State{}, EmoteResult{}, err
	}
	if !ok {
		return State{}, EmoteResult{}, fmt.Errorf("emote event unavailable")
	}
	result.Response = emoteLocal(eventActorText(next, event, spec))
	result.Broadcast = event.Text != ""
	result.Event = &event
	return next, result, nil
}

// RoomEmoteEvent projects a committed action receipt. It is safe to call
// after a receipt has committed; callers should suppress it on receipt replay
// just as the say/movement publishers do. A silent actor clears no additional
// state and produces no room or recipient event.
func (s State) RoomEmoteEvent(actorID, alias, targetID string) (EmoteEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return EmoteEvent{}, false, err
	}
	spec, ok := emoteSpecs[strings.TrimSpace(alias)]
	if !ok {
		return EmoteEvent{}, false, fmt.Errorf("unsupported emote alias %q", alias)
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return EmoteEvent{}, false, fmt.Errorf("online emote actor absent")
	}
	if flag(actor.Body.Flags[:], playerSilentStateFlag) {
		return EmoteEvent{}, false, nil
	}
	if spec.targetlessOnly && targetID != "" {
		return EmoteEvent{}, false, fmt.Errorf("emote %q does not accept a target", alias)
	}

	var target PlayerState
	if targetID != "" {
		var exists bool
		target, exists = s.Players[targetID]
		if !exists || !target.Online || targetID == actorID || target.Body.RoomID != actor.Body.RoomID || !roomContainsPlayer(s.Rooms[actor.Body.RoomID], targetID) {
			return EmoteEvent{}, false, fmt.Errorf("emote target is not an exact same-room player")
		}
		if !emoteTargetVisible(actor, target) {
			return EmoteEvent{}, false, fmt.Errorf("emote target is not visible to actor")
		}
		if spec.targetActor == "" || spec.targetTarget == "" || spec.targetRoom == "" {
			return EmoteEvent{}, false, fmt.Errorf("emote target projection is not admitted")
		}
	}

	actorName := emotePlayerName(actor.Body.Name)
	event := EmoteEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		TargetID:       targetID,
		ExcludeActorID: actorID,
		Text:           emoteRoom(emoteSoloRoom(spec, actorName)),
	}
	if targetID == "" {
		return event, true, nil
	}
	targetName := emotePlayerName(target.Body.Name)
	targetRef := emoteTargetRef(target.Body.Name, targetName, spec.particle)
	event.TargetName = target.Body.Name
	event.ExcludeTargetID = targetID
	event.TargetText = emoteDirect(fmt.Sprintf(spec.targetTarget, actorName))
	event.Text = emoteRoom(fmt.Sprintf(spec.targetRoom, actorName, targetRef))
	return event, true, nil
}

// ExcludeTargetID is kept separate from TargetID in the event shape so a
// future publisher can make the recipient exclusion explicit without having
// to infer it from the presence of TargetText.
//
// (The field is declared in the type below rather than embedded in the
// renderer so JSON/receipt consumers see the same deterministic projection.)

func roomContainsPlayer(room RoomState, id string) bool {
	for _, candidate := range room.PlayerIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

// emoteTargetVisible mirrors the find_crt visibility gate for the player-only
// target boundary.  A hidden target that the actor cannot detect is not
// silently downgraded to a targetless emote, because doing so would create an
// unproven room side effect.  DM-invisible identities are not rendered by
// this static event projection even when a privileged caller could detect
// them; recipient-specific formatter parity remains a later slice.
func emoteTargetVisible(actor, target PlayerState) bool {
	if flag(target.Body.Flags[:], playerDMInvisibleFlag) {
		return false
	}
	return !flag(target.Body.Flags[:], playerInvisibleFlag) || flag(actor.Body.Flags[:], playerDetectFlag)
}

func eventActorText(s State, event EmoteEvent, spec emoteSpec) string {
	if event.TargetID == "" {
		return spec.soloActor
	}
	target := s.Players[event.TargetID]
	targetRef := emoteTargetRef(target.Body.Name, emotePlayerName(target.Body.Name), spec.particle)
	return fmt.Sprintf(spec.targetActor, targetRef)
}

func emoteSoloRoom(spec emoteSpec, actorName string) string {
	return fmt.Sprintf(spec.soloRoom, actorName)
}

func emotePlayerName(name string) string {
	return name + "님"
}

func emoteTargetRef(rawName, displayName string, particle emoteParticle) string {
	switch particle {
	case emoteObjectParticle:
		return displayName + emoteJosa(rawName, "을", "를")
	case emoteWithParticle:
		return displayName + emoteJosa(rawName, "과", "와")
	default:
		return displayName
	}
}

// emoteJosa follows src/io.c's Josa[2]/Josa[3] selection.  The C formatter
// evaluates the final Hangul syllable of the rendered player name; players
// are rendered with a trailing 님, so ASCII names deterministically select the
// same consonant branch once that honorific is materialized.
func emoteJosa(rawName, withFinal, withoutFinal string) string {
	if hasFinalHangul(rawName + "님") {
		return withFinal
	}
	return withoutFinal
}

func hasFinalHangul(text string) bool {
	for len(text) > 0 {
		r, size := utf8.DecodeLastRuneInString(text)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if r == ')' {
			text = text[:len(text)-size]
			continue
		}
		if r < 0xAC00 || r > 0xD7A3 {
			return false
		}
		return (r-0xAC00)%28 != 0
	}
	return false
}

func emoteLocal(text string) string { return text + "\r\n" }

func emoteRoom(text string) string { return "\n" + text + "\r\n" }

func emoteDirect(text string) string { return "\n" + text + "\r\n" }
