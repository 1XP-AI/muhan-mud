package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	playerLookupInvisibleFlag   = 2  // PINVIS
	playerLookupDMInvisibleFlag = 10 // PDMINV
	playerLookupDetectFlag      = 21 // PDINVI
	playerLookupBlindFlag       = 42 // PBLIND
	playerLookupSubDMClass      = 11
	playerLookupDMClass         = 12
)

var (
	// ErrPlayerLookupUnavailable is deliberately generic. It covers an absent,
	// offline, ambiguous, or hidden target so the command cannot turn lookup
	// behavior into an existence oracle.
	ErrPlayerLookupUnavailable = errors.New("player lookup target unavailable")
	// ErrPlayerInfoCanonicalOnly marks the pfinger boundary: this slice never
	// loads legacy player files or invents last-access metadata for offline
	// characters.
	ErrPlayerInfoCanonicalOnly = errors.New("canonical online player information required")
	ErrPlayerInfoForbidden     = errors.New("player information forbidden")
)

var playerLookupClassNames = [...]string{
	"제작", "자객", "권법가", "불제자", "검사", "도술사", "무사",
	"포졸", "도둑", "무적", "초인", "운영자", "관리자",
}

var playerLookupRaceNames = [...]string{
	"Unknown", "난장이족", "용신족", "요괴족", "토신족",
	"인간족", "도깨비족", "거인족", "땅귀신족", "개구리족",
}

// PlayerLookupView is the bounded canonical identity projection shared by
// 사용자검색 and 사용자정보. Internal IDs are retained for authority checks
// but are not rendered to the client.
type PlayerLookupView struct {
	ID     string
	Name   string
	Level  byte
	Class  byte
	Race   byte
	Online bool
}

func validPlayerLookupName(name string) bool {
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

func validPlayerLookupTarget(name string) bool {
	return validPlayerLookupName(name) && !strings.ContainsAny(name, " \t\r\n")
}

func (s State) lookupOnlinePlayerExact(actorID, targetName string) (PlayerLookupView, error) {
	if err := s.Validate(); err != nil {
		return PlayerLookupView{}, err
	}
	if !validPlayerLookupTarget(targetName) {
		return PlayerLookupView{}, ErrPlayerLookupUnavailable
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return PlayerLookupView{}, fmt.Errorf("online player lookup actor absent")
	}

	var found PlayerLookupView
	foundCount := 0
	for id, player := range s.Players {
		if !player.Online || player.Body.Type != 0 || !validPlayerLookupName(player.Body.Name) {
			continue
		}
		if !strings.EqualFold(player.Body.Name, targetName) {
			continue
		}
		found = PlayerLookupView{
			ID: id, Name: player.Body.Name, Level: player.Body.Level,
			Class: player.Body.Class, Race: player.Body.Race, Online: true,
		}
		foundCount++
	}
	if foundCount != 1 {
		return PlayerLookupView{}, ErrPlayerLookupUnavailable
	}
	return found, nil
}

func playerLookupLabels(view PlayerLookupView) (string, string, error) {
	if int(view.Class) >= len(playerLookupClassNames) || int(view.Race) >= len(playerLookupRaceNames) {
		return "", "", fmt.Errorf("canonical player lookup has unsupported class or race")
	}
	return playerLookupClassNames[view.Class], playerLookupRaceNames[view.Race], nil
}

// PlayerSearch implements the bounded command5.c:whois projection. It only
// resolves an exact online player in the current world snapshot. Target
// invisibility, DM invisibility, actor blindness, and missing detect-invisible
// capability all collapse to ErrPlayerLookupUnavailable; no partial identity
// is returned for those cases.
func (s State) PlayerSearch(actorID, targetName string) (string, error) {
	view, err := s.lookupOnlinePlayerExact(actorID, targetName)
	if err != nil {
		return "", err
	}
	actor := s.Players[actorID]
	target := s.Players[view.ID]
	if flag(actor.Body.Flags[:], playerLookupBlindFlag) ||
		flag(target.Body.Flags[:], playerLookupDMInvisibleFlag) ||
		(flag(target.Body.Flags[:], playerLookupInvisibleFlag) && !flag(actor.Body.Flags[:], playerLookupDetectFlag)) {
		return "", ErrPlayerLookupUnavailable
	}
	className, raceName, err := playerLookupLabels(view)
	if err != nil {
		return "", err
	}
	// Title, age and the descriptor's gender metadata belong to legacy
	// projections that are not part of this bounded canonical slice. Do not
	// fabricate them merely to match the old table layout.
	return fmt.Sprintf("사용자: %s\r\n레벨: %d\r\n직업: %s\r\n종족: %s\r\n", view.Name, view.Level, className, raceName), nil
}

// PlayerInformation implements the safe online portion of command11.c:
// pfinger. It intentionally does not read a legacy player file, stat ctime,
// mailbox path, suicide marker, title, or last-access value. A missing or
// offline target is an explicit ErrPlayerInfoCanonicalOnly failure.
func (s State) PlayerInformation(actorID, targetName string) (string, error) {
	view, err := s.lookupOnlinePlayerExact(actorID, targetName)
	if err != nil {
		if errors.Is(err, ErrPlayerLookupUnavailable) {
			return "", ErrPlayerInfoCanonicalOnly
		}
		return "", err
	}
	actor := s.Players[actorID]
	target := s.Players[view.ID]
	if flag(target.Body.Flags[:], playerLookupDMInvisibleFlag) &&
		(actor.Body.Class < playerLookupSubDMClass ||
			(actor.Body.Class == playerLookupSubDMClass && target.Body.Class == playerLookupDMClass)) {
		return "", ErrPlayerInfoForbidden
	}
	className, raceName, err := playerLookupLabels(view)
	if err != nil {
		return "", ErrPlayerInfoCanonicalOnly
	}
	return fmt.Sprintf("사용자: %s\r\n종족: %s\r\n직업: %s\r\n현재 접속 중 입니다.\r\n", view.Name, raceName, className), nil
}
