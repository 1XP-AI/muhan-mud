package world

import "testing"

func TestPermanentCountsIgnoreTemporaryEntities(t *testing.T) {
	monsters := []LegacyMonster{{Name: "늑대", Flags: [8]byte{1}}, {Name: "늑대"}}
	objects := []LegacyObject{{Name: "검", Flags: [8]byte{1}}, {Name: "검"}}
	if PermanentMonsterCounts(monsters)["늑대"] != 1 || PermanentObjectCounts(objects)["검"] != 1 {
		t.Fatal("temporary entities suppress permanent respawn")
	}
}

func TestPermanentSpawnDeadlineGrouping(t *testing.T) {
	slots := [10]LegacyTimer{{LastTime: 10, Interval: 5, Misc: 1}, {LastTime: 10, Interval: 5, Misc: 1}}
	templates := map[int16]string{1: "늑대"}
	at, err := PlanPermanentSpawns(slots, templates, nil, 15)
	if err != nil || len(at) != 1 || at[0].Count != 1 {
		t.Fatalf("equality: %+v %v", at, err)
	}
	after, err := PlanPermanentSpawns(slots, templates, nil, 16)
	if err != nil || len(after) != 1 || after[0].Count != 2 {
		t.Fatalf("after: %+v %v", after, err)
	}
	existing := map[string]int{"늑대": 1}
	pending, err := PlanPermanentSpawns(slots, templates, existing, 16)
	if err != nil || len(pending) != 1 || pending[0].Count != 1 || existing["늑대"] != 1 {
		t.Fatalf("existing: %+v %v", pending, err)
	}
}

func TestPermanentSpawnsShareNameCounts(t *testing.T) {
	slots := [10]LegacyTimer{{Misc: 1}, {Misc: 2}}
	r, err := PlanPermanentSpawns(slots, map[int16]string{1: "동명", 2: "동명"}, nil, 1)
	if err != nil || len(r) != 1 || r[0].TemplateID != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := PlanPermanentSpawns(slots, map[int16]string{1: "동명"}, nil, 1); err == nil {
		t.Fatal("missing template silently skipped")
	}
}

func TestPermanentSpawnFutureAndOverflow(t *testing.T) {
	slots := [10]LegacyTimer{{LastTime: 2147483647, Interval: 2147483647, Misc: 1}}
	r, err := PlanPermanentSpawns(slots, nil, nil, 2147483647)
	if err != nil || len(r) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}
