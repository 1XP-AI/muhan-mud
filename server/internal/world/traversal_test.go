package world

import "testing"

func TestTraversalGuardAndWeightOrder(t *testing.T) {
	e := LegacyExit{Flags: [4]byte{128, 0, 4}}
	guards := []LegacyMonster{{Name: "경비", Flags: [8]byte{0, 0, 0, 0, 64}}}
	result, err := EvaluateTraversal(e, guards, TraversalInput{HP: 30, CarriedWeight: 1}, nil)
	if err != nil || !result.Stop || result.GuardIndex != 0 {
		t.Fatalf("guard order: %+v %v", result, err)
	}
	result, err = EvaluateTraversal(e, guards, TraversalInput{HP: 30, CarriedWeight: 1, Invisible: true}, nil)
	if err != nil || result.Message != "뭘 가지고는 들어갈 수 없습니다." {
		t.Fatalf("%+v %v", result, err)
	}
	guards[0].Flags[2] = 32
	result, _ = EvaluateTraversal(e, guards, TraversalInput{HP: 30, Invisible: true}, nil)
	if result.GuardIndex != 0 {
		t.Fatal("detect invisible guard bypassed")
	}
	result, _ = EvaluateTraversal(e, guards, TraversalInput{HP: 30, Class: 11}, nil)
	if result.Stop {
		t.Fatal("sub-DM guard bypass lost")
	}
}

func TestTraversalFallTransitions(t *testing.T) {
	for _, tc := range []struct {
		name             string
		flags            [4]byte
		hp, roll, damage int
		stop, dead       bool
		wantHP           int
	}{
		{"climb-fall", [4]byte{0, 1}, 30, 49, 7, true, false, 23},
		{"repel-fall", [4]byte{0, 2}, 30, 49, 7, false, false, 23},
		{"exact-threshold", [4]byte{0, 1}, 30, 50, 0, false, false, 30},
		{"lethal", [4]byte{0, 1}, 7, 1, 7, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			rng := func(lo, hi int) int {
				calls++
				if calls == 1 {
					if lo != 1 || hi != 100 {
						t.Fatal("roll bounds")
					}
					return tc.roll
				}
				if lo != 5 || hi != 20 {
					t.Fatal("damage bounds")
				}
				return tc.damage
			}
			r, err := EvaluateTraversal(LegacyExit{Flags: tc.flags}, nil, TraversalInput{HP: tc.hp}, rng)
			if err != nil || r.HP != tc.wantHP || r.Stop != tc.stop || r.Dead != tc.dead {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}

func TestTraversalLevitationAndInvalidRandom(t *testing.T) {
	e := LegacyExit{Flags: [4]byte{0, 1}}
	if r, err := EvaluateTraversal(e, nil, TraversalInput{HP: 10, Levitating: true}, nil); err != nil || r.Stop {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := EvaluateTraversal(e, nil, TraversalInput{HP: 10}, func(int, int) int { return 101 }); err == nil {
		t.Fatal("invalid random accepted")
	}
}
