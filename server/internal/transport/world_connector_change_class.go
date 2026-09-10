package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// submitChangeClassContinuation owns the connection-local confirmation that
// command7.c starts after a bare `직업전환`. Prompting and cancellation do not
// cross the durable boundary; only the exact affirmative response submits the
// existing canonical one-line reducer with the draft's stable command ID.
func (c *worldConnection) submitChangeClassContinuation(ctx context.Context, draft *composeDraft, line string) (string, error) {
	if draft == nil || draft.kind != composeChangeClass {
		return "명령을 처리할 수 없습니다.\r\n", nil
	}
	if strings.TrimSpace(line) != "예" {
		c.clearCompose()
		return session.ChangeClassCancelResponse, nil
	}
	receipt, err := c.game.owners.ExecuteChangeClassLineWithOptions(
		ctx, c.game.config.Store, c.game.config.WorldID, draft.commandID, c.lease,
		"직업전환 예", session.ChangeClassOptions{},
	)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", err
		}
		if changeClassDomainError(err) {
			c.clearCompose()
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		// Keep the draft and command ID for a retry when the commit outcome is
		// uncertain. Reusing the same receipt identity makes this idempotent.
		return session.ChangeClassRetryResponse, nil
	}
	var result world.ChangeClassResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		// A committed receipt with an unreadable response must remain retryable;
		// clearing here would lose the only connection-local idempotency key.
		return session.ChangeClassRetryResponse, nil
	}
	c.clearCompose()
	return result.Response, nil
}

func changeClassDomainError(err error) bool {
	return errors.Is(err, session.ErrUnsupportedChangeClassLine) ||
		errors.Is(err, world.ErrChangeClassActorAbsent) ||
		errors.Is(err, world.ErrChangeClassRoomAbsent) ||
		errors.Is(err, world.ErrChangeClassRoom) ||
		errors.Is(err, world.ErrChangeClassBlind) ||
		errors.Is(err, world.ErrChangeClassUnsupportedClass) ||
		errors.Is(err, world.ErrChangeClassSameClass) ||
		errors.Is(err, world.ErrChangeClassExperience) ||
		errors.Is(err, world.ErrChangeClassFamilyPending) ||
		errors.Is(err, world.ErrChangeClassStaleProposal) ||
		errors.Is(err, world.ErrChangeClassNumeric) ||
		errors.Is(err, world.ErrChangeClassConfirmation)
}
