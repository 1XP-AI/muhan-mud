package world

import (
	"reflect"
	"testing"
)

func TestPlanNPCDeathMovesLegacyInventoryAndGoldToCanonicalFloor(t *testing.T) {
	s := npcAttackFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	npc := s.NPCs["wolf-id"]
	npc.Body.HPCurrent = 0
	npc.Body.Inventory = []LegacyObject{{Name: "검", Value: 7}}
	npc.Body.Gold = 12
	npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 50}}
	s.NPCs["wolf-id"] = npc
	allocated := 0
	next, result, err := s.PlanNPCDeath("wolf-id", "a", 77, func() (string, error) {
		allocated++
		return "drop-" + string(rune('0'+allocated)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DroppedObjectCount != 2 || result.DroppedGold != 12 || allocated != 2 {
		t.Fatalf("drop result=%+v allocated=%d", result, allocated)
	}
	if got := len(next.Rooms[1].Items.Items); got != 2 {
		t.Fatalf("floor item count=%d", got)
	}
	var foundGold, foundSword bool
	for _, id := range next.Rooms[1].Items.Inventory {
		item := next.Rooms[1].Items.Items[id]
		switch item.Object.Type {
		case 10:
			foundGold = item.Object.Value == 12 && item.Object.Name == "12냥"
		default:
			foundSword = item.Object.Name == "검"
		}
	}
	if !foundGold || !foundSword {
		t.Fatalf("floor drops=%+v", next.Rooms[1].Items)
	}
	if _, exists := next.NPCs["wolf-id"]; exists {
		t.Fatal("dead NPC remained")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanNPCDeathUpdatesPermanentTimerAndFollowerEdges(t *testing.T) {
	s := npcAttackFixture(t)
	r := s.Rooms[1]
	r.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 44, LastTime: 1, Interval: 10}
	r.NPCIDs = []string{"wolf-id"}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.NPCFollowerIDs = []string{"wolf-id"}
	p.FollowerRefs = []EntityRef{{Kind: "npc", ID: "wolf-id"}}
	s.Players["a"] = p
	npc := s.NPCs["wolf-id"]
	npc.Body.HPCurrent = 0
	npc.Body.Flags[0] |= 1 // MPERMT
	npc.PermanentOrigin = &NPCPermanentOrigin{RoomID: 1, Slot: 0}
	npc.FollowingPlayerID = "a"
	npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 1}}
	s.NPCs["wolf-id"] = npc
	next, result, err := s.PlanNPCDeath("wolf-id", "a", 99, nil)
	if err != nil || !result.PermanentTimerUpdated {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Rooms[1].Resource.PermanentMonsters[0].LastTime != 99 || len(next.Players["a"].NPCFollowerIDs) != 0 || len(next.Players["a"].FollowerRefs) != 0 {
		t.Fatalf("timer/follower state rooms=%+v player=%+v", next.Rooms[1].Resource.PermanentMonsters[0], next.Players["a"])
	}
	if !reflect.DeepEqual(s.Players["a"].NPCFollowerIDs, []string{"wolf-id"}) {
		t.Fatal("death mutated source follower state")
	}
}

func TestPlanNPCDeathRejectsSummonWithoutPartialState(t *testing.T) {
	s := npcAttackFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.HPCurrent = 0
	npc.Body.Flags[npcSummonFlag/8] |= 1 << (npcSummonFlag % 8)
	npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 1}}
	s.NPCs["wolf-id"] = npc
	want := s.clone()
	next, result, err := s.PlanNPCDeath("wolf-id", "a", 1, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || result != (NPCDeathResult{}) || !reflect.DeepEqual(s, want) {
		t.Fatalf("summon death partially committed: next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestPlanNPCDeathAppliesQuestExperienceAndUnassignedProficiency(t *testing.T) {
	s := npcAttackFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.HPCurrent = 0
	npc.Body.HPMax = 1
	npc.Body.Experience = 10
	npc.Body.Quest = 1
	npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 1}}
	s.NPCs["wolf-id"] = npc
	next, result, err := s.PlanNPCDeath("wolf-id", "a", 1, nil)
	if err != nil || result.ExperienceAward != 10 || result.QuestExperienceAward != 120 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	player := next.Players["a"].Body
	if player.Experience != 130 || player.Quests[0]&1 == 0 {
		t.Fatalf("quest reward body=%+v", player)
	}
	for i, value := range player.Proficiency {
		if value != 13 {
			t.Fatalf("proficiency[%d]=%d", i, value)
		}
	}
	for i, value := range player.Realm {
		if value != 13 {
			t.Fatalf("realm[%d]=%d", i, value)
		}
	}
}
