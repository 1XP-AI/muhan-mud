package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func teachStateFixture(class byte) State {
	actor := LegacyMonster{
		Name: "Alice", Type: 0, Class: class, Level: 20, RoomID: 1,
	}
	// 삭풍 is spell index 1, level 2.  A mage can teach it.
	actor.Spells[1/8] |= 1 << (1 % 8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice", "bob", "observer"},
		}},
		Players: map[string]PlayerState{
			"alice": {Body: actor, Online: true},
			"bob":   {Body: LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true},
			"observer": {
				Body:   LegacyMonster{Name: "Observer", Type: 0, RoomID: 1},
				Online: true,
			},
		},
	}
}

func TestPlanAndApplyTeachSetsTargetSpellAndClearsActorHidden(t *testing.T) {
	s := teachStateFixture(teachMageClass)
	actor := s.Players["alice"]
	actor.Body.Flags[teachPlayerHiddenFlag/8] |= 1 << (teachPlayerHiddenFlag % 8)
	s.Players["alice"] = actor
	original := s
	proposal, err := s.PlanTeach("alice", "bob", "삭풍", 1)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "bob" || proposal.TargetKind != TeachTargetPlayer || proposal.SpellIndex != 1 || proposal.SpellName != "삭풍" || proposal.SpellLevel != 2 || !proposal.Changed || !proposal.Broadcast {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated source state")
	}

	next, result, err := s.ApplyTeach(proposal)
	if err != nil {
		t.Fatal(err)
	}
	targetBody := next.Players["bob"].Body
	actorBody := next.Players["alice"].Body
	if !flag(targetBody.Spells[:], 1) || flag(actorBody.Flags[:], teachPlayerHiddenFlag) {
		t.Fatalf("teacher/target state not updated: actor=%v target=%v", next.Players["alice"].Body, next.Players["bob"].Body)
	}
	if result.Action != "teach" || result.ActorID != "alice" || result.TargetID != "bob" || result.SpellName != "삭풍" || result.Event == nil || result.Event.ExcludeActorID != "alice" || result.Event.ExcludeTargetID != "bob" {
		t.Fatalf("result=%+v", result)
	}
	if result.Response != proposal.Response || result.Event.TargetText != proposal.TargetText || result.Event.Text != proposal.RoomText {
		t.Fatalf("recipient projections differ: result=%+v proposal=%+v", result, proposal)
	}
	if !strings.Contains(result.Response, "삭풍") || !strings.Contains(result.Event.TargetText, "Alice") || !strings.Contains(result.Event.Text, "Bob") {
		t.Fatalf("unexpected teach output: %+v", result)
	}
	if _, _, err := next.ApplyTeach(proposal); err == nil {
		t.Fatal("same proposal applied twice without a new plan")
	}
}

func TestPlanTeachUsesCanonicalRoomOrderKeysAndOccurrence(t *testing.T) {
	s := teachStateFixture(teachMageClass)
	room := s.Rooms[1]
	room.PlayerIDs = []string{"alice", "first", "second"}
	s.Rooms[1] = room
	first := s.Players["bob"]
	first.Body.Name = "Borin"
	first.Body.Keys[0] = "Bob"
	second := s.Players["observer"]
	second.Body.Name = "Bora"
	second.Body.Keys[0] = "Bob"
	s.Players["first"] = first
	s.Players["second"] = second
	delete(s.Players, "bob")
	delete(s.Players, "observer")

	proposal, err := s.PlanTeach("alice", "bob", "삭풍", 2)
	if err != nil || proposal.TargetID != "second" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if _, err := s.PlanTeach("alice", "bob", "삭풍", 3); !errors.Is(err, ErrTeachTargetUnavailable) {
		t.Fatalf("missing occurrence err=%v", err)
	}

	// The source normalizes only the first ASCII target byte before find_crt.
	if proposal, err := s.PlanTeach("alice", "bor", "삭풍", 1); err != nil || proposal.TargetID != "first" {
		t.Fatalf("ASCII target normalization proposal=%+v err=%v", proposal, err)
	}
}

func TestPlanTeachHonorsVisibilityAndPermissionGates(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "blind", mutate: func(s *State) {
			p := s.Players["alice"]
			p.Body.Flags[teachPlayerBlindFlag/8] |= 1 << (teachPlayerBlindFlag % 8)
			s.Players["alice"] = p
		}, want: ErrTeachActorBlind},
		{name: "silent", mutate: func(s *State) {
			p := s.Players["alice"]
			p.Body.Flags[teachPlayerSilentFlag/8] |= 1 << (teachPlayerSilentFlag % 8)
			s.Players["alice"] = p
		}, want: ErrTeachActorSilent},
		{name: "teacher class", mutate: func(s *State) {
			p := s.Players["alice"]
			p.Body.Class = 4
			s.Players["alice"] = p
		}, want: ErrTeachTeacherClass},
		{name: "source spell", mutate: func(s *State) {
			p := s.Players["alice"]
			p.Body.Spells = [16]byte{}
			s.Players["alice"] = p
		}, want: ErrTeachSourceSpell},
		{name: "target invisible", mutate: func(s *State) {
			p := s.Players["bob"]
			p.Body.Flags[teachPlayerInvisibleFlag/8] |= 1 << (teachPlayerInvisibleFlag % 8)
			s.Players["bob"] = p
		}, want: ErrTeachTargetUnavailable},
		{name: "target DM invisible caretaker", mutate: func(s *State) {
			p := s.Players["bob"]
			p.Body.Class = teachCaretakerClass
			p.Body.Flags[teachPlayerDMInvisibleFlag/8] |= 1 << (teachPlayerDMInvisibleFlag % 8)
			s.Players["bob"] = p
		}, want: ErrTeachTargetUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := teachStateFixture(teachMageClass)
			tc.mutate(&s)
			before := s
			_, err := s.PlanTeach("alice", "bob", "삭풍", 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want errors.Is(%v)", err, tc.want)
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("rejected teach mutated source state")
			}
		})
	}
	// A cleric may teach level-1 회복 but not level-2 발광.  The source spell
	// bit is present, so this exercises the teacher-level gate rather than the
	// earlier "not learned" branch.
	s := teachStateFixture(teachMageClass)
	cleric := s.Players["alice"]
	cleric.Body.Class = teachClericClass
	cleric.Body.Spells = [16]byte{}
	cleric.Body.Spells[2/8] |= 1 << (2 % 8) // 발광
	s.Players["alice"] = cleric
	if _, err := s.PlanTeach("alice", "bob", "발광", 1); !errors.Is(err, ErrTeachPermission) {
		t.Fatalf("level permission err=%v", err)
	}

	// A detector can see ordinary invisibility, matching find_crt's PDINVI
	// branch; DM invisibility for caretaker+ remains hidden.
	s = teachStateFixture(teachMageClass)
	p := s.Players["bob"]
	p.Body.Flags[teachPlayerInvisibleFlag/8] |= 1 << (teachPlayerInvisibleFlag % 8)
	s.Players["bob"] = p
	actor := s.Players["alice"]
	actor.Body.Flags[teachPlayerDetectFlag/8] |= 1 << (teachPlayerDetectFlag % 8)
	s.Players["alice"] = actor
	if _, err := s.PlanTeach("alice", "bob", "삭풍", 1); err != nil {
		t.Fatalf("detector could not teach visible invisible target: %v", err)
	}
}

func TestPlanTeachRejectsAmbiguousSpellAndUnresolvedCatalog(t *testing.T) {
	s := teachStateFixture(teachMageClass)
	if _, err := s.PlanTeach("alice", "bob", "수", 1); !errors.Is(err, ErrTeachSpellAmbiguous) {
		t.Fatalf("ambiguous spell err=%v", err)
	}
	if _, err := s.PlanTeach("alice", "bob", "없는주문", 1); !errors.Is(err, ErrTeachSpellUnavailable) {
		t.Fatalf("unknown spell err=%v", err)
	}
	original := legacyInfoSpellNames[1]
	legacyInfoSpellNames[1] = ""
	t.Cleanup(func() { legacyInfoSpellNames[1] = original })
	if _, err := s.PlanTeach("alice", "bob", "삭풍", 1); !errors.Is(err, ErrTeachSpellUnavailable) {
		t.Fatalf("unresolved catalog err=%v", err)
	}
}

func TestApplyTeachRejectsStaleActorOrTargetWithoutPartialMutation(t *testing.T) {
	s := teachStateFixture(teachMageClass)
	proposal, err := s.PlanTeach("alice", "bob", "삭풍", 1)
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	target := changed.Players["bob"]
	target.Body.Name = "Bobby"
	changed.Players["bob"] = target
	before := changed
	if _, _, err := changed.ApplyTeach(proposal); err == nil {
		t.Fatal("stale target proposal accepted")
	}
	if !reflect.DeepEqual(changed, before) {
		t.Fatal("stale apply mutated source state")
	}

	changed = s.clone()
	room := changed.Rooms[1]
	room.PlayerIDs = []string{"alice", "observer", "bob"}
	changed.Rooms[1] = room
	if _, _, err := changed.ApplyTeach(proposal); err == nil {
		t.Fatal("stale room order accepted")
	}
}

func TestPlanTeachDoesNotTreatNPCAsPlayerTarget(t *testing.T) {
	s := teachStateFixture(teachMageClass)
	room := s.Rooms[1]
	room.NPCIDs = []string{"wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{"wolf": {Body: LegacyMonster{Name: "Wolf", Type: 1, RoomID: 1}}}
	// teach never falls back to NPCIDs, even though the canonical NPC graph is
	// present and valid.
	if _, err := s.PlanTeach("alice", "Wolf", "삭풍", 1); !errors.Is(err, ErrTeachTargetUnavailable) {
		t.Fatalf("teach accepted an NPC target: %v", err)
	}
}
