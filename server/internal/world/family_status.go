package world

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// These values are the legacy mtype.h bits/slot used by command11.c's
// family_who/family_member/list_family handlers.  Family membership is kept
// on the canonical player body until the family file migration is admitted.
const (
	FamilyMemberFlag  uint  = 55 // PFAMIL
	FamilyPendingFlag uint  = 56 // PRDFML
	FamilyBossFlag    uint  = 57 // PFMBOS
	FamilyBlindFlag   uint  = 42 // PBLIND
	FamilyDailySlot         = 9  // DL_EXPND
	FamilyMaxID       int16 = 15
)

var (
	ErrFamilyCatalogUnavailable = errors.New("family catalog unavailable")
	ErrFamilyCatalogInvalid     = errors.New("invalid family catalog")
	ErrFamilyActorAbsent        = errors.New("online family actor absent")
	ErrFamilyTargetRequired     = errors.New("family target required")
	ErrFamilyTargetUnavailable  = errors.New("family target unavailable")
	ErrFamilyIdentityUnresolved = errors.New("family identity unresolved")
)

// FamilyDefinition is the server-owned projection of one family_list row.
// The legacy numeric id is the only stable join key; names are display data
// and are never accepted from a client as an authorization selector.
type FamilyDefinition struct {
	ID   int16  `json:"id"`
	Name string `json:"name"`
	Boss string `json:"boss"`
}

// FamilyCatalog is loaded once from the versioned family resource and passed
// to read-only family projections. A nil/empty catalog deliberately does not
// turn a numeric family id into a guessed display name.
type FamilyCatalog struct {
	Families map[int16]FamilyDefinition `json:"families"`
}

func (c FamilyCatalog) Validate() error {
	if c.Families == nil {
		return ErrFamilyCatalogUnavailable
	}
	seenNames := make(map[string]bool, len(c.Families))
	for id, family := range c.Families {
		if id < 1 || id > FamilyMaxID || family.ID != id || !validFamilyText(family.Name) || !validFamilyText(family.Boss) {
			return fmt.Errorf("%w: family %d", ErrFamilyCatalogInvalid, id)
		}
		key := strings.ToLower(family.Name)
		if seenNames[key] {
			return fmt.Errorf("%w: duplicate family name", ErrFamilyCatalogInvalid)
		}
		seenNames[key] = true
	}
	return nil
}

func (c FamilyCatalog) lookup(id int16) (FamilyDefinition, error) {
	if err := c.Validate(); err != nil {
		return FamilyDefinition{}, err
	}
	family, ok := c.Families[id]
	if !ok || id < 1 || id > FamilyMaxID {
		return FamilyDefinition{}, fmt.Errorf("%w: family %d", ErrFamilyCatalogInvalid, id)
	}
	return family, nil
}

func validFamilyText(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// FamilyMemberView is a canonical online member rendered by family_who or
// family_member. Pending applications are shown with the source "(-)" marker.
type FamilyMemberView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Class   byte   `json:"class"`
	Pending bool   `json:"pending,omitempty"`
}

// FamilyWhoProjection contains both the semantic lookup and the rendered
// response. TargetName is empty for the roster form. Members are ordered by
// canonical room membership followed by sorted residual online IDs, never by
// Go map iteration.
type FamilyWhoProjection struct {
	ActorID    string             `json:"actor_id"`
	TargetID   string             `json:"target_id,omitempty"`
	TargetName string             `json:"target_name,omitempty"`
	FamilyID   int16              `json:"family_id,omitempty"`
	FamilyName string             `json:"family_name,omitempty"`
	Pending    bool               `json:"pending,omitempty"`
	Members    []FamilyMemberView `json:"members,omitempty"`
	Response   string             `json:"response"`
}

// FamilyWho is the descriptive result name used by command adapters.
type FamilyWhoResult = FamilyWhoProjection

func familyID(body LegacyMonster) int16 { return int16(body.Daily[FamilyDailySlot].Max) }

func familyActive(body LegacyMonster) bool { return flag(body.Flags[:], FamilyMemberFlag) }

func familyPending(body LegacyMonster) bool { return flag(body.Flags[:], FamilyPendingFlag) }

func familyVisibleTo(viewer, target PlayerState, viewerID, targetID string) bool {
	if targetID == viewerID {
		return true
	}
	// family_who rejects PDMINV unconditionally and applies the ordinary
	// invisible/detect-invisible pair to PINVIS.
	if flag(target.Body.Flags[:], playerDMInvisibleFlag) {
		return false
	}
	return !flag(target.Body.Flags[:], playerInvisibleFlag) || flag(viewer.Body.Flags[:], playerDetectFlag)
}

func familyMemberView(id string, p PlayerState, pending bool) (FamilyMemberView, error) {
	if id == "" || p.Body.Type != 0 || !p.Online || !validFamilyText(p.Body.Name) {
		return FamilyMemberView{}, ErrFamilyIdentityUnresolved
	}
	return FamilyMemberView{ID: id, Name: p.Body.Name, Class: p.Body.Class, Pending: pending}, nil
}

func familyRoster(s State, familyID int16) ([]FamilyMemberView, error) {
	if familyID < 1 || familyID > FamilyMaxID {
		return nil, fmt.Errorf("%w: family id %d", ErrFamilyIdentityUnresolved, familyID)
	}
	members := make([]FamilyMemberView, 0)
	for _, id := range orderedOnlinePlayers(s) {
		p, ok := s.Players[id]
		if !ok || !p.Online || p.Body.Type != 0 {
			continue
		}
		if familyID != familyIDOf(p.Body) || (!familyActive(p.Body) && !familyPending(p.Body)) {
			continue
		}
		member, err := familyMemberView(id, p, familyPending(p.Body))
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, nil
}

// familyIDOf is kept separate from familyID so the comparison above remains
// explicit at call sites and future snapshot versions can change the source
// slot without silently changing the projection.
func familyIDOf(body LegacyMonster) int16 { return familyID(body) }

// ProjectFamilyWho implements the bounded, read-only family_who command. An
// optional exact target reports that character's membership; an empty target
// renders the actor's online roster. Offline-file fallback, name prefixes,
// and unresolved family resources are intentionally rejected.
func (s State) ProjectFamilyWho(actorID, targetName string, catalog FamilyCatalog) (FamilyWhoProjection, error) {
	if err := s.Validate(); err != nil {
		return FamilyWhoProjection{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validFamilyText(actor.Body.Name) {
		return FamilyWhoProjection{}, ErrFamilyActorAbsent
	}
	if flag(actor.Body.Flags[:], FamilyBlindFlag) {
		return FamilyWhoProjection{ActorID: actorID, Response: "당신은 눈이 멀어 있습니다!\r\n"}, nil
	}
	if targetName != "" {
		if !validFamilyText(targetName) {
			return FamilyWhoProjection{}, ErrFamilyTargetRequired
		}
		for _, id := range orderedOnlinePlayers(s) {
			target, exists := s.Players[id]
			if !exists || !target.Online || target.Body.Type != 0 || !strings.EqualFold(target.Body.Name, targetName) {
				continue
			}
			if !familyVisibleTo(actor, target, actorID, id) {
				return FamilyWhoProjection{}, ErrFamilyTargetUnavailable
			}
			active, pending := familyActive(target.Body), familyPending(target.Body)
			if !active && !pending {
				return FamilyWhoProjection{ActorID: actorID, TargetID: id, TargetName: target.Body.Name, Response: fmt.Sprintf("%s님은 어떤 패거리에도 소속되어 있지 않습니다.\r\n", target.Body.Name)}, nil
			}
			fid := familyID(target.Body)
			family, err := catalog.lookup(fid)
			if err != nil {
				return FamilyWhoProjection{}, err
			}
			response := fmt.Sprintf("%s님은 [%s] 패거리에 속해있습니다.\r\n", target.Body.Name, family.Name)
			if pending {
				response = fmt.Sprintf("%s님은 [%s] 패거리에 가입을 신청중입니다.\r\n", target.Body.Name, family.Name)
			}
			return FamilyWhoProjection{ActorID: actorID, TargetID: id, TargetName: target.Body.Name, FamilyID: fid, FamilyName: family.Name, Pending: pending, Response: response}, nil
		}
		return FamilyWhoProjection{}, ErrFamilyTargetUnavailable
	}

	active, pending := familyActive(actor.Body), familyPending(actor.Body)
	if !active && !pending {
		return FamilyWhoProjection{ActorID: actorID, Response: "당신은 어떤 패거리에도 소속되어 있지 않습니다.\r\n"}, nil
	}
	id := familyID(actor.Body)
	family, err := catalog.lookup(id)
	if err != nil {
		return FamilyWhoProjection{}, err
	}
	members, err := familyRoster(s, id)
	if err != nil {
		return FamilyWhoProjection{}, err
	}
	response := fmt.Sprintf("당신은 [%s] 패거리에 소속되어 있습니다.\r\n\r\n", family.Name)
	if pending {
		response = fmt.Sprintf("당신은 [%s] 패거리에 가입을 신청중입니다.\r\n\r\n", family.Name)
	}
	for i, member := range members {
		marker := "   "
		if member.Pending {
			marker = "(-)"
		}
		response += fmt.Sprintf("%s%-14s", marker, member.Name)
		if (i+1)%3 == 0 {
			response += "\r\n"
		}
	}
	if len(members)%3 != 0 {
		response += "\r\n"
	}
	response += fmt.Sprintf("\r\n총 %d명의 패거리원들이 이용중입니다.\r\n", len(members))
	return FamilyWhoProjection{ActorID: actorID, FamilyID: id, FamilyName: family.Name, Pending: pending, Members: members, Response: response}, nil
}

// FamilyWho renders the source-compatible response-only form.
func (s State) FamilyWho(actorID, targetName string, catalog FamilyCatalog) (string, error) {
	projection, err := s.ProjectFamilyWho(actorID, targetName, catalog)
	if err != nil {
		return "", err
	}
	return projection.Response, nil
}

// FamilyMember renders 패거리원's active-member roster. Unlike family_who's
// roster form it does not include pending applications, matching C's separate
// family_member handler.
func (s State) FamilyMember(actorID string, catalog FamilyCatalog) (string, error) {
	projection, err := s.ProjectFamilyWho(actorID, "", catalog)
	if err != nil {
		return "", err
	}
	// family_member checks PFAMIL itself; a pending application is not yet a
	// member and must not be presented as an empty/active roster.
	if projection.FamilyID == 0 || projection.Pending {
		return "당신은 패거리에 가입되어 있지 않습니다.\r\n", nil
	}
	active := make([]FamilyMemberView, 0, len(projection.Members))
	for _, member := range projection.Members {
		if !member.Pending {
			active = append(active, member)
		}
	}
	response := fmt.Sprintf("당신은 [%s] 패거리에 가입되어 있습니다.\r\n", projection.FamilyName)
	for i, member := range active {
		response += fmt.Sprintf("[%d]  %-15s  ", member.Class, member.Name)
		if (i+1)%3 == 0 {
			response += "\r\n"
		}
	}
	if len(active)%3 != 0 {
		response += "\r\n"
	}
	response += fmt.Sprintf("총 %d명의 사람들이 가입되어 있습니다.\r\n", len(active))
	return response, nil
}

// ListFamily renders the server-owned family directory in numeric order.
func (s State) ListFamily(catalog FamilyCatalog) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	if err := catalog.Validate(); err != nil {
		return "", err
	}
	ids := make([]int, 0, len(catalog.Families))
	for id := range catalog.Families {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	var out strings.Builder
	out.WriteString("다음과 같은 패거리가 있습니다.\r\n")
	out.WriteString("패거리이름      두목이름\r\n")
	out.WriteString("--------------------------------------------\r\n")
	for _, rawID := range ids {
		family := catalog.Families[int16(rawID)]
		fmt.Fprintf(&out, "%-14s %-14s\r\n", family.Name, family.Boss)
	}
	fmt.Fprintf(&out, "\r\n총 %d 개의 패거리가 활동중에 있습니다.\r\n", len(ids))
	return out.String(), nil
}
