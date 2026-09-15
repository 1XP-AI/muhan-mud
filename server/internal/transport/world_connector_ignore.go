package transport

import (
	"fmt"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	ignoreListResponsePrefix = "듣기 거부된 사용자: "
	ignoreOfflineResponse    = "그 사용자는 접속중이 아닙니다.\n"
	ignoreLimitResponse      = "더 이상 듣기 거부 대상을 추가할 수 없습니다.\n"
)

// submitIgnoreLine handles command9.c's descriptor-local list. It is called
// while the owning connection and world command locks are held, but it never
// enters the receipt/state reducer: ignore entries are intentionally lost on
// disconnect and must not become persistent game data.
func (c *worldConnection) submitIgnoreLine(line string, state world.State, stateOK bool) (string, bool, error) {
	command, ok := session.ParseIgnoreLine(line)
	if !ok {
		return "", false, nil
	}
	if command.Kind == session.IgnoreList {
		return renderIgnoreList(c.ignore.List()), true, nil
	}

	// Removal is allowed after the target logs out, exactly like command9.c:
	// first_ignore is searched before find_who is consulted for an add.
	if c.ignore.Contains(command.Target) {
		result, err := c.ignore.Toggle(command.Target)
		if err != nil {
			return "", true, err
		}
		if result.Removed {
			return fmt.Sprintf("%s님을 이야기 듣기 거부 대상에서 삭제합니다.", result.Name), true, nil
		}
	}

	if !stateOK {
		return "명령을 처리할 수 없습니다.\r\n", true, nil
	}
	actor, actorOK := state.Players[c.lease.ActorID]
	if !actorOK || !actor.Online || actor.Body.Type != 0 {
		return "명령을 처리할 수 없습니다.\r\n", true, nil
	}
	targetID, targetName, found := c.findIgnoreTarget(state, command.Target)
	if !found || c.liveConnection(targetID) == nil || world.PlayerFlagSet(state.Players[targetID].Body, 10) { // PDMINV
		return ignoreOfflineResponse, true, nil
	}
	result, err := c.ignore.Toggle(targetName)
	if err != nil {
		if err == ErrIgnoreLimit {
			return ignoreLimitResponse, true, nil
		}
		return "", true, err
	}
	if result.Added {
		return fmt.Sprintf("%s님을 이야기 듣기 거부 대상에 추가합니다.\n", result.Name), true, nil
	}
	// The Contains branch above should have consumed removal. Keep this
	// fail-closed fallback if a future IgnoreList implementation changes that
	// invariant rather than claiming an add that did not happen.
	return ignoreOfflineResponse, true, nil
}

func renderIgnoreList(names []string) string {
	if len(names) == 0 {
		return ignoreListResponsePrefix + "없음.\n"
	}
	return ignoreListResponsePrefix + strings.Join(names, ", ") + ".\n"
}

// findIgnoreTarget resolves the exact canonical display name used by C's
// find_who. Prefix matching is deliberately not used for this command.
func (c *worldConnection) findIgnoreTarget(state world.State, selector string) (string, string, bool) {
	var targetID, targetName string
	for id, player := range state.Players {
		if id == "" || !player.Online || player.Body.Type != 0 {
			continue
		}
		canonical, err := CanonicalIgnoreName(player.Body.Name)
		if err != nil || canonical != selector {
			continue
		}
		// A duplicate canonical name is not a safe identity. C's descriptor
		// table has a stable scan order; the canonical map does not, so reject
		// ambiguity rather than choosing a random player.
		if targetID != "" {
			return "", "", false
		}
		targetID, targetName = id, canonical
	}
	return targetID, targetName, targetID != ""
}

func (c *worldConnection) liveConnection(actorID string) *worldConnection {
	c.game.mu.Lock()
	defer c.game.mu.Unlock()
	for connection := range c.game.connections {
		// Membership is the connector's synchronized live-descriptor index;
		// do not read the target's connection-local ready/closed flags here,
		// because its Close path owns those fields under a different mutex.
		if connection.lease.ActorID == actorID {
			return connection
		}
	}
	return nil
}

// directMessageIgnored performs the non-durable listener check that lives in
// command4.c's descriptor extra state. The durable world reducer still owns
// target/visibility/message validation; this helper only suppresses delivery
// when the target's live connection has explicitly ignored the sender.
func (c *worldConnection) directMessageIgnored(state world.State, line string) (string, bool) {
	command, ok := session.ParseDirectMessageLine(line)
	if !ok || command.Target == "" || command.Text == "" {
		return "", false
	}
	return c.directMessageIgnoredForTarget(state, line, "", command.Target)
}

// directMessageIgnoredForTarget is the same descriptor-local listener check
// for `대답`/`/`, whose target was already captured from an incoming event.
// An empty targetID keeps the original `얘기` path on selector resolution;
// replies additionally require the exact identity to remain unchanged.
func (c *worldConnection) directMessageIgnoredForTarget(state world.State, line, targetID, targetName string) (string, bool) {
	var text string
	if targetID == "" {
		command, ok := session.ParseDirectMessageLine(line)
		if !ok || command.Target == "" || command.Text == "" {
			return "", false
		}
		targetName, text = command.Target, command.Text
	} else {
		command, ok := session.ParseReplyLine(line)
		if !ok || targetName == "" {
			return "", false
		}
		text = command.Text
	}
	proposal, err := state.PlanDirectMessage(c.lease.ActorID, targetName, text)
	if err != nil || !proposal.Delivered {
		return "", false
	}
	if targetID != "" && proposal.TargetID != targetID {
		return "", false
	}
	target := c.liveConnection(proposal.TargetID)
	if target == nil || !target.ignore.Contains(proposal.SenderName) {
		return "", false
	}
	return fmt.Sprintf("%s is ignoring you.\r\n", proposal.TargetName), true
}
