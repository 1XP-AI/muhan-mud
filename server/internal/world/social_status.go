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
	playerInvisibleFlag   = 2
	playerDMInvisibleFlag = 10
	playerDetectFlag      = 21
	playerDMClass         = 12
	playerSubDMClass      = 11
)

var ErrPlayerWhoIdentityUnresolved = errors.New("who player identity unresolved")

// PlayerWhoOptions contains only snapshot-owned options. FamilyCatalog is
// optional because the social adapter predates the family catalog injection;
// without it, a PFAMIL row is rendered without guessing a family label.
type PlayerWhoOptions struct {
	Long          bool
	FamilyCatalog FamilyCatalog
}

func orderedOnlinePlayers(s State) []string {
	seen := make(map[string]bool, len(s.Players))
	ids := make([]string, 0, len(s.Players))
	roomIDs := make([]int, 0, len(s.Rooms))
	for id := range s.Rooms {
		roomIDs = append(roomIDs, int(id))
	}
	sort.Ints(roomIDs)
	for _, roomID := range roomIDs {
		for _, id := range s.Rooms[int16(roomID)].PlayerIDs {
			if p, ok := s.Players[id]; ok && p.Online && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	remaining := make([]string, 0, len(s.Players))
	for id, p := range s.Players {
		if p.Online && !seen[id] {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	return append(ids, remaining...)
}

func visibleToWho(viewer, target PlayerState, viewerID, targetID string) bool {
	if targetID == viewerID {
		return true
	}
	if flag(target.Body.Flags[:], playerDMInvisibleFlag) && (target.Body.Class == playerDMClass || viewer.Body.Class < playerSubDMClass) {
		return false
	}
	if flag(target.Body.Flags[:], playerInvisibleFlag) && !flag(viewer.Body.Flags[:], playerDetectFlag) && viewer.Body.Class < playerSubDMClass {
		return false
	}
	return true
}

func visibleToPlayerWho(viewer, target PlayerState, viewerID, targetID string) bool {
	if targetID == viewerID {
		return true
	}
	// command5.c has two PDMINV gates: DM invisibility is hidden from
	// ordinary viewers, and the general PDMINV gate has the same ordinary-viewer
	// boundary. Together they reduce to this one source-compatible condition;
	// SUB_DM and DM viewers can see the marker.
	if flag(target.Body.Flags[:], playerDMInvisibleFlag) && viewer.Body.Class < playerSubDMClass {
		return false
	}
	if flag(target.Body.Flags[:], playerInvisibleFlag) && !flag(viewer.Body.Flags[:], playerDetectFlag) && viewer.Body.Class < playerSubDMClass {
		return false
	}
	return true
}

// PlayerWho renders a deterministic semantic online-player list. Descriptor
// order from C is unavailable in a snapshot, so room membership order followed
// by sorted residual IDs is explicit rather than map iteration. The optional
// bool is source-compatible with the C `누구 l` long form while preserving the
// existing bare call shape.
func (s State) PlayerWho(actorID string, longMode ...bool) (string, error) {
	if len(longMode) > 1 {
		return "", fmt.Errorf("%w: multiple mode arguments", ErrPlayerWhoIdentityUnresolved)
	}
	return s.PlayerWhoWithOptions(actorID, PlayerWhoOptions{Long: len(longMode) == 1 && longMode[0]})
}

// PlayerWhoLong is the explicit long projection for callers that do not use
// the variadic compatibility form.
func (s State) PlayerWhoLong(actorID string) (string, error) {
	return s.PlayerWho(actorID, true)
}

// PlayerWhoWithOptions renders the bounded command5.c who projection. The
// online count controls the default projection exactly as C does: more than
// 30 online players uses compact level/class output, unless Long is explicit.
func (s State) PlayerWhoWithOptions(actorID string, options PlayerWhoOptions) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	viewer, ok := s.Players[actorID]
	if !ok || !viewer.Online {
		return "", fmt.Errorf("online who viewer absent")
	}
	if flag(viewer.Body.Flags[:], playerBlindFlag) {
		return "당신은 눈이 멀어 있습니다!\r\n", nil
	}
	ids := orderedOnlinePlayers(s)
	for _, id := range ids {
		if err := validateWhoPlayerIdentity(id, s.Players[id]); err != nil {
			return "", err
		}
	}
	compact := len(ids) > 30 && !options.Long
	lines := whoHeader(compact)
	total := 0
	for _, id := range ids {
		p := s.Players[id]
		if !visibleToPlayerWho(viewer, p, actorID, id) {
			continue
		}
		if compact {
			lines = append(lines, whoCompactRow(p))
		} else {
			family, err := whoFamilyLabel(p, options.FamilyCatalog)
			if err != nil {
				return "", err
			}
			lines = append(lines, whoDetailedRow(p, family))
		}
		total++
	}
	if total != 1 {
		lines = append(lines, fmt.Sprintf("\r\n총 %d명의 사용자가 무한대전을 이용하고 있습니다.", total))
	} else {
		lines = append(lines, "\r\n당신 혼자서 외로이 무한대전을 이용하고 있습니다.")
	}
	war, err := whoWarSuffix(s.War, options.FamilyCatalog)
	if err != nil {
		return "", err
	}
	if war != "" {
		lines = append(lines, war)
	}
	return joinStrings(lines), nil
}

func whoHeader(compact bool) []string {
	if compact {
		return []string{
			fmt.Sprintf("%-14s    [레벨 직업]  %-14s    [레벨 직업]\r\n", "사용자", "사용자"),
			strings.Repeat("-", 60) + "\r\n",
		}
	}
	return []string{
		fmt.Sprintf("%-13s     레벨  %-4s %-8s %-6s %-32s\r\n", "사용자", "직업", "종족", "패거리", "칭호"),
		"----------------------------------------------------------------------\r\n",
	}
}

func whoMarker(p PlayerState) string {
	if flag(p.Body.Flags[:], playerInvisibleFlag) || flag(p.Body.Flags[:], playerDMInvisibleFlag) {
		return "(*)"
	}
	return "   "
}

func whoLevel(level byte) string {
	if level >= 100 {
		return fmt.Sprintf("%02d", level)
	}
	return fmt.Sprintf(" %02d", level)
}

func whoCompactRow(p PlayerState) string {
	return fmt.Sprintf("%-14s%s [%s %-4s]\r\n", p.Body.Name, whoMarker(p), whoLevel(p.Body.Level), legacyInfoClassNames[p.Body.Class])
}

func whoDetailedRow(p PlayerState, family string) string {
	return fmt.Sprintf("%-13s%s [%s ] %-4s %-8s [%-4s] %s\r\n", p.Body.Name, whoMarker(p), whoLevel(p.Body.Level), legacyInfoClassNames[p.Body.Class], legacyInfoRaceNames[p.Body.Race], family, p.Title)
}

func validWhoText(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validateWhoPlayerIdentity(id string, p PlayerState) error {
	if id == "" || !p.Online || p.Body.Type != 0 || !validWhoText(p.Body.Name) {
		return fmt.Errorf("%w: player %q", ErrPlayerWhoIdentityUnresolved, id)
	}
	if int(p.Body.Class) >= len(legacyInfoClassNames) || int(p.Body.Race) >= len(legacyInfoRaceNames) {
		return fmt.Errorf("%w: player %q class=%d race=%d", ErrPlayerWhoIdentityUnresolved, id, p.Body.Class, p.Body.Race)
	}
	if p.Title != "" {
		if err := ValidatePlayerTitle(p.Title); err != nil {
			return fmt.Errorf("%w: player %q title: %v", ErrPlayerWhoIdentityUnresolved, id, err)
		}
	}
	if flag(p.Body.Flags[:], FamilyMemberFlag) {
		familyID := int16(p.Body.Daily[FamilyDailySlot].Max)
		if familyID < 1 || familyID > FamilyMaxID {
			return fmt.Errorf("%w: player %q family=%d", ErrPlayerWhoIdentityUnresolved, id, familyID)
		}
	}
	return nil
}

func whoFamilyLabel(p PlayerState, catalog FamilyCatalog) (string, error) {
	if !flag(p.Body.Flags[:], FamilyMemberFlag) {
		return "중립", nil
	}
	familyID := int16(p.Body.Daily[FamilyDailySlot].Max)
	if familyID < 1 || familyID > FamilyMaxID {
		return "", fmt.Errorf("%w: family=%d", ErrPlayerWhoIdentityUnresolved, familyID)
	}
	// A nil catalog is the explicit unimported-family boundary. The row stays
	// in the projection, but no guessed numeric/name label is emitted.
	if catalog.Families == nil {
		return "", nil
	}
	family, err := catalog.lookup(familyID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPlayerWhoIdentityUnresolved, err)
	}
	return family.Name, nil
}

func whoWarSuffix(war *FamilyWar, catalog FamilyCatalog) (string, error) {
	if war == nil || war.Active == 0 {
		return "", nil
	}
	firstID := int16(war.Active / 16)
	secondID := int16(war.Active % 16)
	if firstID < 1 || firstID > FamilyMaxID || secondID < 1 || secondID > FamilyMaxID {
		return "", fmt.Errorf("%w: war=%d", ErrPlayerWhoIdentityUnresolved, war.Active)
	}
	if catalog.Families == nil {
		return "", nil
	}
	first, err := catalog.lookup(firstID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPlayerWhoIdentityUnresolved, err)
	}
	second, err := catalog.lookup(secondID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPlayerWhoIdentityUnresolved, err)
	}
	return fmt.Sprintf("\r\n%s 패거리와 %s 패거리가 전쟁중입니다.", first.Name, second.Name), nil
}

func joinStrings(values []string) string {
	var out string
	for _, value := range values {
		out += value
	}
	return out
}

// PlayerGroup renders the player/NPC first_fol group around the actor. It is
// read-only and keeps the canonical mixed follower order when it is available.
func (s State) PlayerGroup(actorID string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return "", fmt.Errorf("online group viewer absent")
	}
	leaderID := actorID
	if actor.FollowingID != "" {
		leaderID = actor.FollowingID
	}
	leader, ok := s.Players[leaderID]
	if !ok {
		return "", fmt.Errorf("group leader absent")
	}
	refs := mixedFollowerRefs(leader)
	if len(refs) == 0 {
		return "당신은 그룹에 속해 있지 않습니다.\r\n", nil
	}
	var out string
	out += "그룹원:\r\n"
	out += fmt.Sprintf("  %-14s  체력:%4d 도력:%4d (대장)\r\n", leader.Body.Name, leader.Body.HPCurrent, leader.Body.MPCurrent)
	shown := 0
	for _, ref := range refs {
		var name string
		var hp, mp int16
		switch ref.Kind {
		case "player":
			follower, exists := s.Players[ref.ID]
			if !exists || flag(follower.Body.Flags[:], playerDMInvisibleFlag) {
				continue
			}
			name, hp, mp = follower.Body.Name, follower.Body.HPCurrent, follower.Body.MPCurrent
		case "npc":
			npc, exists := s.NPCs[ref.ID]
			if !exists {
				return "", fmt.Errorf("group NPC follower absent")
			}
			name, hp, mp = npc.Body.Name, npc.Body.HPCurrent, npc.Body.MPCurrent
		default:
			return "", fmt.Errorf("unknown group follower kind")
		}
		out += fmt.Sprintf("  %-14s  체력:%4d 도력:%4d\r\n", name, hp, mp)
		shown++
	}
	if shown == 0 {
		return "당신은 그룹에 속해 있지 않습니다.\r\n", nil
	}
	return out, nil
}
