package world

import (
	"reflect"
	"testing"
)

func TestDirectionalTraversalChecksSexBeforeCarriedWeight(t *testing.T) {
	e := LegacyExit{}
	e.Flags[0] |= 128
	e.Flags[1] |= 16 // naked + female-only
	v := TraversalInput{HP: 30, CarriedWeight: 1, Male: true}
	got, err := EvaluateDirectionalTraversal(e, nil, v, nil)
	if err != nil || !got.Stop || got.Message != "여성만 들어갈수 있습니다. 여탕인가~~" {
		t.Fatalf("%+v %v", got, err)
	}
	v.Male = false
	got, err = EvaluateDirectionalTraversal(e, nil, v, nil)
	if err != nil || got.Message != "뭘 가지고는 들어갈수 없습니다." {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestDirectionalTraversalGuardAndFall(t *testing.T) {
	e := LegacyExit{}
	e.Flags[2] |= 4 // guard
	m := LegacyMonster{}
	m.Flags[4] |= 64
	got, err := EvaluateDirectionalTraversal(e, []LegacyMonster{m}, TraversalInput{HP: 30, Class: 4}, nil)
	if err != nil || !got.Stop || got.GuardIndex != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	e.Flags = [4]byte{}
	e.Flags[1] |= 2 // rappel
	calls := 0
	roll := func(low, high int) int {
		calls++
		if calls == 1 {
			return 1
		}
		return 7
	}
	got, err = EvaluateDirectionalTraversal(e, nil, TraversalInput{HP: 30}, roll)
	if err != nil || got.HP != 23 || !got.Fell || got.Stop || got.Message != "당신은 구덩이에 떨어져서 7 만큼의 상처를 입었습니다" {
		t.Fatalf("%+v %v", got, err)
	}
	calls = 0
	e.Flags[1] |= 1 // climb stops after damage
	got, err = EvaluateDirectionalTraversal(e, nil, TraversalInput{HP: 7}, roll)
	if err != nil || got.HP != 0 || !got.Dead || !got.Stop {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = EvaluateDirectionalTraversal(e, nil, TraversalInput{HP: 30}, func(int, int) int { return 0 })
	if err == nil || !reflect.DeepEqual(got, TraversalResult{}) {
		t.Fatal("invalid RNG produced partial state")
	}
}
