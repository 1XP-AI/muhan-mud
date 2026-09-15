package transport

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// submitFamilyNewsAppendContinuation owns the connection-local newsedit loop
// from post.c. Each non-terminator line is one durable append receipt; `.`
// only exits the editor after a successful session. Persist failures keep the
// per-line command ID and must not print `->` or the done banner.
func (c *worldConnection) submitFamilyNewsAppendContinuation(ctx context.Context, draft *composeDraft, line string) (string, error) {
	if draft == nil || draft.kind != composeFamilyNewsAppend {
		c.clearCompose()
		return "명령을 처리할 수 없습니다.\r\n", nil
	}
	if strings.HasPrefix(line, ".") {
		pending := draft.title != ""
		c.clearCompose()
		if pending {
			return world.FamilyNewsAppendRetryResponse, nil
		}
		return world.FamilyNewsAppendDoneResponse, nil
	}
	if draft.commandID == "" || (draft.title != "" && draft.title != line) {
		draft.commandID = "family-news-append-" + rand.Text()
	}
	draft.title = line
	receipt, err := c.game.owners.ExecuteFamilyNewsAppendLine(
		ctx, c.game.config.Store, c.game.config.WorldID,
		draft.commandID, c.lease, line, c.game.config.FamilyCatalog,
	)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.clearCompose()
			c.ready = false
			return "", err
		}
		if familyNewsFailClosed(err) {
			c.clearCompose()
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		if errors.Is(err, world.ErrFamilyNewsLimit) {
			return world.FamilyNewsLimitResponse, nil
		}
		if errors.Is(err, world.ErrFamilyNewsLineInvalid) {
			return world.FamilyNewsLineInvalidResponse, nil
		}
		return world.FamilyNewsAppendRetryResponse, nil
	}
	var result world.FamilyNewsResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return world.FamilyNewsAppendRetryResponse, nil
	}
	draft.title = ""
	draft.commandID = ""
	if result.Response == world.FamilyNewsNotMemberResponse {
		c.clearCompose()
	}
	return result.Response, nil
}

func familyNewsFailClosed(err error) bool {
	return errors.Is(err, session.ErrUnsupportedFamilyNewsLine) ||
		errors.Is(err, world.ErrFamilyNewsUnresolved) ||
		errors.Is(err, world.ErrFamilyNewsInvalid) ||
		errors.Is(err, world.ErrFamilyNewsActorAbsent) ||
		errors.Is(err, world.ErrFamilyNewsIdentityUnresolved) ||
		errors.Is(err, world.ErrFamilyNewsStateInvalid) ||
		errors.Is(err, world.ErrFamilyNewsStaleProposal) ||
		errors.Is(err, world.ErrFamilyNewsInvalidProposal) ||
		errors.Is(err, world.ErrFamilyCatalogUnavailable) ||
		errors.Is(err, world.ErrFamilyCatalogInvalid)
}
