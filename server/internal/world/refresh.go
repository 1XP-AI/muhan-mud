package world

import "fmt"

type SpawnCatalog interface {
	Monster(int16) (LegacyMonster, error)
	Object(int16) (LegacyObject, error)
}

func cloneObjects(in []LegacyObject) []LegacyObject {
	out := append([]LegacyObject(nil), in...)
	for i := range out {
		out[i].Contents = cloneObjects(out[i].Contents)
	}
	return out
}

// RefreshRoomResources composes the resource phase of room entry. It produces
// an isolated replacement value only on complete success, including nested
// existing inventory. Caller must commit it with movement/arrival atomically.
// Catalog is one stable snapshot. RNG is consumed even on an aborted attempt;
// the future transaction/replay layer must own its seed, not retry blindly.
func RefreshRoomResources(room LegacyRoom, catalog SpawnCatalog, now int32, roll func(int, int) int) (LegacyRoom, error) {
	return refreshRoomResources(room, catalog, now, roll, nil)
}

// onObject observes only newly spawned floor objects, never existing objects.
func refreshRoomResources(room LegacyRoom, catalog SpawnCatalog, now int32, roll func(int, int) int, onObject func(LegacyObject)) (LegacyRoom, error) {
	return refreshRoomResourcesWithNPCEvents(room, catalog, now, roll, onObject, nil)
}

// onNPC observes each new instance at its exact insertion position. Existing
// instances are never reconstructed from names or body equality.
func refreshRoomResourcesWithNPCEvents(room LegacyRoom, catalog SpawnCatalog, now int32, roll func(int, int) int, onObject func(LegacyObject), onNPC func(int, LegacyMonster) error) (LegacyRoom, error) {
	monsterNames := map[int16]string{}
	monsterTemplates := map[int16]LegacyMonster{}
	due := func(t LegacyTimer) bool { return t.Misc != 0 && int64(t.LastTime)+int64(t.Interval) <= int64(now) }
	for _, t := range room.PermanentMonsters {
		if !due(t) {
			continue
		}
		if _, ok := monsterTemplates[t.Misc]; ok {
			continue
		}
		if catalog == nil {
			return LegacyRoom{}, fmt.Errorf("missing spawn catalog")
		}
		m, err := catalog.Monster(t.Misc)
		if err != nil {
			return LegacyRoom{}, err
		}
		monsterNames[t.Misc] = m.Name
		monsterTemplates[t.Misc] = m
	}
	requests, err := PlanPermanentSpawns(room.PermanentMonsters, monsterNames, PermanentMonsterCounts(room.Monsters), int64(now))
	if err != nil {
		return LegacyRoom{}, err
	}
	out := room
	out.Objects = cloneObjects(room.Objects)
	out.Monsters = append([]LegacyMonster(nil), room.Monsters...)
	for i := range out.Monsters {
		out.Monsters[i].Inventory = cloneObjects(out.Monsters[i].Inventory)
	}
	for _, request := range requests {
		for n := 0; n < request.Count; n++ {
			m, err := SpawnPermanentMonster(monsterTemplates[request.TemplateID], now, catalog.Object, roll)
			if err != nil {
				return LegacyRoom{}, err
			}
			m.RoomID = room.ID
			at := len(out.Monsters)
			for i, existing := range out.Monsters {
				if existing.Name > m.Name {
					at = i
					break
				}
			}
			out.Monsters = append(out.Monsters, LegacyMonster{})
			copy(out.Monsters[at+1:], out.Monsters[at:])
			out.Monsters[at] = m
			if onNPC != nil {
				if err := onNPC(at, m); err != nil {
					return LegacyRoom{}, err
				}
			}
		}
	}
	objectNames := map[int16]string{}
	objectTemplates := map[int16]LegacyObject{}
	for _, t := range room.PermanentObjects {
		if !due(t) {
			continue
		}
		if _, ok := objectTemplates[t.Misc]; ok {
			continue
		}
		if catalog == nil {
			return LegacyRoom{}, fmt.Errorf("missing spawn catalog")
		}
		o, err := catalog.Object(t.Misc)
		if err != nil {
			return LegacyRoom{}, err
		}
		if len(o.Contents) != 0 {
			return LegacyRoom{}, fmt.Errorf("catalog object has unexpected contents")
		}
		objectNames[t.Misc] = o.Name
		objectTemplates[t.Misc] = o
	}
	requests, err = PlanPermanentSpawns(room.PermanentObjects, objectNames, PermanentObjectCounts(room.Objects), int64(now))
	if err != nil {
		return LegacyRoom{}, err
	}
	for _, request := range requests {
		for n := 0; n < request.Count; n++ {
			o := objectTemplates[request.TemplateID]
			if flag(o.Flags[:], 21) {
				o, err = EnchantObject(o, roll)
				if err != nil {
					return LegacyRoom{}, err
				}
			}
			o.Flags[0] |= 1
			if onObject != nil {
				onObject(o)
			}
			at := len(out.Objects)
			for i, existing := range out.Objects {
				if existing.Name > o.Name || (existing.Name == o.Name && int8(existing.Adjustment) > int8(o.Adjustment)) {
					at = i
					break
				}
			}
			out.Objects = append(out.Objects, LegacyObject{})
			copy(out.Objects[at+1:], out.Objects[at:])
			out.Objects[at] = o
		}
	}
	out.Exits = RefreshDoors(room.Exits, int64(now))
	return out, nil
}
