package world

import "fmt"

// ImportInvitations is an explicit, one-time migration candidate, not a login
// hook. The caller supplies the complete decoded invite_N inventory after
// character import and persists this candidate atomically. An empty map means
// confirmed no files; nil means unknown and must never silently become empty.
// Exact name comparison matches legacy strcmp; duplicate slots grant no extra
// rights. Missing/ambiguous identities and malformed input require resolution.
func (s State) ImportInvitations(source map[int16][]string) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if source == nil || s.Invitations != nil {
		return State{}, fmt.Errorf("invitation import requires complete source and unimported destination")
	}
	byName := map[string][]string{}
	for id, player := range s.Players {
		byName[player.Body.Name] = append(byName[player.Body.Name], id)
	}
	resolved := make(map[int16][]string, len(source))
	for property, names := range source {
		if len(names) > 10 {
			return State{}, fmt.Errorf("invitation file exceeds ten slots")
		}
		resolved[property] = nil
		seen := map[string]bool{}
		for _, name := range names {
			ids := byName[name]
			if name == "" || len(ids) != 1 {
				return State{}, fmt.Errorf("invitation name is unresolved or ambiguous")
			}
			if !seen[ids[0]] {
				resolved[property] = append(resolved[property], ids[0])
				seen[ids[0]] = true
			}
		}
	}
	next := s.clone()
	next.Invitations = resolved
	return next, nil
}
