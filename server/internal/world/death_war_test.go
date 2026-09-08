package world

import "testing"

func TestDeathWarLossAndLeaderDefeat(t *testing.T) {
	war := FamilyWar{Active: 2*16 + 3, CalledBy: 3, CalledAgainst: 2}
	for _, tc := range []struct {
		first, second byte
		loss          bool
	}{{2, 3, false}, {3, 2, false}, {2, 2, true}, {0, 3, true}, {2, 4, true}} {
		if war.AllowsDeathLoss(tc.first, tc.second) != tc.loss {
			t.Fatalf("%+v", tc)
		}
	}
	for _, leader := range []bool{false, true} {
		for _, family := range []byte{0, 2, 3, 4} {
			p := LegacyMonster{}
			p.Daily[9].Max = family
			p.Daily[8].Max = 4
			if leader {
				p.Flags[7] |= 2
			}
			next, defeated := war.AfterPlayerDeath(p)
			want := leader && (family == 2 || family == 3)
			if defeated != want || (want && next != (FamilyWar{})) || (!want && next != war) {
				t.Fatalf("family%d leader%v %+v %v", family, leader, next, defeated)
			}
		}
	}
}

func TestPeaceDeathDoesNotCancelPendingDeclaration(t *testing.T) {
	war := FamilyWar{CalledBy: 2, CalledAgainst: 3}
	p := LegacyMonster{}
	p.Daily[9].Max = 2
	p.Flags[7] = 2
	next, defeated := war.AfterPlayerDeath(p)
	if next != war || defeated || !war.AllowsDeathLoss(2, 3) {
		t.Fatal("peace treated as active war")
	}
}
