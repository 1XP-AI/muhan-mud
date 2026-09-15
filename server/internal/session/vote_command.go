package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var (
	// ErrUnsupportedVoteLine is returned before a receipt for input outside
	// global.c's exact bare `투표` alias.
	ErrUnsupportedVoteLine = errors.New("line is not an implemented vote command")
	// ErrVoteContinuationRequired documents the source's connection-local
	// y/n and a..g prompts. They are not ordinary command lines and are not
	// sent to ExecuteGame as independent commands.
	ErrVoteContinuationRequired = errors.New("vote continuation is connection-local")
)

// Source-oriented aliases keep the fail-closed boundary easy to discover from
// adapters that use command-function terminology.
var ErrUnsupportedVoteCommandLine = ErrUnsupportedVoteLine

// These prompts are the connection-local portion of command11.c's
// vote_cmnd. The final success text remains owned by world.VoteResult and is
// only returned after a canonical receipt commits.
const (
	VoteAlreadyVotedResponse  = "당신은 이미 투표를 했습니다.\n"
	VoteChangePrompt          = "당신의 선택을 바꾸시겠습니까? (y/n): "
	VoteChoicePrompt          = "당신의 선택은? : "
	VoteCancelResponse        = "중단합니다.\n"
	VoteInvalidChoiceResponse = "잘못된 선택입니다. 중단합니다.\n"
	VoteCommitRetryResponse   = "투표를 저장하지 못했습니다. 다시 시도해 주세요.\r\n"
)

// VotePromptForOption renders one source issue line and the choice prompt.
// Option zero is the issue's question; subsequent one-based indices select
// the server-owned answer lines. No terminal-provided text is rendered here.
func VotePromptForOption(issue world.VoteIssue, option int) (string, error) {
	if err := (world.VoteCatalog{Issue: issue}).Validate(); err != nil {
		return "", err
	}
	var text string
	if option == 0 {
		text = issue.Prompt
	} else if option > 0 && option <= len(issue.Options) {
		text = issue.Options[option-1]
	} else {
		return "", world.ErrVoteInvalidProposal
	}
	return "\n" + text + "\n" + VoteChoicePrompt, nil
}

// VoteChoicePromptFor is a descriptive alias for VotePromptForOption.
func VoteChoicePromptFor(issue world.VoteIssue, option int) (string, error) {
	return VotePromptForOption(issue, option)
}

// VoteCommand is the parser-owned projection of the exact global.c alias.
// The current command has no client-selected issue or ballot identity.
type VoteCommand struct {
	Alias string `json:"alias"`
}

// VoteOptions carries the server-owned immutable issue/options snapshot. A
// missing catalog is a migration boundary, never an empty issue.
type VoteOptions struct {
	Catalog world.VoteCatalog
}

// VoteCommandOptions and VoteIssueOptions are descriptive aliases for
// callers that name the source operation rather than the terminal command.
type VoteCommandOptions = VoteOptions
type VoteIssueOptions = VoteOptions

func validVoteLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseVoteLine admits only the exact bare Korean alias from global.c. Outer
// terminal whitespace is harmless; arguments, newlines, and escape/control
// input remain outside this bounded adapter.
func ParseVoteLine(line string) (VoteCommand, bool) {
	if !validVoteLine(line) || strings.TrimSpace(line) != "투표" {
		return VoteCommand{}, false
	}
	return VoteCommand{Alias: "투표"}, true
}

func IsVoteLine(line string) bool {
	_, ok := ParseVoteLine(line)
	return ok
}

func ParseVoteCommandLine(line string) (VoteCommand, bool) { return ParseVoteLine(line) }
func IsVoteCommandLine(line string) bool                   { return IsVoteLine(line) }

// VoteContinuationKind identifies the two connection-local phases in
// vote_cmnd: case 1 confirms replacement of an existing vote; case 2 records
// one option letter. Neither phase is a durable command by itself.
type VoteContinuationKind string

const (
	VoteConfirmContinuation VoteContinuationKind = "confirm"
	VoteChoiceContinuation  VoteContinuationKind = "choice"
)

type VoteContinuationInput struct {
	Kind   VoteContinuationKind
	Choice byte
}

// ParseVoteConfirmationLine parses the source y/n replacement prompt. It is
// deliberately strict about one ASCII byte; the source's prefix behavior is
// not promoted into the ordinary command parser.
func ParseVoteConfirmationLine(line string) (VoteContinuationInput, bool) {
	if !validVoteLine(line) || len(line) != 1 {
		return VoteContinuationInput{}, false
	}
	switch line[0] {
	case 'y', 'Y':
		return VoteContinuationInput{Kind: VoteConfirmContinuation, Choice: 'Y'}, true
	case 'n', 'N':
		return VoteContinuationInput{Kind: VoteConfirmContinuation, Choice: 'N'}, true
	default:
		return VoteContinuationInput{}, false
	}
}

// ParseVoteChoiceLine parses one case-2 option letter. It accepts the exact
// source range a..g and canonicalizes lowercase input to uppercase. The
// number of prompts is owned by world.VoteContinuation, not by this parser.
func ParseVoteChoiceLine(line string) (VoteContinuationInput, bool) {
	if !validVoteLine(line) || len(line) != 1 {
		return VoteContinuationInput{}, false
	}
	choice := line[0]
	if choice >= 'a' && choice <= 'g' {
		choice -= 'a' - 'A'
	}
	if choice < 'A' || choice > 'G' {
		return VoteContinuationInput{}, false
	}
	return VoteContinuationInput{Kind: VoteChoiceContinuation, Choice: choice}, true
}

// ParseVoteContinuationLine identifies either connection-local continuation
// input. A caller must retain the same connection-local state and issue
// digest; this function never reads or writes State or a receipt.
func ParseVoteContinuationLine(line string) (VoteContinuationInput, bool) {
	if input, ok := ParseVoteConfirmationLine(line); ok {
		return input, true
	}
	return ParseVoteChoiceLine(line)
}

func IsVoteContinuationLine(line string) bool {
	_, ok := ParseVoteContinuationLine(line)
	return ok
}

type voteLineRequest struct {
	Kind          string `json:"kind"`
	Line          string `json:"line"`
	Alias         string `json:"alias"`
	CatalogDigest string `json:"catalog_digest"`
}

// VoteContinuationStart is the server-owned result of the first `투표` line.
// It contains the issue snapshot shown to the connection and the
// connection-local progress value that will later bind the final choices to
// a canonical receipt. No field is sourced from the terminal.
type VoteContinuationStart struct {
	Catalog      world.VoteCatalog
	Projection   world.VoteIssueProjection
	Continuation world.VoteContinuation
}

// VoteStart is a concise alias for callers that name the first phase rather
// than the source's continuation helper.
type VoteStart = VoteContinuationStart

// BeginVoteContinuation performs the source's case-0 read without creating a
// receipt. The loaded snapshot must already contain a complete canonical vote
// aggregate; a nil or incomplete State.Votes is an unresolved migration
// boundary and fails closed before any prompt is exposed.
func (o *Ownership) BeginVoteContinuation(ctx context.Context, store engine.CommandStore, worldID string, lease SessionLease, options VoteOptions) (VoteContinuationStart, error) {
	var start VoteContinuationStart
	err := o.RunGame(lease, func() error {
		if store == nil || worldID == "" {
			return errors.New("invalid vote continuation input")
		}
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			return err
		}
		state, err := world.DecodeState(snapshot.State)
		if err != nil {
			return err
		}
		catalog, err := options.Catalog.Clone()
		if err != nil {
			return err
		}
		projection, err := state.ProjectVoteIssue(lease.ActorID, catalog)
		if err != nil {
			return err
		}
		if !projection.BallotStateResolved {
			return world.ErrVoteStateUnresolved
		}
		continuation, err := world.NewVoteContinuation(projection)
		if err != nil {
			return err
		}
		start = VoteContinuationStart{Catalog: catalog, Projection: projection, Continuation: continuation}
		return nil
	})
	if err != nil {
		return VoteContinuationStart{}, err
	}
	return start, nil
}

// PlanVoteContinuation is the descriptive alias for BeginVoteContinuation.
func (o *Ownership) PlanVoteContinuation(ctx context.Context, store engine.CommandStore, worldID string, lease SessionLease, options VoteOptions) (VoteContinuationStart, error) {
	return o.BeginVoteContinuation(ctx, store, worldID, lease, options)
}

type voteContinuationRequest struct {
	Kind          string `json:"kind"`
	Alias         string `json:"alias"`
	CatalogDigest string `json:"catalog_digest"`
	IssueNumber   int    `json:"issue_number"`
	Choices       []byte `json:"choices"`
}

// ExecuteVoteContinuation commits the source case-3 write/rewrite after the
// connection-local choices are complete. The request contains only the
// server-owned catalog digest and the choices collected by the connection;
// actor, ballot identity, and the issue itself are derived inside the
// reducer. ExecuteGame checks an existing command receipt first, so a retry
// with the same command ID replays without running PlanVoteWithChoices or
// ApplyVote again.
func (o *Ownership) ExecuteVoteContinuation(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, catalog world.VoteCatalog, continuation world.VoteContinuation) (storage.WorldReceipt, error) {
	canonicalCatalog, err := catalog.Clone()
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	digest, err := canonicalCatalog.Digest()
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	if continuation.CatalogDigest == "" || continuation.CatalogDigest != digest || continuation.IssueNumber != canonicalCatalog.Issue.Number || continuation.NextOption != continuation.IssueNumber+1 || len(continuation.Choices) != continuation.IssueNumber {
		return storage.WorldReceipt{}, world.ErrVoteInvalidProposal
	}
	for _, choice := range continuation.Choices {
		if (choice < 'A' || choice > 'G') && (choice < 'a' || choice > 'g') {
			return storage.WorldReceipt{}, world.ErrVoteInvalidProposal
		}
	}
	payload, err := json.Marshal(voteContinuationRequest{
		Kind:          "vote-continuation",
		Alias:         "투표",
		CatalogDigest: digest,
		IssueNumber:   canonicalCatalog.Issue.Number,
		Choices:       append([]byte(nil), continuation.Choices...),
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanVoteWithChoices(actorID, canonicalCatalog, continuation.Choices)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyVote(proposal)
		if err != nil {
			return nil, nil, err
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		return nextRaw, response, err
	})
}

// ExecuteVoteChoices is the source/schema spelling for the final vote
// continuation receipt.
func (o *Ownership) ExecuteVoteChoices(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, catalog world.VoteCatalog, continuation world.VoteContinuation) (storage.WorldReceipt, error) {
	return o.ExecuteVoteContinuation(ctx, store, worldID, commandID, lease, catalog, continuation)
}

// ExecuteVoteLine accepts at most one server-owned catalog for the
// receipt-backed case-0 projection. Interactive terminal callers should use
// BeginVoteContinuation followed by ExecuteVoteContinuation; the latter is
// the case-3 write/rewrite boundary. A replaying store may still return an
// existing receipt before this reducer is entered, as required by
// ExecuteGame's durable identity contract.
func (o *Ownership) ExecuteVoteLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.VoteCatalog) (storage.WorldReceipt, error) {
	if len(catalogs) > 1 {
		return storage.WorldReceipt{}, errors.New("multiple vote issue catalogs")
	}
	var catalog world.VoteCatalog
	if len(catalogs) == 1 {
		catalog = catalogs[0]
	}
	return o.ExecuteVoteLineWithOptions(ctx, store, worldID, commandID, lease, line, VoteOptions{Catalog: catalog})
}

// ExecuteVoteLineWithOptions is the explicit dependency-visible form used
// by callers wiring the versioned issue catalog.
func (o *Ownership) ExecuteVoteLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options VoteOptions) (storage.WorldReceipt, error) {
	command, ok := ParseVoteLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedVoteLine
	}
	digest, err := options.Catalog.Digest()
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	payload, err := json.Marshal(voteLineRequest{Kind: "vote", Line: line, Alias: command.Alias, CatalogDigest: digest})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanVote(actorID, options.Catalog)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyVote(proposal)
		if err != nil {
			return nil, nil, err
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if result.Operation == world.VoteOperationRead {
			// Keep a read-only case-0 receipt from rewriting unrelated JSON
			// fields or map ordering in the loaded snapshot.
			return raw, response, nil
		}
		return nextRaw, response, err
	})
}

// ExecuteVoteLineWithCatalog is the explicit catalog spelling used by
// callers that prefer the dependency name in the method itself.
func (o *Ownership) ExecuteVoteLineWithCatalog(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.VoteCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteVoteLineWithOptions(ctx, store, worldID, commandID, lease, line, VoteOptions{Catalog: catalog})
}

func (o *Ownership) ExecuteVote(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.VoteCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteVoteLine(ctx, store, worldID, commandID, lease, line, catalogs...)
}

// ExecuteVoteCommandLine is the source-function spelling alias.
func (o *Ownership) ExecuteVoteCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalogs ...world.VoteCatalog) (storage.WorldReceipt, error) {
	return o.ExecuteVoteLine(ctx, store, worldID, commandID, lease, line, catalogs...)
}

func (o *Ownership) ExecuteVoteCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options VoteCommandOptions) (storage.WorldReceipt, error) {
	return o.ExecuteVoteLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
