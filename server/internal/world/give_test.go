package world

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func giveFixture(t *testing.T) State {
	t.Helper()
	actorItems := ItemCollection{
		Items: map[string]Item{
			"bag":  {Object: LegacyObject{Name: "가방", Weight: 2, Flags: [8]byte{0: 1 << itemContainerFlag}}, Contents: []string{"gem"}},
			"gem":  {Object: LegacyObject{Name: "보석", Weight: 1}},
			"coin": {Object: LegacyObject{Name: "동전", Weight: 1}},
		},
		Inventory: []string{"bag", "coin"},
	}
	targetItems := ItemCollection{
		Items:     map[string]Item{"cloak": {Object: LegacyObject{Name: "망토", Weight: 1}}},
		Inventory: []string{"cloak"},
	}
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor", "target"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "앨리스", Type: 0, RoomID: 1, Gold: 100, Stats: [5]byte{0: 10}, Flags: [8]byte{0: 1 << (playerHiddenStateFlag % 8)}},
				Online: true,
				Items:  &actorItems,
			},
			"target": {
				Body:   LegacyMonster{Name: "밥", Type: 0, RoomID: 1, Gold: 5, Stats: [5]byte{0: 10}},
				Online: true,
				Items:  &targetItems,
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseGiveAmountAdmitsOnlyPositiveDecimalNyang(t *testing.T) {
	for _, test := range []struct {
		token string
		want  int64
	}{
		{token: "1냥", want: 1},
		{token: "100냥", want: 100},
		{token: "2147483647냥", want: 2147483647},
	} {
		got, err := ParseGiveAmount(test.token)
		if err != nil || got != test.want {
			t.Fatalf("ParseGiveAmount(%q)=%d,%v want %d", test.token, got, err, test.want)
		}
	}
	for _, token := range []string{"", "냥", "0냥", "-1냥", "+1냥", "1.5냥", "1", "9223372036854775808냥"} {
		if _, err := ParseGiveAmount(token); err == nil || !errors.Is(err, ErrGiveAmountInvalid) {
			t.Fatalf("ParseGiveAmount(%q) err=%v", token, err)
		}
	}
}

func TestGiveItemMovesWholeSubtreeAndSeparatesResponses(t *testing.T) {
	s := giveFixture(t)
	next, result, err := s.GiveItemByName("actor", "가방", "밥")
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != GiveItem || result.ItemID != "bag" || result.TargetID != "target" || result.TargetKind != GiveTargetPlayer || !result.Broadcast {
		t.Fatalf("result=%+v", result)
	}
	if result.Response == "" || result.TargetResponse == "" || result.ObserverResponse == "" || result.Response == result.TargetResponse || result.TargetResponse == result.ObserverResponse {
		t.Fatalf("responses are not separated: %+v", result)
	}
	if result.Event == nil || result.Event.ActorID != "actor" || result.Event.TargetID != "target" || result.Event.ExcludeActorID != "actor" || result.Event.ExcludeTargetID != "target" || result.Event.TargetText != result.TargetResponse || result.Event.ObserverText != result.ObserverResponse {
		t.Fatalf("event=%+v", result.Event)
	}
	_, bagRemains := next.Players["actor"].Items.Items["bag"]
	_, gemRemains := next.Players["actor"].Items.Items["gem"]
	if bagRemains || gemRemains {
		t.Fatal("source subtree remained after transfer")
	}
	if !containsString(next.Players["target"].Items.Inventory, "bag") || next.Players["target"].Items.Items["bag"].Contents[0] != "gem" || len(next.Players["target"].Items.Items) != 3 {
		t.Fatalf("target inventory=%+v", next.Players["target"].Items)
	}
	nextActor := next.Players["actor"]
	if flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("actor hidden flag was not cleared on successful give")
	}
	originalActor := s.Players["actor"]
	if !flag(originalActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("input state was mutated")
	}
}

func TestGiveMoneyMovesGoldAtomically(t *testing.T) {
	s := giveFixture(t)
	next, result, err := s.GiveMoneyByName("actor", "밥", 40)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != GiveMoney || result.Amount != 40 || result.GoldBefore != 100 || result.GoldAfter != 60 || result.ItemID != "" || result.ItemName != "" {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["actor"].Body.Gold != 60 || next.Players["target"].Body.Gold != 45 {
		t.Fatalf("gold actor=%d target=%d", next.Players["actor"].Body.Gold, next.Players["target"].Body.Gold)
	}
	if result.Event == nil || result.Event.TargetText != result.TargetResponse || result.Event.ObserverText != result.ObserverResponse {
		t.Fatalf("event=%+v", result.Event)
	}
	if !reflect.DeepEqual(s.Players["actor"].Body.Gold, int32(100)) || !reflect.DeepEqual(s.Players["target"].Body.Gold, int32(5)) {
		t.Fatal("input state was mutated")
	}
}

func TestGiveValidatesSourceBranchesBeforeTarget(t *testing.T) {
	s := giveFixture(t)
	if _, _, err := s.GiveItemByName("actor", "없는물건", "없는사람"); !errors.Is(err, ErrGiveItemAbsent) {
		t.Fatalf("item branch error=%v", err)
	}
	if _, _, err := s.GiveMoneyByName("actor", "없는사람", 101); !errors.Is(err, ErrGiveInsufficientGold) {
		t.Fatalf("money branch error=%v", err)
	}
	if _, _, err := s.GiveMoneyByName("actor", "없는사람", 0); !errors.Is(err, ErrGiveAmountInvalid) {
		t.Fatalf("invalid amount error=%v", err)
	}
}

func TestGiveRejectsProtectedDescendantWithoutMutation(t *testing.T) {
	s := giveFixture(t)
	actor := s.Players["actor"]
	child := actor.Items.Items["gem"]
	child.Object.Quest = 1
	actor.Items.Items["gem"] = child
	s.Players["actor"] = actor
	before := s.clone()
	next, _, err := s.GiveItemByName("actor", "가방", "밥")
	if err == nil || !errors.Is(err, ErrGiveNestedProtectedPending) || !errors.Is(err, ErrGiveQuestPending) {
		t.Fatalf("protected descendant err=%v", err)
	}
	if !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(s, before) {
		t.Fatal("protected give mutated state")
	}
}

func TestGiveFailsClosedForCanonicalNPCRecipients(t *testing.T) {
	s := giveFixture(t)
	s.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor", "target"}, NPCIDs: []string{"vendor"}}
	s.NPCs = map[string]NPCState{"vendor": {Body: LegacyMonster{Name: "상인", Type: 1, RoomID: 1}}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	before := s.clone()
	if _, _, err := s.GiveItemByName("actor", "가방", "상인"); !errors.Is(err, ErrGiveNPCPending) {
		t.Fatalf("item NPC error=%v", err)
	}
	if _, _, err := s.GiveMoneyByName("actor", "상인", 1); !errors.Is(err, ErrGiveNPCPending) {
		t.Fatalf("money NPC error=%v", err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("NPC rejection mutated state")
	}
}

func TestGiveRejectsStaleProposalAndGoldOverflow(t *testing.T) {
	s := giveFixture(t)
	proposal, err := s.PlanGive(GiveInput{ActorID: "actor", Kind: GiveMoney, TargetName: "밥", TargetOccurrence: 1, Amount: 1})
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	p := changed.Players["actor"]
	p.Body.Description = "changed after planning"
	changed.Players["actor"] = p
	if _, _, err := changed.ApplyGive(proposal); !errors.Is(err, ErrGiveStaleProposal) {
		t.Fatalf("stale proposal error=%v", err)
	}

	overflow := giveFixture(t)
	p = overflow.Players["target"]
	p.Body.Gold = math.MaxInt32
	overflow.Players["target"] = p
	if _, _, err := overflow.GiveMoneyByName("actor", "밥", 1); !errors.Is(err, ErrGiveGoldOverflow) {
		t.Fatalf("overflow error=%v", err)
	}
}

func TestGiveErrorTextKeepsSourceProtectedReason(t *testing.T) {
	s := giveFixture(t)
	p := s.Players["actor"]
	item := p.Items.Items["bag"]
	item.Object.Quest = 1
	p.Items.Items["bag"] = item
	s.Players["actor"] = p
	_, _, err := s.GiveItemByName("actor", "가방", "밥")
	if err == nil || !strings.Contains(err.Error(), "quest") {
		t.Fatalf("error=%v", err)
	}
}
