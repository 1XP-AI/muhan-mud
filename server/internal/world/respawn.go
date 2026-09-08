package world

import "fmt"

type PermanentSpawn struct {
	TemplateID int16
	Count      int
}

// PlanPermanentSpawns is the shared count phase in add_permcrt/obj_rom.
// existing counts ONLY permanent entities by exact name, not template ID.
// Returned requests assume every requested spawn succeeds; instantiation and
// application must be all-or-nothing. Missing due templates abort the proposal
// rather than quietly publishing an incomplete world as the C loader did.
func PlanPermanentSpawns(slots [10]LegacyTimer, templates map[int16]string, existing map[string]int, now int64) ([]PermanentSpawn, error) {
	counts := make(map[string]int, len(existing))
	for name, count := range existing {
		if count < 0 {
			return nil, fmt.Errorf("negative permanent entity count")
		}
		counts[name] = count
	}
	var checked [10]bool
	var requests []PermanentSpawn
	due := func(s LegacyTimer) int64 { return int64(s.LastTime) + int64(s.Interval) }
	for i, s := range slots {
		if checked[i] || s.Misc == 0 || due(s) > now {
			continue
		}
		needed := 1
		for j := i + 1; j < len(slots); j++ {
			// Source intentionally uses strict < for followers, <= for the leader.
			if slots[j].Misc == s.Misc && due(slots[j]) < now {
				needed++
				checked[j] = true
			}
		}
		name, ok := templates[s.Misc]
		if !ok {
			return nil, fmt.Errorf("missing permanent template %d", s.Misc)
		}
		missing := needed - counts[name]
		if missing > 0 {
			requests = append(requests, PermanentSpawn{TemplateID: s.Misc, Count: missing})
			counts[name] += missing
		}
	}
	return requests, nil
}

func PermanentMonsterCounts(monsters []LegacyMonster) map[string]int {
	result := map[string]int{}
	for _, m := range monsters {
		if flag(m.Flags[:], 0) {
			result[m.Name]++
		}
	}
	return result
}

func PermanentObjectCounts(objects []LegacyObject) map[string]int {
	result := map[string]int{}
	for _, o := range objects {
		if flag(o.Flags[:], 0) {
			result[o.Name]++
		}
	}
	return result
}
