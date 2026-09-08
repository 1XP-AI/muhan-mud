package world

// FamilyWar preserves the original AT_WAR / CALLWAR1 / CALLWAR2 encoding.
// Active packs two family numbers in base 16; zero means no active war.
// Declaration/acceptance commands and import validation remain separate work.
type FamilyWar struct{ Active, CalledBy, CalledAgainst byte }

func (w FamilyWar) AllowsDeathLoss(first, second byte) bool {
	if first == 0 || second == 0 || w.Active == 0 {
		return true
	}
	a, b := w.Active/16, w.Active%16
	return !((first == a && second == b) || (first == b && second == a))
}

// AfterPlayerDeath is independent of survival and attacker identity in die.
// DL_EXPND is slot 9, not the marriage field in slot 8. Returning defeated
// requests the two family announcements, published only after full commit.
func (w FamilyWar) AfterPlayerDeath(player LegacyMonster) (FamilyWar, bool) {
	family := player.Daily[9].Max
	if player.Type != 0 || !flag(player.Flags[:], 57) || family == 0 || w.Active == 0 {
		return w, false
	}
	if family == w.Active/16 || family == w.Active%16 {
		return FamilyWar{}, true
	}
	return w, false
}
