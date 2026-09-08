package world

import "fmt"

// PlayerStatus renders the durable subset of command4.c health/score. Titles,
// ANSI coloring and locale-specific class titles remain transport concerns;
// the numeric values come only from the authoritative player and equipment.
func (s State) PlayerStatus(actorID string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online || p.Items == nil {
		return "", fmt.Errorf("online status actor with migrated equipment required")
	}
	if flag(p.Body.Flags[:], 42) {
		return "당신은 눈이 멀어 있습니다!\r\n", nil
	}
	combat, err := p.Items.CombatStats(p.Body)
	if err != nil {
		return "", err
	}
	need := ExperienceThreshold(p.Body.Level)
	target := int64(need) - int64(p.Body.Experience)
	if p.Body.Class == 10 {
		target = int64(p.Body.Experience) - 100000000
	}
	if target < 0 {
		target = 0
	}
	return fmt.Sprintf("%s : 레벨 %d\r\n [체력] %d/%d [도력] %d/%d [방어력] %d\r\n [목표치] %d [돈] %d냥\r\n 당신은 %s서 있습니다.\r\n", p.Body.Name, p.Body.Level, p.Body.HPCurrent, p.Body.HPMax, p.Body.MPCurrent, p.Body.MPMax, 100-int(combat.Armor), target, p.Body.Gold, p.Body.Description), nil
}
