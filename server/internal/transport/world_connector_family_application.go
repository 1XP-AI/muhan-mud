package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// submitFamilyApplicationContinuation owns the connection-local editor for
// the original bare `패거리가입` command.  Listing, choosing and confirming a
// family do not create receipts; only the confirmed canonical family name is
// submitted to the existing world reducer.
func (c *worldConnection) submitFamilyApplicationContinuation(ctx context.Context, draft *composeDraft, line string) (string, error) {
	if draft == nil || draft.kind != composeFamilyApplication {
		return "명령을 처리할 수 없습니다.\r\n", nil
	}
	if draft.familyName == "" {
		name := strings.TrimSpace(line)
		if name == "" || name != line {
			c.clearCompose()
			return session.FamilyApplicationInvalidChoice, nil
		}
		state, ok := c.game.snapshot(ctx)
		if !ok {
			c.clearCompose()
			return "명령을 처리할 수 없습니다.\r\n", nil
		}
		family, found := familyByName(c.game.config.FamilyCatalog, name)
		if !found {
			c.clearCompose()
			return session.FamilyApplicationInvalidChoice, nil
		}
		if _, err := state.PlanFamilyJoinByName(c.lease.ActorID, family.Name, c.game.config.FamilyCatalog); err != nil {
			c.clearCompose()
			return familyApplicationErrorResponse(err, family), nil
		}
		draft.familyName = family.Name
		return fmt.Sprintf(session.FamilyApplicationConfirmPrompt, family.Name), nil
	}

	// add_family accepts only the exact affirmative token; every other answer
	// is a local cancellation and must not cross the durable command boundary.
	if strings.TrimSpace(line) != "예" {
		c.clearCompose()
		return session.FamilyApplicationCancelResponse, nil
	}

	receipt, err := c.game.owners.ExecuteFamilyMutationLineWithCatalog(
		ctx, c.game.config.Store, c.game.config.WorldID, draft.commandID, c.lease,
		"패거리가입 "+draft.familyName, c.game.config.FamilyCatalog,
	)
	if err != nil {
		// A generic store/response failure may be transient. Keep the command ID
		// and selected family so the next confirmation retries the exact request.
		if familyApplicationDomainError(err) {
			c.clearCompose()
			family, _ := familyByName(c.game.config.FamilyCatalog, draft.familyName)
			if family.Name == "" {
				family.Name = draft.familyName
			}
			return familyApplicationErrorResponse(err, family), nil
		}
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", err
		}
		return "가입 신청을 저장하지 못했습니다. 다시 시도해 주세요.\r\n", nil
	}
	var result world.FamilyMutationResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "가입 신청을 저장하지 못했습니다. 다시 시도해 주세요.\r\n", nil
	}
	c.clearCompose()
	if !receipt.Replayed && len(result.Events) != 0 {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishFamilyMutation(after, result.Events)
		}
	}
	return result.Response, nil
}

func familyByName(catalog world.FamilyCatalog, name string) (world.FamilyDefinition, bool) {
	for _, family := range catalog.Families {
		if family.Name == name {
			return family, true
		}
	}
	return world.FamilyDefinition{}, false
}

func familyApplicationDomainError(err error) bool {
	return errors.Is(err, world.ErrFamilyCatalogUnavailable) ||
		errors.Is(err, world.ErrFamilyCatalogInvalid) ||
		errors.Is(err, world.ErrFamilyMutationActorAbsent) ||
		errors.Is(err, world.ErrFamilyMutationIdentityUnresolved) ||
		errors.Is(err, world.ErrFamilyMutationStateInvalid) ||
		errors.Is(err, world.ErrFamilyMutationFamilyRequired) ||
		errors.Is(err, world.ErrFamilyMutationFamilyUnavailable) ||
		errors.Is(err, world.ErrFamilyMutationBossUnavailable) ||
		errors.Is(err, world.ErrFamilyMutationBossAmbiguous) ||
		errors.Is(err, world.ErrFamilyMutationAlreadyMember) ||
		errors.Is(err, world.ErrFamilyMutationAlreadyPending) ||
		errors.Is(err, world.ErrFamilyMutationStaleProposal) ||
		errors.Is(err, world.ErrFamilyMutationInvalidProposal)
}

func familyApplicationErrorResponse(err error, family world.FamilyDefinition) string {
	switch {
	case errors.Is(err, world.ErrFamilyMutationBossUnavailable), errors.Is(err, world.ErrFamilyMutationBossAmbiguous):
		return fmt.Sprintf("패거리의 두목인 %s님이 현재 이용중이 아닙니다.\r\n", family.Boss)
	case errors.Is(err, world.ErrFamilyMutationFamilyUnavailable):
		return "해체된 패거리입니다.\r\n"
	case errors.Is(err, world.ErrFamilyMutationAlreadyMember):
		return "당신은 이미 패거리에 가입이 되어있습니다.\r\n"
	case errors.Is(err, world.ErrFamilyMutationAlreadyPending):
		return "당신은 이미 가입신청을 해두고 있습니다.\r\n"
	default:
		return "아직 구현되지 않은 명령입니다.\r\n"
	}
}

func (c *worldConnection) submitFamilyWithdrawalContinuation(ctx context.Context, draft *composeDraft, line string) (string, error) {
	if draft == nil || draft.kind != composeFamilyWithdrawal {
		return "명령을 처리할 수 없습니다.\r\n", nil
	}
	if strings.TrimSpace(line) != "예" {
		c.clearCompose()
		return session.FamilyWithdrawalCancelResponse, nil
	}
	receipt, err := c.game.owners.ExecuteFamilyMutationLineWithCatalog(
		ctx, c.game.config.Store, c.game.config.WorldID, draft.commandID, c.lease,
		"패거리탈퇴", c.game.config.FamilyCatalog,
	)
	if err != nil {
		if familyWithdrawalDomainError(err) {
			c.clearCompose()
			return familyWithdrawalErrorResponse(err), nil
		}
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", err
		}
		return "탈퇴 신청을 저장하지 못했습니다. 다시 시도해 주세요.\r\n", nil
	}
	var result world.FamilyMutationResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "탈퇴 신청을 저장하지 못했습니다. 다시 시도해 주세요.\r\n", nil
	}
	c.clearCompose()
	return result.Response, nil
}

func familyWithdrawalDomainError(err error) bool {
	return familyApplicationDomainError(err) ||
		errors.Is(err, world.ErrFamilyMutationFeeUnavailable) ||
		errors.Is(err, world.ErrFamilyMutationMemberLedgerMissing) ||
		errors.Is(err, world.ErrFamilyMutationInsufficientGold) ||
		errors.Is(err, world.ErrFamilyMutationGoldOverflow) ||
		errors.Is(err, world.ErrFamilyMutationBossCannotWithdraw) ||
		errors.Is(err, world.ErrFamilyMutationNotPending)
}

func familyWithdrawalErrorResponse(err error) string {
	switch {
	case errors.Is(err, world.ErrFamilyMutationBossCannotWithdraw):
		return "패거리의 두목은 탈퇴를 할수 없습니다.\r\n"
	case errors.Is(err, world.ErrFamilyMutationInsufficientGold):
		return "당신이 가진 돈으로는 패거리탈퇴비를 낼수 없습니다.\r\n패거리를 탈퇴하지 않았습니다."
	case errors.Is(err, world.ErrFamilyMutationNotPending):
		return "당신은 어떤 패거리에도 가입이 되어 있지 않습니다.\r\n"
	case errors.Is(err, world.ErrFamilyMutationFeeUnavailable), errors.Is(err, world.ErrFamilyMutationMemberLedgerMissing):
		return "아직 구현되지 않은 명령입니다.\r\n"
	default:
		return "아직 구현되지 않은 명령입니다.\r\n"
	}
}
