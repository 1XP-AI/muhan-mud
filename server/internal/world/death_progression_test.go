package world

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestExperienceLevelBoundaries(t *testing.T) {
	for _, tc := range []struct {
		xp    int32
		level int
	}{{-1, 1}, {127, 1}, {128, 2}, {256, 3}, {99999999, 127}, {100000000, 128}, {105000000, 129}} {
		if got := ExperienceLevel(tc.xp); got != tc.level {
			t.Fatalf("xp%d got%d", tc.xp, got)
		}
	}
}
func TestDeathExperienceSelfNPCAndPvP(t *testing.T) {
	p := LegacyMonster{Level: 10, Experience: 1000}
	for _, rules := range []DeathProgressionRules{{}, {AttackerPlayer: true, Self: true}} {
		got, err := PlanDeathProgression(p, rules)
		if err != nil || got.Experience != 950 {
			t.Fatalf("%+v %v", got, err)
		}
	}
	got, err := PlanDeathProgression(p, DeathProgressionRules{AttackerPlayer: true})
	if err != nil || got.Experience != 1000 {
		t.Fatalf("%+v %v", got, err)
	}
	p.Level = 100
	p.Experience = 10000000
	got, err = PlanDeathProgression(p, DeathProgressionRules{})
	if err != nil || got.Experience != 9900000 {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestDeathPreservesOriginalLevelCorrection(t *testing.T) {
	p := LegacyMonster{Level: 20, Experience: 0}
	got, err := PlanDeathProgression(p, DeathProgressionRules{})
	if err != nil || got.Experience != 6144 || got.TargetLevel != 19 {
		t.Fatalf("original inconsistent-save correction: %+v %v", got, err)
	}
}
func TestDeathSkillLossRedistributesAndKeepsMinimum(t *testing.T) {
	p := LegacyMonster{Level: 1, Proficiency: [5]int32{10000}}
	got, err := PlanDeathProgression(p, DeathProgressionRules{SkillLoss: true})
	if err != nil || got.Proficiency[0] < 1024 || got.Proficiency[0] > 1033 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, v := range got.Realm {
		if v != 0 {
			t.Fatal("added realm")
		}
	}
	got, err = PlanDeathProgression(p, DeathProgressionRules{})
	if err != nil || got.Proficiency != p.Proficiency {
		t.Fatalf("gate ignored %+v %v", got, err)
	}
}

func TestExperienceThresholdTableMatchesLegacySource(t *testing.T) {
	raw, err := os.ReadFile("../../../src/global.c")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)long needed_exp\[\] = \{(.*?)\};`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("missing experience table")
	}
	body := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(match[1]), "")
	fields := strings.Fields(strings.ReplaceAll(body, ",", " "))
	if len(fields) != len(neededExperience) {
		t.Fatal("threshold count changed")
	}
	for i, field := range fields {
		n, err := strconv.ParseInt(field, 10, 32)
		if err != nil || int32(n) != neededExperience[i] {
			t.Fatalf("threshold%d %s Go%d %v", i, field, neededExperience[i], err)
		}
	}
}

func TestDeathSkillMinimumTieAndWideTotals(t *testing.T) {
	p := LegacyMonster{Level: 1}
	got, err := PlanDeathProgression(p, DeathProgressionRules{SkillLoss: true})
	if err != nil || got.Proficiency != ([5]int32{1024}) {
		t.Fatalf("%+v %v", got, err)
	}
	for i := range p.Proficiency {
		p.Proficiency[i] = 2147483647
	}
	for i := range p.Realm {
		p.Realm[i] = 2147483647
	}
	got, err = PlanDeathProgression(p, DeathProgressionRules{SkillLoss: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range got.Proficiency {
		if v < 0 {
			t.Fatal("overflowed skill")
		}
	}
	for _, v := range got.Realm {
		if v < 0 {
			t.Fatal("overflowed realm")
		}
	}
}
