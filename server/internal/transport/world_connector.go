package transport

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type WorldConnectorConfig struct {
	Store     engine.CommandStore
	WorldID   string
	Clock     func() (int32, int)
	WallClock func() time.Time
	Catalog   world.SpawnCatalog
	// FamilyCatalog is the immutable server-owned family directory used by
	// read-only 패거리 status commands. A missing catalog intentionally makes
	// those commands fail closed rather than guessing names from numeric IDs.
	FamilyCatalog world.FamilyCatalog
	// PasswordStore is the account-only credential boundary for the
	// connection-local `암호` flow. It is intentionally optional so tests and
	// non-account world adapters can keep the command fail-closed without
	// changing the world receipt contract.
	PasswordStore session.PasswordChangeStore
	// Accounts is the optional credential check for suicide case 2. When
	// absent, ExecuteSuicidePasswordLine falls back to PasswordStore.
	Accounts session.Accounts
	// TalkCatalog is the immutable, server-owned command8.c topic catalog.
	// It is optional so no-topic NPC speech keeps its original behavior; an
	// MTALKS topic request fails closed when this dependency is absent.
	TalkCatalog *world.TalkCatalog
	// MerchantOffers is the server-owned MPURIT stock catalog. Legacy NPC
	// Carry values are never interpreted from a terminal request; an absent
	// catalog makes merchant purchase fail closed at the session boundary.
	MerchantOffers world.MerchantOffers
	// VoteCatalog is the server-owned snapshot of the current ISSUE resource.
	// A zero catalog keeps `투표` fail-closed until per-player ballot state is
	// migrated; terminal clients cannot supply or replace this dependency.
	VoteCatalog world.VoteCatalog
	Roll        func(int, int) int
	Allocate    func() (string, error)
	HelpFS      fs.FS
	MaxSessions int
}

// WorldConnector implements the real world connection boundary. The host must
// fence/recover its world before serving, run RunCleanup for socket-independent
// retries, and drain PendingCleanup before shutdown. Bare look and direct
// movement are dispatched; this is not a complete game command loop.
type WorldConnector struct {
	mu                     sync.Mutex
	commandMu              sync.Mutex
	tickMu                 sync.Mutex
	config                 WorldConnectorConfig
	owners                 session.Ownership
	cleanup                *session.CleanupQueue
	connections            map[*worldConnection]struct{}
	stopping               bool
	lastPublicAdmissionAt  int32
	lastVitalSlot          int64
	pendingVital           *playerVitalTick
	lastRoomResourceSlot   int64
	pendingRoomResource    *roomResourceTick
	lastNPCResourceSlot    int64
	pendingNPCResource     *npcResourceTick
	lastNPCMaintenanceSlot int64
	pendingNPCMaintenance  *npcMaintenanceTick
	lastNPCRandomSpawnSlot int64
	pendingNPCRandomSpawn  *npcRandomSpawnTick
}

type playerPhaseSummary struct {
	Now                int32                  `json:"now"`
	Hour               int                    `json:"hour"`
	Actors             []string               `json:"actors"`
	Messages           []string               `json:"messages"`
	Deaths             int                    `json:"deaths"`
	SaveDue            []string               `json:"save_due"`
	Extinguished       []string               `json:"extinguished"`
	FamilyDefeatEvents []world.FamilyWarEvent `json:"family_defeat_events,omitempty"`
}

func NewWorldConnector(config WorldConnectorConfig) (*WorldConnector, error) {
	if config.Store == nil || config.WorldID == "" || config.Clock == nil || config.MaxSessions < 1 {
		return nil, errors.New("invalid world connector configuration")
	}
	// Treat the injected catalog as immutable configuration. Copy the value at
	// connector construction so replacing the caller's pointer cannot change a
	// live connector's command dependency. TalkCatalog's public lookups return
	// owned copies of topic slices and expose no mutation path for its map.
	if config.TalkCatalog != nil {
		catalog := *config.TalkCatalog
		config.TalkCatalog = &catalog
	}
	if config.FamilyCatalog.Families != nil {
		families := make(map[int16]world.FamilyDefinition, len(config.FamilyCatalog.Families))
		for id, family := range config.FamilyCatalog.Families {
			families[id] = family
		}
		config.FamilyCatalog.Families = families
	}
	if config.VoteCatalog.Issue.Number != 0 || config.VoteCatalog.Issue.Prompt != "" || len(config.VoteCatalog.Issue.Options) != 0 {
		catalog, err := config.VoteCatalog.Clone()
		if err != nil {
			return nil, fmt.Errorf("invalid vote catalog: %w", err)
		}
		config.VoteCatalog = catalog
	}
	if config.WallClock == nil {
		config.WallClock = func() time.Time { return time.Now().In(mudPST) }
	}
	g := &WorldConnector{config: config, connections: map[*worldConnection]struct{}{}, lastVitalSlot: -1, lastRoomResourceSlot: -1, lastNPCResourceSlot: -1, lastNPCMaintenanceSlot: -1, lastNPCRandomSpawnSlot: -1}
	g.cleanup = session.NewWorldCleanupQueue(&g.owners, config.Store, config.WorldID)
	return g, nil
}

var mudPST = time.FixedZone("PST", -8*60*60)

func (g *WorldConnector) PendingCleanup() []session.PendingCleanup { return g.cleanup.Pending() }
func (g *WorldConnector) RunCleanup(ctx context.Context) error {
	return g.cleanup.Run(ctx, time.Second, 5*time.Second)
}

// RunPlayerVitalPhase applies the durable player update_ply subset once. The
// caller owns the command ID and must reuse it when the commit outcome is
// uncertain. This is deliberately not a full legacy scheduler: NPC AI, room
// spawn/tick phases, combat rounds and exact broadcast formatting remain
// separate slices.
func (g *WorldConnector) RunPlayerVitalPhase(ctx context.Context, commandID string) (storage.WorldReceipt, error) {
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing player phase command ID")
	}
	now, hour := g.config.Clock()
	return g.runPlayerVitalPhaseAt(ctx, commandID, now, hour)
}

func (g *WorldConnector) runPlayerVitalPhaseAt(ctx context.Context, commandID string, now int32, hour int) (storage.WorldReceipt, error) {
	request, err := json.Marshal(struct {
		Kind string `json:"kind"`
		Now  int32  `json:"now"`
		Hour int    `json:"hour"`
	}{Kind: "player-vital-phase", Now: now, Hour: hour})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	g.commandMu.Lock()
	receipt, err := engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		order, err := s.PlayerUpdateOrder(hour)
		if err != nil {
			return nil, nil, err
		}
		next, results, err := s.UpdatePlayers(order, now, g.config.Catalog, g.config.Roll, g.config.Allocate)
		if err != nil {
			return nil, nil, err
		}
		summary := playerPhaseSummary{Now: now, Hour: hour}
		for _, result := range results {
			summary.Actors = append(summary.Actors, result.ActorID)
			if result.Update.SaveDue {
				summary.SaveDue = append(summary.SaveDue, result.ActorID)
			}
			if result.Update.ExtinguishedID != "" {
				summary.Extinguished = append(summary.Extinguished, result.Update.ExtinguishedID)
			}
			summary.Messages = append(summary.Messages, result.Update.Vitals.Messages...)
			summary.Deaths += len(result.Update.Vitals.Deaths)
			events, bindErr := familyDefeatEventsFromVitals(next, result.ActorID, result.Update.Vitals, g.config.FamilyCatalog)
			if bindErr != nil {
				return nil, nil, bindErr
			}
			summary.FamilyDefeatEvents = append(summary.FamilyDefeatEvents, events...)
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(summary)
		return state, response, err
	})
	g.commandMu.Unlock()
	if err != nil {
		return receipt, err
	}
	if !receipt.Replayed {
		var summary playerPhaseSummary
		if decodeErr := json.Unmarshal(receipt.Response, &summary); decodeErr == nil && len(summary.FamilyDefeatEvents) != 0 {
			if after, ok := g.snapshot(ctx); ok {
				g.publishFamilyDefeat(after, summary.FamilyDefeatEvents)
			}
		}
	}
	return receipt, nil
}

func familyDefeatEventsFromVitals(after world.State, actorID string, vitals world.VitalsTransitionResult, catalog world.FamilyCatalog) ([]world.FamilyWarEvent, error) {
	var events []world.FamilyWarEvent
	for _, death := range vitals.Deaths {
		if !death.Result.FamilyDefeated {
			continue
		}
		if len(death.Result.Events) != 0 {
			events = append(events, death.Result.Events...)
			continue
		}
		player, ok := after.Players[actorID]
		if !ok {
			return nil, fmt.Errorf("family-defeat vitals actor absent")
		}
		bound, err := world.FamilyDefeatBroadcasts(catalog, player.Body.Daily[world.FamilyDailySlot].Max)
		if err != nil {
			return nil, err
		}
		events = append(events, bound...)
	}
	return events, nil
}

// Shutdown prevents new leases before snapshotting existing ones. It can be
// retried after a deadline; unresolved work remains sealed in PendingCleanup.
// The host separately closes network listeners/sockets and cancels RunCleanup.
func (g *WorldConnector) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	g.stopping = true
	g.mu.Unlock()
	for _, lease := range g.owners.Ordered() {
		if err := g.cleanup.Enqueue(lease); err != nil && g.owners.Owns(lease) {
			return err
		}
	}
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(last, err)
		}
		if len(g.cleanup.Pending()) == 0 {
			return nil
		}
		last = g.cleanup.Retry(ctx, 5*time.Second)
		if len(g.cleanup.Pending()) == 0 {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(last, ctx.Err())
		case <-timer.C:
		}
	}
}
func (g *WorldConnector) Open(ctx context.Context, c storage.Character) (GameConnection, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	g.mu.Lock()
	if g.stopping {
		g.mu.Unlock()
		return nil, "", errors.New("world shutting down")
	}
	if len(g.owners.Ordered()) >= g.config.MaxSessions {
		g.mu.Unlock()
		return nil, "", errors.New("world sessions full")
	}
	lease, err := g.owners.Acquire(c.ID)
	g.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	connection := &worldConnection{game: g, lease: lease, events: make(chan string, 32), accountName: c.Name}
	now, hour := g.config.Clock()
	receipt, err := g.owners.EnterWorld(ctx, g.config.Store, g.config.WorldID, "enter-"+rand.Text(), lease, now, world.SceneOptions{ViewOptions: world.ViewOptions{Hour: hour}}, g.config.Catalog, g.config.Roll, g.config.Allocate)
	if err != nil {
		return connection, "", err
	} // handler must Close even on Open error
	var entry world.RoomEntry
	if err = json.Unmarshal(receipt.Response, &entry); err != nil {
		return connection, "", err
	}
	// New characters are registered before a canonical account name is
	// attached to storage.Character. Resolve that name from the admitted
	// player once, without making it part of world state or a command receipt.
	if connection.accountName == "" {
		if admitted, ok := g.snapshot(ctx); ok {
			if player, exists := admitted.Players[lease.ActorID]; exists {
				connection.accountName = player.Body.Name
			}
		}
	}
	connection.ready = true
	g.mu.Lock()
	// command4.c resets the global public-broadcast cooldown when a visible
	// non-DM player enters. Character drafts do not carry the runtime PDMINV
	// bit, so class is the durable admission boundary available here; an
	// operator/DM admission never delays ordinary players.
	if c.Draft.Class < 12 {
		g.lastPublicAdmissionAt = now
	}
	g.connections[connection] = struct{}{}
	g.mu.Unlock()
	return connection, entry.Scene, nil
}

type worldConnection struct {
	mu     sync.Mutex
	game   *WorldConnector
	lease  session.SessionLease
	events chan string
	// accountName is the canonical account identity used only by the
	// connection-local password flow. It is never copied into world receipts.
	accountName string
	// lastCommand mirrors C's connection-local extr->lastcommand. It is
	// deliberately excluded from world state and durable receipts: `!` only
	// expands the next line before the normal command reducer runs.
	lastCommand string
	// replyTarget is the exact incoming direct-message sender used by the
	// source `대답`/`/` command. It is connection-local and atomic because a
	// committed event updates another connection while the recipient may be
	// submitting a line concurrently.
	replyTarget      atomic.Pointer[replyTarget]
	lastBroadcastAt  int32
	ready, closed    bool
	closeAfterSubmit bool
	infoPending      bool
	// infoContinuationCommandID binds the one [엔터] page to a durable
	// receipt. It remains stable across an uncertain commit/response so a
	// retry cannot render a newer snapshot or create a second page.
	infoContinuationCommandID string
	// forgeSelectArmPending owns select_arm case 2 after a successful 제련
	// start. The next player line is not parsed as an ordinary command; it
	// is ExecuteForgeSelectArmLine. The command ID stays stable across an
	// uncertain commit so a retry cannot load 900-904 twice.
	forgeSelectArmPending   bool
	forgeSelectArmCommandID string
	// forgeMaterialPending owns select_arm case 3 after a successful weapon
	// type. The next player line is ExecuteForgeSelectMaterialLine. ObjectID
	// is the C forge1 template (900-904) loaded in case 2.
	forgeMaterialPending   bool
	forgeMaterialCommandID string
	forgeObjectID          int16
	// forgeQuenchPending owns select_arm case 4 after a successful material.
	// The next player line is ExecuteForgeSelectQuenchLine. forgeSum is the
	// C forge2 running cost from case 3 (material only) until case 6.
	forgeQuenchPending   bool
	forgeQuenchCommandID string
	forgeSum             int32
	forgeQuenchChoice    int
	// forgeNamePending owns select_arm case 5 after a successful quench.
	// The next player line is ExecuteForgeSelectNameLine.
	forgeNamePending   bool
	forgeNameCommandID string
	// forgeConfirmPending owns select_arm case 6 after a successful name.
	// The next player line is ExecuteForgeSelectConfirmLine. forgeWeaponName
	// is the C forge1 name copied in case 5.
	forgeConfirmPending   bool
	forgeConfirmCommandID string
	forgeWeaponName       string
	// newForgeSelectArmPending owns select_newarm case 2 after a successful
	// 무기만들기 start. The next player line is ExecuteNewForgeSelectArmLine,
	// not ParseCommand. This flag is the C RETURN marker so the next line
	// cannot become 제련's select_arm. The command ID stays stable across
	// an uncertain commit.
	newForgeSelectArmPending   bool
	newForgeSelectArmCommandID string
	// newForgeMaterialPending owns select_newarm case 3 after a successful
	// weapon type. The next player line is ExecuteNewForgeSelectMaterialLine.
	// ObjectID is the C forge1 template (900-904) loaded in case 2; Sum is
	// the C forge2 cost written in case 3 until case 6.
	newForgeMaterialPending   bool
	newForgeMaterialCommandID string
	newForgeObjectID          int16
	newForgeSum               int32
	// newForgeQuenchPending owns select_newarm case 4 after a successful
	// material. The next player line is ExecuteNewForgeSelectQuenchLine.
	newForgeQuenchPending   bool
	newForgeQuenchCommandID string
	newForgeQuenchChoice    int
	// newForgeNamePending owns select_newarm case 5 after a successful
	// quench. The next player line is ExecuteNewForgeSelectNameLine.
	newForgeNamePending   bool
	newForgeNameCommandID string
	// newForgeConfirmPending owns select_newarm case 6 after a successful
	// name. The next player line is ExecuteNewForgeSelectConfirmLine, not
	// ParseCommand. newForgeWeaponName is the C forge1 name copied in case 5.
	newForgeConfirmPending   bool
	newForgeConfirmCommandID string
	newForgeWeaponName       string
	// suicidePasswordPending owns suicide case 2 after 목매달기. The next
	// player line is ExecuteSuicidePasswordLine, not ParseCommand. The
	// command ID stays stable across an uncertain commit so a retry cannot
	// re-prompt.
	suicidePasswordPending   bool
	suicidePasswordCommandID string
	// vote is command11.c's connection-local vote_cmnd state. It is never
	// serialized into State or a receipt; the final choices are bound to one
	// canonical receipt only after the connection has completed the prompts.
	vote           *voteDraft
	compose        *composeDraft
	notepad        *notepadDraft
	passwordChange session.PasswordChanger
	passwordSecret bool
	// ignore is command9.c's connection-local first_ignore list. It is never
	// serialized with the world snapshot or a command receipt.
	ignore IgnoreList
}

type votePhase uint8

const (
	voteConfirmPhase votePhase = iota + 1
	voteChoicePhase
	voteCommitPhase
)

type voteDraft struct {
	commandID    string
	catalog      world.VoteCatalog
	continuation world.VoteContinuation
	phase        votePhase
	lastChoice   byte
}

type replyTarget struct {
	id   string
	name string
}

func (c *worldConnection) setReplyTarget(id, name string) {
	if id == "" || name == "" {
		c.replyTarget.Store(nil)
		return
	}
	c.replyTarget.Store(&replyTarget{id: id, name: name})
}

func (c *worldConnection) getReplyTarget() (string, string, bool) {
	target := c.replyTarget.Load()
	if target == nil || target.id == "" || target.name == "" {
		return "", "", false
	}
	return target.id, target.name, true
}

type composeKind uint8

const (
	composeMailSend composeKind = iota + 1
	composeBoardWrite
	composeFamilyApplication
	composeFamilyWithdrawal
	composeFamilyNewsAppend
	composeChangeClass
)

type composeDraft struct {
	kind        composeKind
	commandID   string
	recipientID string
	boardID     int
	title       string
	body        []string
	timestamp   time.Time
	messageID   string
	// familyName/familyID are connection-local selection data for the bare
	// `패거리가입` flow. They cross the receipt boundary only after the user
	// confirms the exact catalog entry.
	familyName string
}

// notepadDraft is the connection-local post.c noteedit buffer. It remains
// outside State and receipts until the line beginning with `.` submits the
// complete buffer under one stable command ID.
type notepadDraft struct {
	commandID     string
	verb          string
	lines         []string
	commitPending bool
}

// Events is an optional asynchronous room-output stream. The WebSocket
// transport consumes it with one writer lock, while non-WebSocket test
// connectors can continue to implement only Submit/Close.
func (c *worldConnection) Events() <-chan string { return c.events }

// ShouldClose is observed by the WebSocket loop after the quit receipt has
// been sent. Close itself still performs the durable departure/cleanup.
func (c *worldConnection) ShouldClose() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeAfterSubmit
}

func (c *worldConnection) clearCompose() {
	if c.compose == nil {
		return
	}
	// The editor is connection-local and must not survive disconnect or a
	// completed receipt. Strings cannot be scrubbed in place in Go; dropping
	// every reference is the safest lifetime boundary available here.
	c.compose.body = nil
	c.compose.title = ""
	c.compose.recipientID = ""
	c.compose.messageID = ""
	c.compose.familyName = ""
	c.compose.commandID = ""
	c.compose.boardID = 0
	c.compose.timestamp = time.Time{}
	c.compose.kind = 0
	c.compose = nil
}

func (c *worldConnection) clearNotepad() {
	if c.notepad == nil {
		return
	}
	c.notepad.lines = nil
	c.notepad.verb = ""
	c.notepad.commandID = ""
	c.notepad = nil
}

// submitNotepadLine consumes the bounded post.c editor before history,
// aliases or the ordinary parser. Only the terminating `.` reaches the
// durable command boundary; all previous lines remain connection-local.
func (c *worldConnection) submitNotepadLine(ctx context.Context, line string) (string, bool, error) {
	if c.notepad != nil {
		return c.submitNotepadContinuation(ctx, line)
	}
	command, ok := session.ParseNotepadLine(line)
	if !ok || command.Action != world.NotepadAppend {
		return "", false, nil
	}
	state, ok := c.game.snapshot(ctx)
	if !ok {
		return "아직 구현되지 않은 명령입니다.\r\n", true, nil
	}
	proposal, err := state.PlanNotepad(c.lease.ActorID, command.Verb, command.Option)
	if err != nil {
		return "아직 구현되지 않은 명령입니다.\r\n", true, nil
	}
	if proposal.Action != world.NotepadAppend {
		// An unauthorized canonical actor follows post.c's ordinary unknown
		// command response without entering a continuation.
		return proposal.Response, true, nil
	}
	c.notepad = &notepadDraft{
		commandID: "notepad-append-" + rand.Text(),
		verb:      command.Verb,
	}
	c.lastCommand = strings.TrimLeft(line, " ")
	return proposal.Response, true, nil
}

func (c *worldConnection) submitNotepadContinuation(ctx context.Context, line string) (string, bool, error) {
	draft := c.notepad
	if draft == nil {
		return "", false, nil
	}
	if draft.commitPending && line != "." {
		return session.NotepadAppendRetryResponse, true, nil
	}
	if err := world.ValidateNotepadLine(line); err != nil {
		return session.NotepadAppendInvalidLineResponse, true, nil
	}
	if strings.HasPrefix(line, ".") {
		wasPending := draft.commitPending
		if !wasPending {
			// The first dot must admit against the latest canonical state before
			// entering the durable command boundary. A later dot is a retry of
			// the same command and must reach Execute first so its receipt can be
			// replayed even when the committed append now fills the limits.
			if fits, checked := c.notepadDraftFitsCurrentLimits(ctx, draft.lines); checked && !fits {
				c.clearNotepad()
				return "아직 구현되지 않은 명령입니다.\r\n", true, nil
			}
		}
		draft.commitPending = true
		receipt, err := c.game.owners.ExecuteNotepadAppendWithVerb(
			ctx, c.game.config.Store, c.game.config.WorldID, draft.commandID, c.lease, draft.verb, draft.lines,
		)
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
				return "", true, err
			}
			if errors.Is(err, world.ErrNotepadLimit) {
				if !wasPending {
					// The reducer saw a fresh canonical limit rejection before a
					// receipt or mutation existed. It cannot become retryable state;
					// clear the local editor so the connection is not wedged behind
					// an impossible append. A pending retry keeps its stable command
					// and buffered input because the receipt may be temporarily absent.
					c.clearNotepad()
					return "아직 구현되지 않은 명령입니다.\r\n", true, nil
				}
				return session.NotepadAppendRetryResponse, true, nil
			}
			return session.NotepadAppendRetryResponse, true, nil
		}
		var result world.NotepadResult
		if err := json.Unmarshal(receipt.Response, &result); err != nil || result.Response == "" ||
			(result.Action != world.NotepadAppend && result.Action != world.NotepadUnknown) {
			return session.NotepadAppendRetryResponse, true, nil
		}
		c.clearNotepad()
		return result.Response, true, nil
	}
	canonical := world.TruncateNotepadLine(line)
	pending := make([]string, 0, len(draft.lines)+1)
	pending = append(pending, draft.lines...)
	pending = append(pending, canonical)
	if fits, checked := c.notepadDraftFitsCurrentLimits(ctx, pending); !checked {
		// A body line is not admitted without a current canonical snapshot. It
		// remains retryable as local input and has not entered commitPending.
		return session.NotepadAppendRetryResponse, true, nil
	} else if !fits {
		c.clearNotepad()
		return "아직 구현되지 않은 명령입니다.\r\n", true, nil
	}
	draft.lines = append(draft.lines, canonical)
	return world.NotepadAppendContinuePrompt, true, nil
}

// notepadDraftFitsCurrentLimits checks the complete append projection against
// the latest durable notepad. The draft remains connection-local; this helper
// only loads canonical state and accounts for the header that PlanNotepad
// inserts when an empty file receives its first body line.
func (c *worldConnection) notepadDraftFitsCurrentLimits(ctx context.Context, lines []string) (bool, bool) {
	state, ok := c.game.snapshot(ctx)
	if !ok || state.Notepad == nil {
		return false, false
	}
	return notepadDraftFitsState(state, lines), true
}

func notepadDraftFitsState(state world.State, lines []string) bool {
	if state.Notepad == nil {
		return false
	}
	if len(lines) == 0 {
		return true
	}

	totalLines := len(state.Notepad)
	totalBytes := notepadDraftBytes(state.Notepad)
	if totalLines == 0 {
		totalLines = 2 // NotepadHeaderLine plus the blank separator line.
		totalBytes = len(world.NotepadHeaderLine) + 2
	}
	totalLines += len(lines)
	totalBytes += notepadDraftBytes(lines)
	return totalLines <= world.MaxNotepadLines && totalBytes <= world.MaxNotepadBytes
}

func notepadDraftBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line) + 1
	}
	return total
}

// submitComposeLine consumes the connection-local editor before history,
// aliases or the ordinary parser. Only the final line calls ExecuteGame;
// title/body prompts and cancellations are deliberately receipt-free.
func (c *worldConnection) submitComposeLine(ctx context.Context, line string) (string, bool, error) {
	if c.compose != nil {
		output, err := c.submitComposeContinuation(ctx, line)
		return output, true, err
	}
	if command, ok := session.ParseMailSendLine(line); ok {
		state, ok := c.game.snapshot(ctx)
		if !ok {
			return "명령을 처리할 수 없습니다.\r\n", true, nil
		}
		actor, actorOK := state.Players[c.lease.ActorID]
		room, roomOK := state.Rooms[actor.Body.RoomID]
		if !actorOK || !actor.Online || actor.Body.Type != 0 || !roomOK || !roomFlagSet(room.Resource.Flags, world.RoomPostOfficeFlag) {
			return world.MailRoomResponse, true, nil
		}
		if command.Recipient == "" {
			c.lastCommand = strings.TrimLeft(line, " ")
			return "누구한테 편지를 보내시려구요?\n", true, nil
		}
		recipientID, err := state.ResolveMailRecipientID(command.Recipient)
		if err != nil {
			return "그런 사용자는 없습니다.\n", true, nil
		}
		c.compose = &composeDraft{kind: composeMailSend, commandID: "mail-send-" + rand.Text(), recipientID: recipientID}
		c.lastCommand = strings.TrimLeft(line, " ")
		return session.MailSendPrompt, true, nil
	}
	if _, ok := session.ParseBoardWriteLine(line); ok {
		state, ok := c.game.snapshot(ctx)
		if !ok {
			return "명령을 처리할 수 없습니다.\r\n", true, nil
		}
		_, _, boardID, err := state.BoardContext(c.lease.ActorID)
		if err != nil {
			return "이곳에는 게시판이 없습니다.\r\n", true, nil
		}
		c.compose = &composeDraft{kind: composeBoardWrite, commandID: "board-write-" + rand.Text(), boardID: boardID}
		c.lastCommand = strings.TrimLeft(line, " ")
		return session.BoardWriteTitlePrompt, true, nil
	}
	if session.ParseFamilyMutationStartLine(line) {
		state, ok := c.game.snapshot(ctx)
		if !ok {
			return "명령을 처리할 수 없습니다.\r\n", true, nil
		}
		actor, exists := state.Players[c.lease.ActorID]
		if !exists || !actor.Online || actor.Body.Type != 0 {
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		room, roomExists := state.Rooms[actor.Body.RoomID]
		member := false
		if roomExists {
			for _, id := range room.PlayerIDs {
				if id == c.lease.ActorID {
					member = true
					break
				}
			}
		}
		if !roomExists || !member {
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		if world.PlayerFlagSet(actor.Body, world.FamilyMemberFlag) {
			return "당신은 이미 패거리에 가입이 되어있습니다.\r\n", true, nil
		}
		if world.PlayerFlagSet(actor.Body, world.FamilyPendingFlag) {
			return "당신은 이미 가입신청을 해두고 있습니다.\r\n", true, nil
		}
		if err := c.game.config.FamilyCatalog.Validate(); err != nil {
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		list, err := state.ListFamily(c.game.config.FamilyCatalog)
		if err != nil {
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		c.compose = &composeDraft{kind: composeFamilyApplication, commandID: "family-join-" + rand.Text()}
		c.lastCommand = strings.TrimLeft(line, " ")
		return list + session.FamilyApplicationSelectionPrompt, true, nil
	}
	if _, ok := session.ParseFamilyWithdrawalLine(line); ok {
		state, ok := c.game.snapshot(ctx)
		if !ok {
			return "명령을 처리할 수 없습니다.\r\n", true, nil
		}
		actor, exists := state.Players[c.lease.ActorID]
		if !exists || !actor.Online || actor.Body.Type != 0 {
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		// Pending applications retain the already-admitted one-step cancel
		// receipt. Only an active member enters the original confirmation flow.
		if !world.PlayerFlagSet(actor.Body, world.FamilyMemberFlag) {
			return "", false, nil
		}
		if world.PlayerFlagSet(actor.Body, world.FamilyBossFlag) {
			return "패거리의 두목은 탈퇴를 할수 없습니다.\r\n", true, nil
		}
		if err := c.game.config.FamilyCatalog.Validate(); err != nil {
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		if _, err := state.PlanFamilyWithdrawal(c.lease.ActorID, c.game.config.FamilyCatalog); err != nil {
			return familyWithdrawalErrorResponse(err), true, nil
		}
		c.compose = &composeDraft{kind: composeFamilyWithdrawal, commandID: "family-withdraw-" + rand.Text()}
		c.lastCommand = strings.TrimLeft(line, " ")
		return session.FamilyWithdrawalConfirmPrompt, true, nil
	}
	if session.ParseFamilyNewsAppendStartLine(line) {
		state, ok := c.game.snapshot(ctx)
		if !ok {
			return "명령을 처리할 수 없습니다.\r\n", true, nil
		}
		result, err := state.PlanFamilyNewsView(c.lease.ActorID, c.game.config.FamilyCatalog)
		if err != nil {
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		if result.Response == world.FamilyNewsNotMemberResponse {
			return result.Response, true, nil
		}
		c.compose = &composeDraft{kind: composeFamilyNewsAppend}
		c.lastCommand = strings.TrimLeft(line, " ")
		return world.FamilyNewsAppendPrompt, true, nil
	}
	if session.ParseChangeClassStartLine(line) {
		state, ok := c.game.snapshot(ctx)
		if !ok {
			return "명령을 처리할 수 없습니다.\r\n", true, nil
		}
		proposal, err := state.PlanChangeClass(c.lease.ActorID, false)
		if err != nil {
			// The bare form is a local prompt only after the same source gates
			// have passed. No durable receipt is created for a failed gate.
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		c.compose = &composeDraft{kind: composeChangeClass, commandID: "change-class-" + rand.Text()}
		c.lastCommand = strings.TrimLeft(line, " ")
		return proposal.Response, true, nil
	}
	return "", false, nil
}

func roomFlagSet(flags [8]byte, bit int) bool {
	return bit >= 0 && bit/8 < len(flags) && flags[bit/8]&(1<<uint(bit%8)) != 0
}

func (c *worldConnection) submitComposeContinuation(ctx context.Context, line string) (string, error) {
	draft := c.compose
	if draft == nil {
		return "", nil
	}
	switch draft.kind {
	case composeMailSend:
		return c.submitMailSendContinuation(ctx, draft, line)
	case composeBoardWrite:
		return c.submitBoardWriteContinuation(ctx, draft, line)
	case composeFamilyApplication:
		return c.submitFamilyApplicationContinuation(ctx, draft, line)
	case composeFamilyWithdrawal:
		return c.submitFamilyWithdrawalContinuation(ctx, draft, line)
	case composeFamilyNewsAppend:
		return c.submitFamilyNewsAppendContinuation(ctx, draft, line)
	case composeChangeClass:
		return c.submitChangeClassContinuation(ctx, draft, line)
	default:
		c.clearCompose()
		return "명령을 처리할 수 없습니다.\r\n", nil
	}
}

func (c *worldConnection) submitMailSendContinuation(ctx context.Context, draft *composeDraft, line string) (string, error) {
	if strings.HasPrefix(line, ".") {
		body := strings.Join(draft.body, "\n")
		if draft.timestamp.IsZero() {
			now, _ := c.game.config.Clock()
			draft.timestamp = time.Unix(int64(now), 0).UTC()
		}
		if draft.messageID == "" && c.game.config.Allocate != nil {
			messageID, err := c.game.config.Allocate()
			if err != nil {
				return "편지를 보내지 못했습니다. 다시 시도해 주세요.\n", nil
			}
			draft.messageID = messageID
		}
		receipt, err := c.game.owners.ExecuteMailSendWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, draft.commandID, c.lease, world.MailSendPayload{
			RecipientID: draft.recipientID,
			Body:        body,
			Timestamp:   draft.timestamp,
		}, session.MailSendOptions{MessageID: draft.messageID})
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
				return "", err
			}
			return "편지를 보내지 못했습니다. 다시 시도해 주세요.\n", nil
		}
		var result world.MailSendResult
		if err := json.Unmarshal(receipt.Response, &result); err != nil {
			return "편지를 보내지 못했습니다. 다시 시도해 주세요.\n", nil
		}
		c.clearCompose()
		return result.Response, nil
	}
	candidate := append(append([]string(nil), draft.body...), line)
	if err := world.ValidateMailSendBody(strings.Join(candidate, "\n")); err != nil {
		return session.MailSendInvalidLineResponse, nil
	}
	draft.body = candidate
	return session.MailSendContinuePrompt, nil
}

func (c *worldConnection) submitBoardWriteContinuation(ctx context.Context, draft *composeDraft, line string) (string, error) {
	if draft.title == "" {
		if line == "" {
			c.clearCompose()
			return session.BoardWriteCancelResponse, nil
		}
		if err := world.ValidateBoardTitle(line); err != nil {
			return session.BoardWriteInvalidTitleResponse, nil
		}
		draft.title = line
		return session.BoardWriteBodyPrompt + fmt.Sprintf("%3d: ", len(draft.body)+1), nil
	}
	if strings.HasPrefix(line, "!!") {
		c.clearCompose()
		return session.BoardWriteCancelResponse, nil
	}
	if strings.HasPrefix(line, ".") {
		payload, err := world.BoardWritePayloadFromLines(draft.boardID, draft.title, draft.body)
		if err != nil {
			return "게시물을 등록할 수 없습니다. 본문을 입력해 주세요.\r\n", nil
		}
		if draft.timestamp.IsZero() {
			now, _ := c.game.config.Clock()
			draft.timestamp = time.Unix(int64(now), 0).UTC()
		}
		receipt, err := c.game.owners.ExecuteBoardWrite(ctx, c.game.config.Store, c.game.config.WorldID, draft.commandID, c.lease, payload, draft.timestamp)
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
				return "", err
			}
			return "게시물을 등록하지 못했습니다. 다시 시도해 주세요.\r\n", nil
		}
		var result world.BoardWriteResult
		if err := json.Unmarshal(receipt.Response, &result); err != nil {
			return "게시물을 등록하지 못했습니다. 다시 시도해 주세요.\r\n", nil
		}
		c.clearCompose()
		if !receipt.Replayed && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				publishWorldRoomEvent(c.game, after, result.Event.RoomID, result.Event.ActorID, result.Event.ExcludeActorID, result.Event.Text)
			}
		}
		return result.Response, nil
	}
	if err := world.ValidateBoardWriteLine(line); err != nil {
		return session.BoardWriteInvalidLineResponse + fmt.Sprintf("%3d: ", len(draft.body)+1), nil
	}
	candidate := append(append([]string(nil), draft.body...), line)
	if len(strings.Join(candidate, "\n"))+1 > world.MaxBoardBodyBytes {
		return session.BoardWriteInvalidLineResponse + fmt.Sprintf("%3d: ", len(draft.body)+1), nil
	}
	draft.body = candidate
	return fmt.Sprintf("%3d: ", len(draft.body)+1), nil
}

func (c *worldConnection) clearVote() {
	if c.vote == nil {
		return
	}
	c.vote.continuation.Choices = nil
	c.vote.catalog.Issue.Options = nil
	c.vote.commandID = ""
	c.vote.lastChoice = 0
	c.vote.phase = 0
	c.vote = nil
}

func (c *worldConnection) clearForgeSelectArm() {
	c.forgeSelectArmPending = false
	c.forgeSelectArmCommandID = ""
}

func (c *worldConnection) clearForgeMaterial() {
	c.forgeMaterialPending = false
	c.forgeMaterialCommandID = ""
}

func (c *worldConnection) clearForgeQuench() {
	c.forgeQuenchPending = false
	c.forgeQuenchCommandID = ""
	c.forgeObjectID = 0
	c.forgeSum = 0
	c.forgeQuenchChoice = 0
}

func (c *worldConnection) clearForgeName() {
	c.forgeNamePending = false
	c.forgeNameCommandID = ""
}

func (c *worldConnection) clearForgeConfirm() {
	c.forgeConfirmPending = false
	c.forgeConfirmCommandID = ""
	c.forgeWeaponName = ""
}

func (c *worldConnection) clearForgeFlow() {
	c.clearForgeSelectArm()
	c.clearForgeMaterial()
	c.clearForgeQuench()
	c.clearForgeName()
	c.clearForgeConfirm()
}

func (c *worldConnection) clearNewForgeSelectArm() {
	c.newForgeSelectArmPending = false
	c.newForgeSelectArmCommandID = ""
}

func (c *worldConnection) clearNewForgeMaterial() {
	c.newForgeMaterialPending = false
	c.newForgeMaterialCommandID = ""
	c.newForgeObjectID = 0
	c.newForgeSum = 0
}

func (c *worldConnection) clearNewForgeQuench() {
	c.newForgeQuenchPending = false
	c.newForgeQuenchCommandID = ""
	c.newForgeQuenchChoice = 0
}

func (c *worldConnection) clearNewForgeName() {
	c.newForgeNamePending = false
	c.newForgeNameCommandID = ""
}

func (c *worldConnection) clearNewForgeConfirm() {
	c.newForgeConfirmPending = false
	c.newForgeConfirmCommandID = ""
	c.newForgeWeaponName = ""
}

func (c *worldConnection) clearNewForgeFlow() {
	c.clearNewForgeSelectArm()
	c.clearNewForgeMaterial()
	c.clearNewForgeQuench()
	c.clearNewForgeName()
	c.clearNewForgeConfirm()
}

// submitNewForgeSelectArmLine owns command7.c:select_newarm case 2 after
// 무기만들기. It runs before history/alias/parser handling so a digit or
// invalid answer cannot become 도/flee, 제련, or another terminal command.
// Replay of the same command ID does not re-commit.
func (c *worldConnection) submitNewForgeSelectArmLine(ctx context.Context, line string) (string, bool, error) {
	if !c.newForgeSelectArmPending {
		return "", false, nil
	}
	if session.IsNewForgeLine(line) {
		return "", false, nil
	}
	if !session.IsNewForgeSelectArmLine(line) {
		return "", false, nil
	}
	if c.newForgeSelectArmCommandID == "" {
		c.newForgeSelectArmCommandID = "newforge-select-arm-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteNewForgeSelectArmLine(ctx, c.game.config.Store, c.game.config.WorldID, c.newForgeSelectArmCommandID, c.lease, line, c.game.config.Catalog)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedNewForgeLine) ||
			errors.Is(err, world.ErrNewForgeActorAbsent) ||
			errors.Is(err, world.ErrNewForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrNewForgeNameInvalid) ||
			errors.Is(err, world.ErrNewForgeStaleProposal) ||
			errors.Is(err, world.ErrNewForgeInvalidProposal) ||
			errors.Is(err, world.ErrNewForgeNotReading) ||
			errors.Is(err, world.ErrNewForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrNewForgeSelectArmInput) {
			c.clearNewForgeFlow()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.NewForgeReprompt:
		c.newForgeSelectArmCommandID = ""
	case world.NewForgeMaterial:
		c.clearNewForgeSelectArm()
		c.newForgeMaterialPending = true
		c.newForgeMaterialCommandID = ""
		c.newForgeObjectID = result.ObjectID
		c.newForgeSum = 0
	default:
		c.clearNewForgeFlow()
	}
	return result.Response, true, nil
}

// submitNewForgeSelectMaterialLine owns command7.c:select_newarm case 3
// after a weapon type. It runs before history/alias/parser and before 제련
// intercepts so a digit or invalid answer cannot become 도/flee, 제련, or
// another terminal command. Replay of the same command ID does not
// re-commit or charge gold. A successful material arms the case-4 quench
// intercept. Case 5 is submitNewForgeSelectNameLine. Case 6 is
// submitNewForgeSelectConfirmLine.
func (c *worldConnection) submitNewForgeSelectMaterialLine(ctx context.Context, line string) (string, bool, error) {
	if !c.newForgeMaterialPending {
		return "", false, nil
	}
	if session.IsNewForgeLine(line) {
		return "", false, nil
	}
	if !session.IsNewForgeSelectMaterialLine(line) {
		return "", false, nil
	}
	if c.newForgeMaterialCommandID == "" {
		c.newForgeMaterialCommandID = "newforge-select-material-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteNewForgeSelectMaterialLine(ctx, c.game.config.Store, c.game.config.WorldID, c.newForgeMaterialCommandID, c.lease, line, c.game.config.Catalog, c.newForgeObjectID)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedNewForgeLine) ||
			errors.Is(err, world.ErrNewForgeActorAbsent) ||
			errors.Is(err, world.ErrNewForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrNewForgeNameInvalid) ||
			errors.Is(err, world.ErrNewForgeStaleProposal) ||
			errors.Is(err, world.ErrNewForgeInvalidProposal) ||
			errors.Is(err, world.ErrNewForgeNotReading) ||
			errors.Is(err, world.ErrNewForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrNewForgeSelectArmInput) {
			c.clearNewForgeMaterial()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.NewForgeReprompt:
		c.newForgeMaterialCommandID = ""
	case world.NewForgeQuench:
		c.newForgeMaterialPending = false
		c.newForgeMaterialCommandID = ""
		c.newForgeQuenchPending = true
		c.newForgeQuenchCommandID = ""
		c.newForgeObjectID = result.ObjectID
		c.newForgeSum = result.Sum
		c.newForgeQuenchChoice = 0
	default:
		c.clearNewForgeMaterial()
		c.clearNewForgeQuench()
	}
	return result.Response, true, nil
}

// submitNewForgeSelectQuenchLine owns command7.c:select_newarm case 4
// after a material. It runs before history/alias/parser and before 제련
// intercepts so a digit or invalid answer cannot become 도/flee, 제련, or
// another terminal command. Replay of the same command ID does not
// re-commit or charge gold. A successful name prompt arms the case-5
// intercept. Case 6 is submitNewForgeSelectConfirmLine.
func (c *worldConnection) submitNewForgeSelectQuenchLine(ctx context.Context, line string) (string, bool, error) {
	if !c.newForgeQuenchPending {
		return "", false, nil
	}
	if session.IsNewForgeLine(line) {
		return "", false, nil
	}
	if !session.IsNewForgeSelectQuenchLine(line) {
		return "", false, nil
	}
	if c.newForgeQuenchCommandID == "" {
		c.newForgeQuenchCommandID = "newforge-select-quench-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteNewForgeSelectQuenchLine(ctx, c.game.config.Store, c.game.config.WorldID, c.newForgeQuenchCommandID, c.lease, line, c.game.config.Catalog, c.newForgeObjectID, c.newForgeSum)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedNewForgeLine) ||
			errors.Is(err, world.ErrNewForgeActorAbsent) ||
			errors.Is(err, world.ErrNewForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrNewForgeNameInvalid) ||
			errors.Is(err, world.ErrNewForgeStaleProposal) ||
			errors.Is(err, world.ErrNewForgeInvalidProposal) ||
			errors.Is(err, world.ErrNewForgeNotReading) ||
			errors.Is(err, world.ErrNewForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrNewForgeSelectArmInput) {
			c.clearNewForgeQuench()
			c.newForgeObjectID = 0
			c.newForgeSum = 0
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.NewForgeReprompt:
		c.newForgeQuenchCommandID = ""
	case world.NewForgeName:
		c.newForgeQuenchPending = false
		c.newForgeQuenchCommandID = ""
		c.newForgeNamePending = true
		c.newForgeNameCommandID = ""
		c.newForgeObjectID = result.ObjectID
		c.newForgeQuenchChoice = result.QuenchChoice
		if result.MaterialSum != 0 {
			c.newForgeSum = result.MaterialSum
		}
	default:
		c.clearNewForgeQuench()
		c.clearNewForgeName()
		c.newForgeObjectID = 0
		c.newForgeSum = 0
	}
	return result.Response, true, nil
}

// submitNewForgeSelectNameLine owns command7.c:select_newarm case 5 after
// a quench. It runs before history/alias/parser and before 제련 intercepts
// so a weapon name cannot become 도/flee, 제련, or another terminal
// command. Replay of the same command ID does not re-commit or charge
// gold. A successful name arms the case-6 confirm intercept.
func (c *worldConnection) submitNewForgeSelectNameLine(ctx context.Context, line string) (string, bool, error) {
	if !c.newForgeNamePending {
		return "", false, nil
	}
	if session.IsNewForgeLine(line) {
		return "", false, nil
	}
	if !session.IsNewForgeSelectNameLine(line) {
		return "", false, nil
	}
	if c.newForgeNameCommandID == "" {
		c.newForgeNameCommandID = "newforge-select-name-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteNewForgeSelectNameLine(ctx, c.game.config.Store, c.game.config.WorldID, c.newForgeNameCommandID, c.lease, line, c.game.config.Catalog, c.newForgeObjectID, c.newForgeSum, c.newForgeQuenchChoice)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedNewForgeLine) ||
			errors.Is(err, world.ErrNewForgeActorAbsent) ||
			errors.Is(err, world.ErrNewForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrNewForgeNameInvalid) ||
			errors.Is(err, world.ErrNewForgeStaleProposal) ||
			errors.Is(err, world.ErrNewForgeInvalidProposal) ||
			errors.Is(err, world.ErrNewForgeNotReading) ||
			errors.Is(err, world.ErrNewForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrNewForgeSelectArmInput) {
			c.clearNewForgeName()
			c.newForgeObjectID = 0
			c.newForgeSum = 0
			c.newForgeQuenchChoice = 0
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.NewForgeReprompt:
		c.newForgeNameCommandID = ""
	case world.NewForgeConfirm:
		c.clearNewForgeName()
		c.newForgeConfirmPending = true
		c.newForgeConfirmCommandID = ""
		c.newForgeWeaponName = result.ObjectName
		c.newForgeObjectID = result.ObjectID
		if result.QuenchChoice != 0 {
			c.newForgeQuenchChoice = result.QuenchChoice
		}
		if result.MaterialSum != 0 {
			c.newForgeSum = result.MaterialSum
		}
	default:
		c.clearNewForgeName()
		c.clearNewForgeConfirm()
		c.newForgeObjectID = 0
		c.newForgeSum = 0
		c.newForgeQuenchChoice = 0
	}
	return result.Response, true, nil
}

// submitNewForgeSelectConfirmLine owns command7.c:select_newarm case 6 after
// a name. It runs before history/alias/parser and before 제련 intercepts so
// 예/아니오 cannot become another terminal command. Replay of the same
// command ID does not re-charge gold or add a second weapon.
func (c *worldConnection) submitNewForgeSelectConfirmLine(ctx context.Context, line string) (string, bool, error) {
	if !c.newForgeConfirmPending {
		return "", false, nil
	}
	if !session.IsNewForgeSelectConfirmLine(line) {
		return "", false, nil
	}
	if c.newForgeConfirmCommandID == "" {
		c.newForgeConfirmCommandID = "newforge-select-confirm-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteNewForgeSelectConfirmLine(ctx, c.game.config.Store, c.game.config.WorldID, c.newForgeConfirmCommandID, c.lease, line, c.game.config.Catalog, c.newForgeObjectID, c.newForgeSum, c.newForgeQuenchChoice, c.newForgeWeaponName)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedNewForgeLine) ||
			errors.Is(err, world.ErrNewForgeActorAbsent) ||
			errors.Is(err, world.ErrNewForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrNewForgeNameInvalid) ||
			errors.Is(err, world.ErrNewForgeStaleProposal) ||
			errors.Is(err, world.ErrNewForgeInvalidProposal) ||
			errors.Is(err, world.ErrNewForgeNotReading) ||
			errors.Is(err, world.ErrNewForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrNewForgeSelectArmInput) ||
			errors.Is(err, world.ErrNewForgeGoldObjectUnmigrated) ||
			errors.Is(err, world.ErrNewForgeItemAllocatorUnavailable) {
			c.clearNewForgeFlow()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	c.clearNewForgeFlow()
	if !receipt.Replayed && len(result.Events) > 0 {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishNewForge(after, result.Events)
		}
	}
	return result.Response, true, nil
}

// publishForge delivers select_arm case 6's broadcast_rom line after the
// first commit. Recipients are current same-room observers; the actor
// already received the command response. A replay never calls this method.
func (g *WorldConnector) publishForge(after world.State, events []world.ForgeEvent) {
	if len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, event := range events {
		if event.ActorID == "" || event.RoomID == 0 || event.Text == "" {
			continue
		}
		actor, ok := after.Players[event.ActorID]
		if !ok || !actor.Online || actor.Body.RoomID != event.RoomID {
			continue
		}
		for connection := range g.connections {
			player, ok := after.Players[connection.lease.ActorID]
			if !ok || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
			}
		}
	}
}

// publishNewForge delivers select_newarm case 6's broadcast_rom line after
// the first commit. Recipients are current same-room observers; the actor
// already received the command response. A replay never calls this method.
func (g *WorldConnector) publishNewForge(after world.State, events []world.NewForgeEvent) {
	if len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, event := range events {
		if event.ActorID == "" || event.RoomID == 0 || event.Text == "" {
			continue
		}
		actor, ok := after.Players[event.ActorID]
		if !ok || !actor.Online || actor.Body.RoomID != event.RoomID {
			continue
		}
		for connection := range g.connections {
			player, ok := after.Players[connection.lease.ActorID]
			if !ok || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
			}
		}
	}
}

func (c *worldConnection) clearSuicidePassword() {
	c.suicidePasswordPending = false
	c.suicidePasswordCommandID = ""
	c.passwordSecret = false
}

// submitSuicidePasswordLine owns command5.c:suicide case 2 after 목매달기.
// It runs before history/alias/parser handling so the password cannot become
// `!` history or another terminal command. Replay of the same command ID
// does not re-commit or re-prompt. Param 3 confirm/archive stays fail-closed.
func (c *worldConnection) submitSuicidePasswordLine(ctx context.Context, line string) (string, bool, error) {
	if !c.suicidePasswordPending {
		return "", false, nil
	}
	if c.suicidePasswordCommandID == "" {
		c.suicidePasswordCommandID = "suicide-password-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteSuicidePasswordLine(ctx, c.game.config.Store, c.game.config.WorldID, c.suicidePasswordCommandID, c.lease, line, session.SuicidePasswordOptions{
		Accounts: c.game.config.Accounts, PasswordStore: c.game.config.PasswordStore, Name: c.accountName,
	})
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, world.ErrSuicideConfirmUnmigrated) ||
			errors.Is(err, world.ErrSuicideNotReading) ||
			errors.Is(err, world.ErrSuicideActorAbsent) ||
			errors.Is(err, world.ErrSuicideNameInvalid) ||
			errors.Is(err, world.ErrSuicideStaleProposal) ||
			errors.Is(err, world.ErrSuicideInvalidProposal) {
			c.clearSuicidePassword()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.SuicideResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	c.clearSuicidePassword()
	return result.Response, true, nil
}

// submitForgeSelectArmLine owns command7.c:select_arm case 2 after 제련.
// It runs before history/alias/parser handling so a digit or invalid
// answer cannot become 도/flee or another terminal command. Replay of the
// same command ID does not re-commit.
func (c *worldConnection) submitForgeSelectArmLine(ctx context.Context, line string) (string, bool, error) {
	if !c.forgeSelectArmPending {
		return "", false, nil
	}
	if session.IsForgeLine(line) {
		return "", false, nil
	}
	if !session.IsForgeSelectArmLine(line) {
		return "", false, nil
	}
	if c.forgeSelectArmCommandID == "" {
		c.forgeSelectArmCommandID = "forge-select-arm-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteForgeSelectArmLine(ctx, c.game.config.Store, c.game.config.WorldID, c.forgeSelectArmCommandID, c.lease, line, c.game.config.Catalog)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedForgeLine) ||
			errors.Is(err, world.ErrForgeActorAbsent) ||
			errors.Is(err, world.ErrForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrForgeNameInvalid) ||
			errors.Is(err, world.ErrForgeStaleProposal) ||
			errors.Is(err, world.ErrForgeInvalidProposal) ||
			errors.Is(err, world.ErrForgeNotReading) ||
			errors.Is(err, world.ErrForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrForgeSelectArmInput) ||
			errors.Is(err, world.ErrForgeGoldObjectUnmigrated) {
			c.clearForgeFlow()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.ForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.ForgeReprompt:
		c.forgeSelectArmCommandID = ""
	case world.ForgeMaterial:
		c.clearForgeSelectArm()
		c.clearForgeQuench()
		c.clearForgeName()
		c.clearForgeConfirm()
		c.forgeMaterialPending = true
		c.forgeMaterialCommandID = ""
		c.forgeObjectID = result.ObjectID
	default:
		c.clearForgeFlow()
	}
	return result.Response, true, nil
}

// submitForgeSelectMaterialLine owns command7.c:select_arm case 3 after a
// weapon type. It runs before history/alias/parser handling so a digit or
// invalid answer cannot become 도/flee or another terminal command. Replay
// of the same command ID does not re-commit or charge gold.
func (c *worldConnection) submitForgeSelectMaterialLine(ctx context.Context, line string) (string, bool, error) {
	if !c.forgeMaterialPending {
		return "", false, nil
	}
	if session.IsForgeLine(line) {
		return "", false, nil
	}
	if !session.IsForgeSelectMaterialLine(line) {
		return "", false, nil
	}
	if c.forgeMaterialCommandID == "" {
		c.forgeMaterialCommandID = "forge-select-material-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteForgeSelectMaterialLine(ctx, c.game.config.Store, c.game.config.WorldID, c.forgeMaterialCommandID, c.lease, line, c.game.config.Catalog, c.forgeObjectID)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedForgeLine) ||
			errors.Is(err, world.ErrForgeActorAbsent) ||
			errors.Is(err, world.ErrForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrForgeNameInvalid) ||
			errors.Is(err, world.ErrForgeStaleProposal) ||
			errors.Is(err, world.ErrForgeInvalidProposal) ||
			errors.Is(err, world.ErrForgeNotReading) ||
			errors.Is(err, world.ErrForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrForgeSelectArmInput) ||
			errors.Is(err, world.ErrForgeGoldObjectUnmigrated) {
			c.clearForgeMaterial()
			c.clearForgeQuench()
			c.clearForgeName()
			c.clearForgeConfirm()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.ForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.ForgeReprompt, world.ForgeMaterialDenied:
		c.forgeMaterialCommandID = ""
	case world.ForgeQuench:
		c.forgeMaterialPending = false
		c.forgeMaterialCommandID = ""
		c.forgeQuenchPending = true
		c.forgeQuenchCommandID = ""
		c.forgeObjectID = result.ObjectID
		c.forgeSum = result.Sum
		c.clearForgeName()
		c.clearForgeConfirm()
	default:
		c.clearForgeMaterial()
		c.clearForgeQuench()
		c.clearForgeName()
		c.clearForgeConfirm()
	}
	return result.Response, true, nil
}

// submitForgeSelectQuenchLine owns command7.c:select_arm case 4 after a
// material. It runs before history/alias/parser handling so a digit or
// invalid answer cannot become 도/flee or another terminal command. Replay
// of the same command ID does not re-commit or charge gold. A successful
// quench arms the case-5 name intercept. Case 6 stays fail-closed.
func (c *worldConnection) submitForgeSelectQuenchLine(ctx context.Context, line string) (string, bool, error) {
	if !c.forgeQuenchPending {
		return "", false, nil
	}
	if session.IsForgeLine(line) {
		return "", false, nil
	}
	if !session.IsForgeSelectQuenchLine(line) {
		return "", false, nil
	}
	if c.forgeQuenchCommandID == "" {
		c.forgeQuenchCommandID = "forge-select-quench-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteForgeSelectQuenchLine(ctx, c.game.config.Store, c.game.config.WorldID, c.forgeQuenchCommandID, c.lease, line, c.game.config.Catalog, c.forgeObjectID, c.forgeSum)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedForgeLine) ||
			errors.Is(err, world.ErrForgeActorAbsent) ||
			errors.Is(err, world.ErrForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrForgeNameInvalid) ||
			errors.Is(err, world.ErrForgeStaleProposal) ||
			errors.Is(err, world.ErrForgeInvalidProposal) ||
			errors.Is(err, world.ErrForgeNotReading) ||
			errors.Is(err, world.ErrForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrForgeSelectArmInput) ||
			errors.Is(err, world.ErrForgeGoldObjectUnmigrated) {
			c.clearForgeQuench()
			c.clearForgeName()
			c.clearForgeConfirm()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.ForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.ForgeReprompt:
		c.forgeQuenchCommandID = ""
	case world.ForgeName:
		c.forgeQuenchPending = false
		c.forgeQuenchCommandID = ""
		c.forgeNamePending = true
		c.forgeNameCommandID = ""
		c.forgeObjectID = result.ObjectID
		c.forgeQuenchChoice = result.QuenchChoice
		if result.MaterialSum != 0 {
			c.forgeSum = result.MaterialSum
		}
		c.clearForgeConfirm()
	default:
		c.clearForgeQuench()
		c.clearForgeName()
		c.clearForgeConfirm()
	}
	return result.Response, true, nil
}

// submitForgeSelectNameLine owns command7.c:select_arm case 5 after a
// quench. It runs before history/alias/parser handling so a weapon name
// cannot become another terminal command. Replay of the same command ID
// does not re-commit or charge gold. A successful confirm prompt arms
// the case-6 gold-charge intercept.
func (c *worldConnection) submitForgeSelectNameLine(ctx context.Context, line string) (string, bool, error) {
	if !c.forgeNamePending {
		return "", false, nil
	}
	if session.IsForgeLine(line) {
		return "", false, nil
	}
	if !session.IsForgeSelectNameLine(line) {
		return "", false, nil
	}
	if c.forgeNameCommandID == "" {
		c.forgeNameCommandID = "forge-select-name-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteForgeSelectNameLine(ctx, c.game.config.Store, c.game.config.WorldID, c.forgeNameCommandID, c.lease, line, c.game.config.Catalog, c.forgeObjectID, c.forgeSum, c.forgeQuenchChoice)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedForgeLine) ||
			errors.Is(err, world.ErrForgeActorAbsent) ||
			errors.Is(err, world.ErrForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrForgeNameInvalid) ||
			errors.Is(err, world.ErrForgeStaleProposal) ||
			errors.Is(err, world.ErrForgeInvalidProposal) ||
			errors.Is(err, world.ErrForgeNotReading) ||
			errors.Is(err, world.ErrForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrForgeSelectArmInput) ||
			errors.Is(err, world.ErrForgeGoldObjectUnmigrated) ||
			errors.Is(err, world.ErrForgeItemAllocatorUnavailable) {
			c.clearForgeName()
			c.clearForgeQuench()
			c.clearForgeConfirm()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.ForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	switch result.Action {
	case world.ForgeReprompt:
		c.forgeNameCommandID = ""
	case world.ForgeConfirm:
		c.clearForgeName()
		c.forgeConfirmPending = true
		c.forgeConfirmCommandID = ""
		c.forgeWeaponName = result.ObjectName
		if result.ObjectID != 0 {
			c.forgeObjectID = result.ObjectID
		}
		if result.QuenchChoice != 0 {
			c.forgeQuenchChoice = result.QuenchChoice
		}
		if result.MaterialSum != 0 {
			c.forgeSum = result.MaterialSum
		}
	default:
		c.clearForgeName()
		c.clearForgeConfirm()
	}
	return result.Response, true, nil
}

// submitForgeSelectConfirmLine owns command7.c:select_arm case 6 after a
// name. It runs before history/alias/parser handling so 예/아니오 cannot
// become another terminal command. Replay of the same command ID does not
// re-charge gold or add a second weapon.
func (c *worldConnection) submitForgeSelectConfirmLine(ctx context.Context, line string) (string, bool, error) {
	if !c.forgeConfirmPending {
		return "", false, nil
	}
	if !session.IsForgeSelectConfirmLine(line) {
		return "", false, nil
	}
	if c.forgeConfirmCommandID == "" {
		c.forgeConfirmCommandID = "forge-select-confirm-" + rand.Text()
	}
	receipt, err := c.game.owners.ExecuteForgeSelectConfirmLine(ctx, c.game.config.Store, c.game.config.WorldID, c.forgeConfirmCommandID, c.lease, line, c.game.config.Catalog, c.forgeObjectID, c.forgeSum, c.forgeQuenchChoice, c.forgeWeaponName)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", true, err
		}
		if errors.Is(err, session.ErrUnsupportedForgeLine) ||
			errors.Is(err, world.ErrForgeActorAbsent) ||
			errors.Is(err, world.ErrForgeFlagsUnresolved) ||
			errors.Is(err, world.ErrForgeNameInvalid) ||
			errors.Is(err, world.ErrForgeStaleProposal) ||
			errors.Is(err, world.ErrForgeInvalidProposal) ||
			errors.Is(err, world.ErrForgeNotReading) ||
			errors.Is(err, world.ErrForgeCatalogUnmigrated) ||
			errors.Is(err, world.ErrForgeSelectArmInput) ||
			errors.Is(err, world.ErrForgeGoldObjectUnmigrated) ||
			errors.Is(err, world.ErrForgeItemAllocatorUnavailable) {
			c.clearForgeFlow()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return "", true, err
	}
	var result world.ForgeResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return "", true, err
	}
	c.clearForgeFlow()
	if !receipt.Replayed && len(result.Events) > 0 {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishForge(after, result.Events)
		}
	}
	return result.Response, true, nil
}

// submitVoteLine owns both the initial `투표` line and all source
// vote_cmnd continuations. It runs before history/alias/parser handling so a
// prompt response can never become an ordinary terminal command.
func (c *worldConnection) submitVoteLine(ctx context.Context, line string) (string, bool, error) {
	if c.vote == nil {
		if _, ok := session.ParseVoteLine(line); !ok {
			return "", false, nil
		}
		start, err := c.game.owners.BeginVoteContinuation(ctx, c.game.config.Store, c.game.config.WorldID, c.lease, session.VoteOptions{Catalog: c.game.config.VoteCatalog})
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
				return "", true, err
			}
			// A missing/incomplete canonical vote aggregate and all other
			// unsupported source gates fail closed without a receipt.
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		phase := voteChoicePhase
		if start.Projection.HasBallot {
			phase = voteConfirmPhase
		}
		c.vote = &voteDraft{
			commandID:    "vote-" + rand.Text(),
			catalog:      start.Catalog,
			continuation: start.Continuation,
			phase:        phase,
		}
		c.lastCommand = strings.TrimLeft(line, " ")
		if phase == voteConfirmPhase {
			return session.VoteAlreadyVotedResponse + session.VoteChangePrompt, true, nil
		}
		prompt, err := session.VotePromptForOption(start.Catalog.Issue, 0)
		if err != nil {
			c.clearVote()
			return "아직 구현되지 않은 명령입니다.\r\n", true, nil
		}
		return prompt, true, nil
	}
	output, err := c.submitVoteContinuation(ctx, line)
	return output, true, err
}

func (c *worldConnection) submitVoteContinuation(ctx context.Context, line string) (string, error) {
	draft := c.vote
	if draft == nil {
		return "", nil
	}
	switch draft.phase {
	case voteConfirmPhase:
		input, ok := session.ParseVoteConfirmationLine(line)
		if !ok || input.Choice != 'Y' {
			c.clearVote()
			return session.VoteCancelResponse, nil
		}
		draft.phase = voteChoicePhase
		prompt, err := session.VotePromptForOption(draft.catalog.Issue, 0)
		if err != nil {
			c.clearVote()
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		return prompt, nil
	case voteChoicePhase:
		input, ok := session.ParseVoteChoiceLine(line)
		if !ok {
			c.clearVote()
			return session.VoteInvalidChoiceResponse, nil
		}
		next, done, err := draft.continuation.Choose(string([]byte{input.Choice}))
		if err != nil {
			c.clearVote()
			return session.VoteInvalidChoiceResponse, nil
		}
		draft.continuation = next
		draft.lastChoice = input.Choice
		if !done {
			prompt, promptErr := session.VotePromptForOption(draft.catalog.Issue, next.NextOption-1)
			if promptErr != nil {
				c.clearVote()
				return "아직 구현되지 않은 명령입니다.\r\n", nil
			}
			return prompt, nil
		}
		return c.commitVote(ctx, draft)
	case voteCommitPhase:
		// A receipt commit may have succeeded while its response was lost.
		// Retain the exact command ID and choices, and permit only a retry of
		// the same final choice (or an explicit dot retry) so arbitrary input
		// cannot become a vote authorization channel.
		if line != "." {
			input, ok := session.ParseVoteChoiceLine(line)
			if !ok || input.Choice != draft.lastChoice {
				return session.VoteCommitRetryResponse, nil
			}
		}
		return c.commitVote(ctx, draft)
	default:
		c.clearVote()
		return "아직 구현되지 않은 명령입니다.\r\n", nil
	}
}

func (c *worldConnection) commitVote(ctx context.Context, draft *voteDraft) (string, error) {
	receipt, err := c.game.owners.ExecuteVoteContinuation(ctx, c.game.config.Store, c.game.config.WorldID, draft.commandID, c.lease, draft.catalog, draft.continuation)
	if err != nil {
		if !c.game.owners.Owns(c.lease) {
			c.ready = false
			return "", err
		}
		if errors.Is(err, world.ErrVoteCatalogUnavailable) ||
			errors.Is(err, world.ErrVoteCatalogInvalid) ||
			errors.Is(err, world.ErrVoteActorAbsent) ||
			errors.Is(err, world.ErrVoteAge) ||
			errors.Is(err, world.ErrVoteRoom) ||
			errors.Is(err, world.ErrVoteNumeric) ||
			errors.Is(err, world.ErrVoteStateUnresolved) ||
			errors.Is(err, world.ErrVoteStateInvalid) ||
			errors.Is(err, world.ErrVoteBallotInvalid) ||
			errors.Is(err, world.ErrVoteHistoryInvalid) ||
			errors.Is(err, world.ErrVoteChoicesRequired) ||
			errors.Is(err, world.ErrVoteChoicesInvalid) ||
			errors.Is(err, world.ErrVoteNoBallot) ||
			errors.Is(err, world.ErrVoteStaleProposal) ||
			errors.Is(err, world.ErrVoteInvalidProposal) {
			c.clearVote()
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		draft.phase = voteCommitPhase
		return session.VoteCommitRetryResponse, nil
	}
	var result world.VoteResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		// Keep the draft and command ID so a malformed/lost response can be
		// retried through the same durable receipt identity.
		draft.phase = voteCommitPhase
		return session.VoteCommitRetryResponse, nil
	}
	c.clearVote()
	return result.Response, nil
}

func (c *worldConnection) Submit(ctx context.Context, line string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || !c.ready {
		return "", errors.New("world connection not ready")
	}
	c.game.commandMu.Lock()
	defer c.game.commandMu.Unlock()
	if c.infoPending {
		// command4.c routes exactly one following line to info_2. Consume the
		// connection-local continuation before either branch. Cancellation is
		// local, while the selected page is a read-only durable receipt whose
		// command ID stays stable until the commit succeeds.
		if line == "." {
			c.infoPending = false
			c.infoContinuationCommandID = ""
			return session.InfoContinuationCancelResponse, nil
		}
		if c.infoContinuationCommandID == "" {
			c.infoContinuationCommandID = "info-continuation-" + rand.Text()
		}
		receipt, err := c.game.owners.ExecuteInfoContinuation(ctx, c.game.config.Store, c.game.config.WorldID, c.infoContinuationCommandID, c.lease)
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		var text string
		if err := json.Unmarshal(receipt.Response, &text); err != nil {
			return "", err
		}
		c.infoPending = false
		c.infoContinuationCommandID = ""
		return text, nil
	}
	// An active password change owns every following line until it reaches a
	// terminal state. Handle it before compose/history so credentials can
	// never be expanded into command history or interpreted as world input.
	if c.passwordChange != nil {
		output, _, err := c.submitPasswordLine(ctx, line)
		return output, err
	}
	// suicide case 2 owns the next raw line after 목매달기. Route it before
	// compose/vote/password/history/alias/parser so the password cannot
	// become another command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitSuicidePasswordLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	if output, handled, err := c.submitComposeLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	if output, handled, err := c.submitVoteLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	if output, handled, err := c.submitNotepadLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// Start `암호` before history expansion. The command is connection-local,
	// so its line and all subsequent credential lines must not become the `!`
	// history entry.
	if output, handled, err := c.submitPasswordLine(ctx, line); handled {
		return output, err
	}
	// select_newarm case 2 owns the next raw line after 무기만들기. Route
	// it before 제련/history/alias/parser so a digit cannot become another
	// command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitNewForgeSelectArmLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_newarm case 3 owns the next raw line after a weapon type.
	// Route it before 제련/history/alias/parser so a digit cannot become
	// another command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitNewForgeSelectMaterialLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_newarm case 4 owns the next raw line after a material.
	// Route it before 제련/history/alias/parser so a digit cannot become
	// another command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitNewForgeSelectQuenchLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_newarm case 5 owns the next raw line after a quench.
	// Route it before 제련/history/alias/parser so a weapon name cannot
	// become another command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitNewForgeSelectNameLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_newarm case 6 owns the next raw line after a weapon name.
	// Route it before 제련/history/alias/parser so 예/아니오 cannot become
	// another command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitNewForgeSelectConfirmLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_arm case 2 owns the next raw line after 제련. Route it before
	// history/alias/parser so a digit cannot become another command and a
	// retry reuses the same receipt identity.
	if output, handled, err := c.submitForgeSelectArmLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_arm case 3 owns the next raw line after a weapon type. Route
	// it before history/alias/parser so a digit cannot become another
	// command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitForgeSelectMaterialLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_arm case 4 owns the next raw line after a material. Route
	// it before history/alias/parser so a digit cannot become another
	// command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitForgeSelectQuenchLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_arm case 5 owns the next raw line after a quench. Route
	// it before history/alias/parser so a weapon name cannot become
	// another command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitForgeSelectNameLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// select_arm case 6 owns the next raw line after a weapon name. Route
	// it before history/alias/parser so 예/아니오 cannot become another
	// command and a retry reuses the same receipt identity.
	if output, handled, err := c.submitForgeSelectConfirmLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	line, c.lastCommand = session.ExpandHistoryLine(c.lastCommand, line)
	// History expansion can recreate an interactive command. Route that
	// expanded line through the same continuation gate before aliases or the
	// ordinary parser; otherwise `!` after `편지보내기`/`써` would be classified
	// as a command kind with no editor entry point.
	if output, handled, err := c.submitComposeLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	// History expansion can recreate the interactive `투표` command. Route it
	// through the same connection-local gate before alias expansion/parser
	// handling, just as the compose editor is routed above.
	if output, handled, err := c.submitVoteLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	if output, handled, err := c.submitNotepadLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	now, hour := c.game.config.Clock()
	before, beforeOK := c.game.snapshot(ctx)
	if beforeOK {
		expanded, matched, expandErr := session.ExpandAliasLine(before, c.lease.ActorID, line)
		if matched {
			if expandErr != nil {
				return "줄임말을 실행할 수 없습니다.\r\n", nil
			}
			line = expanded
		}
	}
	// An alias is server-owned input, but it may still expand to the exact
	// vote alias. Keep that expansion inside the continuation boundary so a
	// vote prompt is never routed to the ordinary parser.
	if output, handled, err := c.submitVoteLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	if output, handled, err := c.submitNotepadLine(ctx, line); handled {
		if err != nil {
			if !c.game.owners.Owns(c.lease) {
				c.ready = false
			}
			return "", err
		}
		return output, nil
	}
	commandID := "command-" + rand.Text()
	parsed, parseErr := session.ParseCommand(line)
	if errors.Is(parseErr, session.ErrCommandTooManyTokens) {
		return "명령이 너무 깁니다.\r\n", nil
	}
	if parseErr != nil {
		return "명령을 이해할 수 없습니다.\r\n", nil
	}
	if parsed.Kind == session.CommandIgnore {
		output, handled, ignoreErr := c.submitIgnoreLine(line, before, beforeOK)
		if handled {
			return output, ignoreErr
		}
	}
	if parsed.Kind == session.CommandDirectMessage {
		if output, ignored := c.directMessageIgnored(before, line); ignored {
			return output, nil
		}
	}
	replyTargetID, replyTargetName := "", ""
	replyCommand := false
	if parsed.Kind == session.CommandReply {
		var found bool
		replyTargetID, replyTargetName, found = c.getReplyTarget()
		if !found {
			return "누구에게 말을 전하시려구요?\r\n", nil
		}
		if output, ignored := c.directMessageIgnoredForTarget(before, line, replyTargetID, replyTargetName); ignored {
			return output, nil
		}
	}
	directional := parsed.Kind == session.CommandDirectional || parsed.Kind == session.CommandGo
	sayText, sayCommand := "", false
	yellText, yellCommand := "", false
	broadcastCommand := false
	var broadcastOptions world.BroadcastOptions
	emoteCommand := false
	var emote session.EmoteCommand
	expressText := ""
	expressCommand := false
	lookAtTargetCommand := false
	var lookAtTarget session.LookAtTargetCommand
	searchCommand := false
	trackCommand := false
	hideCommand := false
	bribeCommand := false
	fleeCommand := false
	peekCommand := false
	shopListCommand := false
	shopSellCommand := false
	shopPurchaseCommand := false
	tradeCommand := false
	valueCommand := false
	repairCommand := false
	directMessageCommand := false
	stealCommand := false
	teachCommand := false
	backstabCommand := false
	drinkCommand := false
	circleCommand := false
	bashCommand := false
	magicStopCommand := false
	poisonCommand := false
	giveCommand := false
	enemyStatusCommand := false
	timeCommand := false
	selectionCommand := false
	trainingCommand := false
	turnCommand := false
	absorbCommand := false
	kickCommand := false
	useCommand := false
	changeClassCommand := false
	readScrollCommand := false
	castCommand := false
	propertyInviteCommand := false
	familyCommand := false
	familyTalkCommand := false
	familyMutationCommand := false
	familyNewsCommand := false
	familyWarCommand := false
	dmFamilyCommand := false
	dmFollowCommand := false
	dmActiveCommand := false
	dmEnemyCommand := false
	dmCharmCommand := false
	moonSetCommand := false
	zapCommand := false
	forgeCommand := false
	newForgeCommand := false
	buyStatesCommand := false
	suicideCommand := false
	marriageCommand := false
	marriageSendCommand := false
	divorceCommand := false
	voteCommand := false
	merchantPurchaseCommand := false
	npcTalkCommand := false
	groupTalkCommand := false
	descriptionCommand := false
	playerLookupCommand := false
	returnSquareCommand := false
	compareCommand := false
	objectAppraisalCommand := false
	itemRenameCommand := false
	rangerPrayCommand := false
	prepareCommand := false
	upDmgCommand := false
	powerAccuracyCommand := false
	meditateCommand := false
	aliasCommand := false
	burnCommand := false
	studyCommand := false
	saveCommand := false
	mailCommand := false
	memoCommand := false
	notepadCommand := false
	boardCommand := false
	titleCommand := false
	infoCommand := false
	settingsCommand := false
	doorCommand := false
	doorKeyCommand := false
	var receipt storage.WorldReceipt
	var err error
	switch parsed.Kind {
	case session.CommandLook:
		receipt, err = c.game.owners.ExecuteLookLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, hour)
	case session.CommandDirectional:
		receipt, err = c.game.owners.ExecuteDirectionalLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, now, hour, world.SceneOptions{}, c.game.config.Catalog, c.game.config.Roll, c.game.config.Allocate)
	case session.CommandGo:
		receipt, err = c.game.owners.ExecuteGoLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, now, hour, c.game.config.Catalog, c.game.config.Roll, c.game.config.Allocate)
	case session.CommandAttack:
		receipt, err = c.game.owners.ExecuteAttackLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.Roll, session.AttackOptions{Now: now, Allocate: c.game.config.Allocate})
	case session.CommandSteal:
		stealCommand = true
		receipt, err = c.game.owners.ExecuteStealLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.StealOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandTeach:
		teachCommand = true
		receipt, err = c.game.owners.ExecuteTeachLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandBackstab:
		backstabCommand = true
		receipt, err = c.game.owners.ExecuteBackstabLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.BackstabOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandDrink:
		drinkCommand = true
		receipt, err = c.game.owners.ExecuteDrinkLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.DrinkOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandCircle:
		circleCommand = true
		receipt, err = c.game.owners.ExecuteCircleLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.CircleOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandBash:
		bashCommand = true
		receipt, err = c.game.owners.ExecuteBashLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.BashOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandMagicStop:
		magicStopCommand = true
		receipt, err = c.game.owners.ExecuteMagicStopLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.MagicStopOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandPoison:
		poisonCommand = true
		receipt, err = c.game.owners.ExecutePoisonLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.PoisonOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandGive:
		giveCommand = true
		receipt, err = c.game.owners.ExecuteGiveLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandEnemyStatus:
		enemyStatusCommand = true
		receipt, err = c.game.owners.ExecuteEnemyStatusLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandTrain:
		trainingCommand = true
		receipt, err = c.game.owners.ExecuteTrainingLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandSelection:
		selectionCommand = true
		receipt, err = c.game.owners.ExecuteSelectionLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.SelectionOptions{Offers: c.game.config.MerchantOffers})
	case session.CommandTurn:
		turnCommand = true
		receipt, err = c.game.owners.ExecuteTurnLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.TurnOptions{Now: now, Roll: c.game.config.Roll, Allocate: c.game.config.Allocate})
	case session.CommandAbsorb:
		absorbCommand = true
		receipt, err = c.game.owners.ExecuteAbsorbLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.AbsorbOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandKick:
		kickCommand = true
		receipt, err = c.game.owners.ExecuteKickLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.KickOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandUse:
		useCommand = true
		receipt, err = c.game.owners.ExecuteUseLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.UseOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandChangeClass:
		changeClassCommand = true
		receipt, err = c.game.owners.ExecuteChangeClassLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.ChangeClassOptions{})
	case session.CommandReadScroll:
		readScrollCommand = true
		receipt, err = c.game.owners.ExecuteReadScrollLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.ReadScrollOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandCast:
		castCommand = true
		receipt, err = c.game.owners.ExecuteCastLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.CastOptions{Now: now, Hour: hour, Roll: c.game.config.Roll})
	case session.CommandPropertyInvite:
		propertyInviteCommand = true
		receipt, err = c.game.owners.ExecutePropertyInviteLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandFamilyWho, session.CommandFamilyMember, session.CommandFamilyList:
		familyCommand = true
		receipt, err = c.game.owners.ExecuteFamilyLineWithCatalog(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.FamilyCatalog)
	case session.CommandFamilyTalk:
		familyTalkCommand = true
		receipt, err = c.game.owners.ExecuteFamilyTalkLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.FamilyCatalog)
	case session.CommandFamilyMutation:
		familyMutationCommand = true
		receipt, err = c.game.owners.ExecuteFamilyMutationLineWithCatalog(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.FamilyCatalog)
	case session.CommandFamilyNews:
		familyNewsCommand = true
		receipt, err = c.game.owners.ExecuteFamilyNewsLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.FamilyCatalog)
	case session.CommandFamilyWar:
		familyWarCommand = true
		receipt, err = c.game.owners.ExecuteFamilyWarLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.FamilyCatalog)
	case session.CommandDMFamily:
		dmFamilyCommand = true
		receipt, err = c.game.owners.ExecuteDMFamilyLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.DMFamilyOptions{
			Catalog: c.game.config.Catalog, Roll: c.game.config.Roll, Allocate: c.game.config.Allocate,
		})
	case session.CommandDMFollow:
		dmFollowCommand = true
		receipt, err = c.game.owners.ExecuteDMFollowLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandDMActive:
		dmActiveCommand = true
		receipt, err = c.game.owners.ExecuteDMActiveLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandDMEnemy:
		dmEnemyCommand = true
		receipt, err = c.game.owners.ExecuteDMEnemyLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandDMCharm:
		dmCharmCommand = true
		receipt, err = c.game.owners.ExecuteDMCharmLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandMoonSet:
		moonSetCommand = true
		receipt, err = c.game.owners.ExecuteMoonSetLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandZap:
		zapCommand = true
		receipt, err = c.game.owners.ExecuteZapLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.ZapOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandForge:
		forgeCommand = true
		receipt, err = c.game.owners.ExecuteForgeLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandNewForge:
		newForgeCommand = true
		receipt, err = c.game.owners.ExecuteNewForgeLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandBuyStates:
		buyStatesCommand = true
		receipt, err = c.game.owners.ExecuteBuyStatesLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandSuicide:
		suicideCommand = true
		receipt, err = c.game.owners.ExecuteSuicideLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandMarriage:
		marriageCommand = true
		receipt, err = c.game.owners.ExecuteMarriageLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandMarriageSend:
		marriageSendCommand = true
		receipt, err = c.game.owners.ExecuteMarriageSendLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandDivorce:
		divorceCommand = true
		receipt, err = c.game.owners.ExecuteDivorceLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandVote:
		voteCommand = true
		receipt, err = c.game.owners.ExecuteVoteLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.VoteOptions{Catalog: c.game.config.VoteCatalog})
	case session.CommandStatus:
		receipt, err = c.game.owners.ExecuteStatusLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandFollow:
		receipt, err = c.game.owners.ExecuteFollowLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandItems:
		receipt, err = c.game.owners.ExecuteItemsLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandSay:
		sayText, sayCommand = session.SayLineText(line)
		receipt, err = c.game.owners.ExecuteSayLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandYell:
		yellText, yellCommand = session.YellLineText(line)
		receipt, err = c.game.owners.ExecuteYellLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandBroadcast:
		broadcastCommand = true
		c.game.mu.Lock()
		globalAt := c.game.lastPublicAdmissionAt
		c.game.mu.Unlock()
		broadcastOptions = world.BroadcastOptions{Now: now, LastAt: c.lastBroadcastAt, GlobalAt: globalAt}
		receipt, err = c.game.owners.ExecuteBroadcastLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, broadcastOptions)
	case session.CommandEmote:
		emote, emoteCommand = session.ParseEmoteLine(line)
		if !emoteCommand {
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		receipt, err = c.game.owners.ExecuteEmoteLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandExpress:
		expressText, expressCommand = session.ExpressLineText(line)
		if !expressCommand {
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		receipt, err = c.game.owners.ExecuteExpressLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandLookAtTarget:
		lookAtTarget, lookAtTargetCommand = session.ParseLookAtTargetLine(line)
		if !lookAtTargetCommand {
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		receipt, err = c.game.owners.ExecuteLookAtTargetLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandSearch:
		searchCommand = true
		receipt, err = c.game.owners.ExecuteSearchLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.SearchOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandTrack:
		trackCommand = true
		receipt, err = c.game.owners.ExecuteTrackLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.TrackOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandHide:
		hideCommand = true
		receipt, err = c.game.owners.ExecuteHideLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.HideOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandBribe:
		bribeCommand = true
		receipt, err = c.game.owners.ExecuteBribeLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandFlee:
		fleeCommand = true
		receipt, err = c.game.owners.ExecuteFleeLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.FleeOptions{Now: now, Hour: hour, Roll: c.game.config.Roll, Catalog: c.game.config.Catalog, Allocate: c.game.config.Allocate})
	case session.CommandPeek:
		peekCommand = true
		receipt, err = c.game.owners.ExecutePeekLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.PeekOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandSettings:
		settingsCommand = true
		receipt, err = c.game.owners.ExecuteSettingsLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandDoor:
		doorCommand = true
		receipt, err = c.game.owners.ExecuteDoorLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, now)
	case session.CommandDoorKey:
		doorKeyCommand = true
		receipt, err = c.game.owners.ExecuteDoorKeyLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.DoorKeyOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandWelcome:
		receipt, err = c.game.owners.ExecuteWelcomeLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.HelpFS)
	case session.CommandSocial:
		receipt, err = c.game.owners.ExecuteSocialLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandItemMutation:
		receipt, err = c.game.owners.ExecuteItemMutationLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandEquipment:
		receipt, err = c.game.owners.ExecuteEquipmentLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandBank:
		receipt, err = c.game.owners.ExecuteBankLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandShopList:
		shopListCommand = true
		receipt, err = c.game.owners.ExecuteShopLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandShopSell:
		shopSellCommand = true
		receipt, err = c.game.owners.ExecuteShopLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandShopPurchase:
		shopPurchaseCommand = true
		err = c.game.owners.RunGame(c.lease, func() error {
			var runErr error
			receipt, runErr = c.game.runShopPurchaseByNameLocked(ctx, commandID, c.lease.ActorID, line)
			return runErr
		})
	case session.CommandTrade:
		tradeCommand = true
		receipt, err = c.game.owners.ExecuteTradeLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandValue:
		valueCommand = true
		receipt, err = c.game.owners.ExecuteValueLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandRepair:
		repairCommand = true
		receipt, err = c.game.owners.ExecuteRepairLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.RepairOptions{Roll: c.game.config.Roll})
	case session.CommandDirectMessage:
		directMessageCommand = true
		receipt, err = c.game.owners.ExecuteDirectMessageLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandReply:
		replyCommand = true
		receipt, err = c.game.owners.ExecuteReplyLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, replyTargetID, replyTargetName)
	case session.CommandMerchantPurchase:
		merchantPurchaseCommand = true
		receipt, err = c.game.owners.ExecuteMerchantPurchaseLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.MerchantPurchaseOptions{Offers: c.game.config.MerchantOffers})
	case session.CommandNPCTalk:
		npcTalkCommand = true
		receipt, err = c.game.owners.ExecuteNPCTalkLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.NPCTalkOptions{Catalog: c.game.config.TalkCatalog, Now: now, Roll: c.game.config.Roll, ObjectCatalog: c.game.config.Catalog, Allocate: c.game.config.Allocate})
	case session.CommandGroupTalk:
		groupTalkCommand = true
		receipt, err = c.game.owners.ExecuteGroupTalkLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandDescription:
		descriptionCommand = true
		receipt, err = c.game.owners.ExecuteDescriptionLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandPlayerLookup:
		playerLookupCommand = true
		receipt, err = c.game.owners.ExecutePlayerLookupLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandReturnSquare:
		returnSquareCommand = true
		receipt, err = c.game.owners.ExecuteReturnSquareLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandCompare:
		compareCommand = true
		receipt, err = c.game.owners.ExecuteCompareLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandObjectAppraisal:
		objectAppraisalCommand = true
		receipt, err = c.game.owners.ExecuteObjectAppraisalLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandItemRename:
		itemRenameCommand = true
		receipt, err = c.game.owners.ExecuteItemRenameLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandRangerPray:
		rangerPrayCommand = true
		receipt, err = c.game.owners.ExecuteRangerPrayLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.RangerPrayOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandPrepare:
		prepareCommand = true
		receipt, err = c.game.owners.ExecutePrepareLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, now)
	case session.CommandUpDmg:
		upDmgCommand = true
		receipt, err = c.game.owners.ExecuteUpDmgLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.UpDmgOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandPowerAccuracy:
		if _, ok := session.ParsePowerAccuracyLine(line); !ok {
			return "아직 구현되지 않은 명령입니다.\r\n", nil
		}
		powerAccuracyCommand = true
		receipt, err = c.game.owners.ExecutePowerAccuracyLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.PowerAccuracyOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandMeditate:
		meditateCommand = true
		receipt, err = c.game.owners.ExecuteMeditateLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.MeditateOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandAlias:
		aliasCommand = true
		receipt, err = c.game.owners.ExecuteAliasLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandBurn:
		burnCommand = true
		receipt, err = c.game.owners.ExecuteBurnLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.BurnOptions{Now: now, Roll: c.game.config.Roll})
	case session.CommandStudy:
		studyCommand = true
		receipt, err = c.game.owners.ExecuteStudyLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandSave:
		saveCommand = true
		receipt, err = c.game.owners.ExecuteSaveLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandMail:
		mailCommand = true
		receipt, err = c.game.owners.ExecuteMailLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandMemo:
		memoCommand = true
		receipt, err = c.game.owners.ExecuteMemoLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandNotepad:
		notepadCommand = true
		receipt, err = c.game.owners.ExecuteNotepadLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandBoard:
		boardCommand = true
		receipt, err = c.game.owners.ExecuteBoardLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandTitle:
		titleCommand = true
		receipt, err = c.game.owners.ExecuteTitleLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandRead:
		timeCommand = true
		// The connector's clock callback exposes both a monotonic-ish tick and
		// the already-projected in-game hour.  The legacy command contract uses
		// the latter (including its 0=>12 display rule), so bind that value into
		// the durable time receipt rather than re-projecting `now` here.
		receipt, err = c.game.owners.ExecuteTimeLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.TimeCommandOptions{Now: int64(hour), WallClock: c.game.config.WallClock()})
	case session.CommandInfo:
		infoCommand = true
		receipt, err = c.game.owners.ExecuteInfoLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandHelp:
		receipt, err = c.game.owners.ExecuteHelpLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.HelpFS)
	case session.CommandQuit:
		receipt, err = c.game.owners.ExecuteQuitLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	default:
		return "아직 구현되지 않은 명령입니다.\r\n", nil
	}
	if errors.Is(err, session.ErrReplyTargetUnavailable) {
		return "누구에게 말을 전하시려구요?\r\n", nil
	}
	if errors.Is(err, session.ErrDirectionalDestinationUnresolved) {
		return session.DirectionalMapMissingResponse, nil
	}
	if errors.Is(err, world.ErrGoDestinationUnresolved) {
		return world.GoMapMissingResponse, nil
	}
	// list_charm's prompt and find_who miss are C-shaped non-durable
	// boundaries. They intentionally do not create a world receipt.
	if errors.Is(err, world.ErrDMCharmTargetRequired) {
		return world.DMCharmPromptResponse, nil
	}
	if errors.Is(err, world.ErrDMCharmTargetAbsent) {
		if command, ok := session.ParseDMCharmLine(line); ok && command.Target != "" {
			return fmt.Sprintf("%s은 없습니다.\n", command.Target), nil
		}
		return "아직 구현되지 않은 명령입니다.\r\n", nil
	}
	if errors.Is(err, session.ErrUnsupportedLookLine) ||
		errors.Is(err, session.ErrUnsupportedDirectionalLine) ||
		errors.Is(err, session.ErrUnsupportedGoLine) ||
		errors.Is(err, session.ErrUnsupportedAttackLine) ||
		errors.Is(err, session.ErrUnsupportedStatusLine) ||
		errors.Is(err, session.ErrUnsupportedFollowLine) ||
		errors.Is(err, session.ErrUnsupportedItemsLine) ||
		errors.Is(err, session.ErrUnsupportedSayLine) ||
		errors.Is(err, session.ErrUnsupportedSocialLine) ||
		errors.Is(err, session.ErrUnsupportedItemMutationLine) ||
		errors.Is(err, session.ErrUnsupportedEquipmentLine) ||
		errors.Is(err, session.ErrUnsupportedBankLine) ||
		errors.Is(err, session.ErrUnsupportedShopLine) ||
		errors.Is(err, session.ErrUnsupportedShopPurchaseLine) ||
		errors.Is(err, session.ErrUnsupportedTradeLine) ||
		errors.Is(err, world.ErrTradeActorAbsent) ||
		errors.Is(err, world.ErrTradeItemsUnmigrated) ||
		errors.Is(err, world.ErrTradeOffersUnmigrated) ||
		errors.Is(err, world.ErrTradeNPCUnresolved) ||
		errors.Is(err, world.ErrTradeStaleProposal) ||
		errors.Is(err, world.ErrTradeInvalidProposal) ||
		errors.Is(err, world.ErrTradeInvalidOccurrence) ||
		errors.Is(err, world.ErrTradeRewardAllocator) ||
		errors.Is(err, session.ErrUnsupportedValueLine) ||
		errors.Is(err, session.ErrUnsupportedRepairLine) ||
		errors.Is(err, session.ErrUnsupportedDirectMessageLine) ||
		errors.Is(err, session.ErrUnsupportedReplyLine) ||
		errors.Is(err, session.ErrUnsupportedStealLine) ||
		errors.Is(err, session.ErrUnsupportedTeachLine) ||
		errors.Is(err, session.ErrUnsupportedBackstabLine) ||
		errors.Is(err, session.ErrUnsupportedDrinkLine) ||
		errors.Is(err, session.ErrUnsupportedCircleLine) ||
		errors.Is(err, session.ErrUnsupportedBashLine) ||
		errors.Is(err, session.ErrUnsupportedMagicStopLine) ||
		errors.Is(err, session.ErrUnsupportedPoisonLine) ||
		errors.Is(err, session.ErrUnsupportedGiveLine) ||
		errors.Is(err, session.ErrUnsupportedEnemyStatusLine) ||
		errors.Is(err, session.ErrUnsupportedTimeLine) ||
		errors.Is(err, session.ErrUnsupportedSelectionLine) ||
		errors.Is(err, session.ErrUnsupportedTrainingLine) ||
		errors.Is(err, session.ErrUnsupportedTurnLine) ||
		errors.Is(err, session.ErrUnsupportedAbsorbLine) ||
		errors.Is(err, session.ErrUnsupportedKickLine) ||
		errors.Is(err, session.ErrUnsupportedUseLine) ||
		errors.Is(err, session.ErrUnsupportedChangeClassLine) ||
		errors.Is(err, session.ErrUnsupportedReadScrollLine) ||
		errors.Is(err, session.ErrUnsupportedCastLine) ||
		errors.Is(err, session.ErrUnsupportedPropertyInviteLine) ||
		errors.Is(err, session.ErrUnsupportedFamilyLine) ||
		errors.Is(err, session.ErrUnsupportedFamilyTalkLine) ||
		errors.Is(err, session.ErrUnsupportedMerchantPurchaseLine) ||
		errors.Is(err, session.ErrUnsupportedNPCTalkLine) ||
		errors.Is(err, session.ErrUnsupportedGroupTalkLine) ||
		errors.Is(err, session.ErrUnsupportedDescriptionLine) ||
		errors.Is(err, session.ErrUnsupportedPlayerLookupLine) ||
		errors.Is(err, session.ErrUnsupportedReturnSquareLine) ||
		errors.Is(err, session.ErrUnsupportedCompareLine) ||
		errors.Is(err, session.ErrUnsupportedObjectAppraisalLine) ||
		errors.Is(err, session.ErrUnsupportedItemRenameLine) ||
		errors.Is(err, session.ErrUnsupportedRangerPrayLine) ||
		errors.Is(err, session.ErrUnsupportedPrepareLine) ||
		errors.Is(err, session.ErrUnsupportedUpDmgLine) ||
		errors.Is(err, session.ErrUnsupportedPowerAccuracyLine) ||
		errors.Is(err, session.ErrUnsupportedMeditateLine) ||
		errors.Is(err, session.ErrUnsupportedAliasLine) ||
		errors.Is(err, session.ErrUnsupportedBurnLine) ||
		errors.Is(err, session.ErrUnsupportedStudyLine) ||
		errors.Is(err, session.ErrUnsupportedSaveLine) ||
		errors.Is(err, session.ErrUnsupportedMailLine) ||
		errors.Is(err, session.ErrUnsupportedMemoLine) ||
		errors.Is(err, session.ErrUnsupportedNotepadLine) ||
		errors.Is(err, session.ErrNotepadAppendContinuationRequired) ||
		errors.Is(err, world.ErrMemoStateUnresolved) ||
		errors.Is(err, world.ErrMemoActorAbsent) ||
		errors.Is(err, world.ErrMemoTargetRequired) ||
		errors.Is(err, world.ErrMemoTargetUnavailable) ||
		errors.Is(err, world.ErrMemoTargetAmbiguous) ||
		errors.Is(err, world.ErrMemoTargetOffline) ||
		errors.Is(err, world.ErrMemoBodyEmpty) ||
		errors.Is(err, world.ErrMemoBodyInvalidUTF8) ||
		errors.Is(err, world.ErrMemoBodyControl) ||
		errors.Is(err, world.ErrMemoBodyTooLong) ||
		errors.Is(err, world.ErrMemoInvalidID) ||
		errors.Is(err, world.ErrMemoInvalidTimestamp) ||
		errors.Is(err, world.ErrMemoInvalidRecord) ||
		errors.Is(err, world.ErrMemoLimit) ||
		errors.Is(err, world.ErrMemoStaleProposal) ||
		errors.Is(err, world.ErrMemoInvalidProposal) ||
		errors.Is(err, world.ErrMemoTargetNameNonCanonical) ||
		errors.Is(err, world.ErrNotepadStateUnresolved) ||
		errors.Is(err, world.ErrNotepadStateInvalid) ||
		errors.Is(err, world.ErrNotepadActorAbsent) ||
		errors.Is(err, world.ErrNotepadUnauthorized) ||
		errors.Is(err, world.ErrNotepadInvalidVerb) ||
		errors.Is(err, world.ErrNotepadInvalidOption) ||
		errors.Is(err, world.ErrNotepadAppendContinuation) ||
		errors.Is(err, world.ErrNotepadLineInvalid) ||
		errors.Is(err, world.ErrNotepadLineTooLong) ||
		errors.Is(err, world.ErrNotepadLimit) ||
		errors.Is(err, world.ErrNotepadStaleProposal) ||
		errors.Is(err, world.ErrNotepadInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedBoardLine) ||
		errors.Is(err, session.ErrUnsupportedTitleLine) ||
		errors.Is(err, session.ErrUnsupportedReadLine) ||
		errors.Is(err, session.ErrUnsupportedInfoLine) ||
		errors.Is(err, session.ErrUnsupportedHelpLine) ||
		errors.Is(err, session.ErrUnsupportedYellLine) ||
		errors.Is(err, session.ErrUnsupportedBroadcastLine) ||
		errors.Is(err, session.ErrUnsupportedEmoteLine) ||
		errors.Is(err, session.ErrUnsupportedExpressLine) ||
		errors.Is(err, session.ErrUnsupportedLookAtTargetLine) ||
		errors.Is(err, session.ErrUnsupportedSearchLine) ||
		errors.Is(err, session.ErrUnsupportedTrackLine) ||
		errors.Is(err, session.ErrUnsupportedHideLine) ||
		errors.Is(err, session.ErrUnsupportedBribeLine) ||
		errors.Is(err, session.ErrUnsupportedFleeLine) ||
		errors.Is(err, session.ErrUnsupportedPeekLine) ||
		errors.Is(err, session.ErrUnsupportedSettingsLine) ||
		errors.Is(err, session.ErrUnsupportedDoorLine) ||
		errors.Is(err, session.ErrUnsupportedDoorKeyLine) ||
		errors.Is(err, session.ErrUnsupportedWelcomeLine) ||
		errors.Is(err, session.ErrUnsupportedQuitLine) ||
		// Bounded combat/spell reducers fail closed when a canonical death,
		// relation, or combat continuation is not yet admitted. Treat those
		// domain errors as an unsupported command response so a user socket is
		// not sealed merely because a feature is intentionally gated.
		errors.Is(err, world.ErrCircleDeathTransitionPending) ||
		errors.Is(err, world.ErrCircleCharmStateUnresolved) ||
		errors.Is(err, world.ErrCircleFamilyWarUnresolved) ||
		errors.Is(err, world.ErrBashDeathTransitionPending) ||
		errors.Is(err, world.ErrBashCharmStateUnresolved) ||
		errors.Is(err, world.ErrBashWarStateUnresolved) ||
		errors.Is(err, world.ErrMagicStopCombatSideEffectPending) ||
		errors.Is(err, world.ErrMagicStopNPCStateUnresolved) ||
		errors.Is(err, world.ErrPoisonCombatSideEffectPending) ||
		errors.Is(err, world.ErrPoisonDeathTransitionPending) ||
		errors.Is(err, world.ErrPoisonNPCStateUnresolved) ||
		errors.Is(err, world.ErrGiveActorAbsent) ||
		errors.Is(err, world.ErrGiveRoomMembership) ||
		errors.Is(err, world.ErrGiveInventoryPending) ||
		errors.Is(err, world.ErrGiveItemAbsent) ||
		errors.Is(err, world.ErrGiveTargetAbsent) ||
		errors.Is(err, world.ErrGiveSelf) ||
		errors.Is(err, world.ErrGiveNPCPending) ||
		errors.Is(err, world.ErrGiveNPCIdentityPending) ||
		errors.Is(err, world.ErrGiveAmountInvalid) ||
		errors.Is(err, world.ErrGiveInsufficientGold) ||
		errors.Is(err, world.ErrGiveGoldOverflow) ||
		errors.Is(err, world.ErrGiveCapacity) ||
		errors.Is(err, world.ErrGiveProtectedPending) ||
		errors.Is(err, world.ErrGiveQuestPending) ||
		errors.Is(err, world.ErrGiveEventPending) ||
		errors.Is(err, world.ErrGiveNestedProtectedPending) ||
		errors.Is(err, world.ErrEnemyStatusCanonicalOnly) ||
		errors.Is(err, world.ErrEnemyStatusTargetRequired) ||
		errors.Is(err, world.ErrEnemyStatusTargetAbsent) ||
		errors.Is(err, world.ErrEnemyStatusVitalsUnavailable) ||
		errors.Is(err, world.ErrSelectionNPCStateUnresolved) ||
		errors.Is(err, world.ErrSelectionMerchantOffersUnresolved) ||
		errors.Is(err, world.ErrSelectionMerchantOffersInvalid) ||
		errors.Is(err, world.ErrSelectionStaleProposal) ||
		errors.Is(err, world.ErrTrainingActorAbsent) ||
		errors.Is(err, world.ErrTrainingRoomAbsent) ||
		errors.Is(err, world.ErrTrainingRoom) ||
		errors.Is(err, world.ErrTrainingClass) ||
		errors.Is(err, world.ErrTrainingBlind) ||
		errors.Is(err, world.ErrTrainingCaretaker) ||
		errors.Is(err, world.ErrTrainingExperience) ||
		errors.Is(err, world.ErrTrainingGold) ||
		errors.Is(err, world.ErrTrainingFamilyPending) ||
		errors.Is(err, world.ErrTrainingBroadcastPending) ||
		errors.Is(err, world.ErrTrainingNumeric) ||
		errors.Is(err, world.ErrTrainingStaleProposal) ||
		errors.Is(err, world.ErrTrainingUnsupportedClass) ||
		errors.Is(err, world.ErrTrainingUnsupportedLevel) ||
		errors.Is(err, world.ErrTurnCombatSideEffectPending) ||
		errors.Is(err, world.ErrTurnDeathTransitionPending) ||
		errors.Is(err, world.ErrTurnNPCStateUnresolved) ||
		errors.Is(err, world.ErrAbsorbNPCStateUnresolved) ||
		errors.Is(err, world.ErrAbsorbCombatSideEffectPending) ||
		errors.Is(err, world.ErrAbsorbDeathTransitionPending) ||
		errors.Is(err, world.ErrAbsorbHPOverflow) ||
		errors.Is(err, world.ErrKickDeathTransitionPending) ||
		errors.Is(err, world.ErrKickCharmStateUnresolved) ||
		errors.Is(err, world.ErrKickWarStateUnresolved) ||
		errors.Is(err, world.ErrUseUnsupported) ||
		errors.Is(err, world.ErrUseFloor) ||
		errors.Is(err, world.ErrDrinkMissingItem) ||
		errors.Is(err, world.ErrDrinkNotPotion) ||
		errors.Is(err, world.ErrDrinkEmpty) ||
		errors.Is(err, world.ErrDrinkNoPotionRoom) ||
		errors.Is(err, world.ErrDrinkSurvivalRoom) ||
		errors.Is(err, world.ErrDrinkClass) ||
		errors.Is(err, world.ErrDrinkSpellUnavailable) ||
		errors.Is(err, world.ErrDrinkSpecial) ||
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
		errors.Is(err, world.ErrChangeClassConfirmation) ||
		errors.Is(err, world.ErrReadScrollBlind) ||
		errors.Is(err, world.ErrReadScrollMissingItem) ||
		errors.Is(err, world.ErrReadScrollNotScroll) ||
		errors.Is(err, world.ErrReadScrollEmpty) ||
		errors.Is(err, world.ErrReadScrollLevel) ||
		errors.Is(err, world.ErrReadScrollAlignment) ||
		errors.Is(err, world.ErrReadScrollClass) ||
		errors.Is(err, world.ErrReadScrollNoMagicRoom) ||
		errors.Is(err, world.ErrReadScrollCooldown) ||
		errors.Is(err, world.ErrReadScrollUnsupported) ||
		errors.Is(err, world.ErrReadScrollSpellUnavailable) ||
		errors.Is(err, world.ErrReadScrollRandom) ||
		errors.Is(err, world.ErrCastActorAbsent) ||
		errors.Is(err, world.ErrCastSpellUnavailable) ||
		errors.Is(err, world.ErrCastSpellAmbiguous) ||
		errors.Is(err, world.ErrCastRandom) ||
		errors.Is(err, world.ErrCastStaleProposal) ||
		errors.Is(err, world.ErrCastInvalidProposal) ||
		errors.Is(err, world.ErrPropertyInvitationsUnmigrated) ||
		errors.Is(err, world.ErrPropertyInviteActorAbsent) ||
		errors.Is(err, world.ErrPropertyInviteNotHome) ||
		errors.Is(err, world.ErrPropertyInviteNoProperty) ||
		errors.Is(err, world.ErrPropertyInviteNameRequired) ||
		errors.Is(err, world.ErrPropertyInviteNameInvalid) ||
		errors.Is(err, world.ErrPropertyInviteNameTooLong) ||
		errors.Is(err, world.ErrPropertyInviteTargetMissing) ||
		errors.Is(err, world.ErrPropertyInviteTargetAmbiguous) ||
		errors.Is(err, world.ErrPropertyInviteTargetInvisible) ||
		errors.Is(err, world.ErrPropertyInviteSelf) ||
		errors.Is(err, world.ErrPropertyInviteLimit) ||
		errors.Is(err, world.ErrPropertyInviteInvalidProposal) ||
		errors.Is(err, world.ErrPropertyInviteStaleProposal) ||
		errors.Is(err, world.ErrFamilyCatalogUnavailable) ||
		errors.Is(err, world.ErrFamilyCatalogInvalid) ||
		errors.Is(err, world.ErrFamilyActorAbsent) ||
		errors.Is(err, world.ErrFamilyTargetRequired) ||
		errors.Is(err, world.ErrFamilyTargetUnavailable) ||
		errors.Is(err, world.ErrFamilyIdentityUnresolved) ||
		errors.Is(err, world.ErrFamilyTalkActorAbsent) ||
		errors.Is(err, world.ErrFamilyTalkNotMember) ||
		errors.Is(err, world.ErrFamilyTalkSilent) ||
		errors.Is(err, world.ErrFamilyTalkMessageEmpty) ||
		errors.Is(err, world.ErrFamilyTalkMessageInvalid) ||
		errors.Is(err, world.ErrFamilyMutationInvalidAction) ||
		errors.Is(err, world.ErrFamilyMutationActorAbsent) ||
		errors.Is(err, world.ErrFamilyMutationIdentityUnresolved) ||
		errors.Is(err, world.ErrFamilyMutationStateInvalid) ||
		errors.Is(err, world.ErrFamilyMutationFamilyRequired) ||
		errors.Is(err, world.ErrFamilyMutationFamilyUnavailable) ||
		errors.Is(err, world.ErrFamilyMutationBossUnavailable) ||
		errors.Is(err, world.ErrFamilyMutationBossAmbiguous) ||
		errors.Is(err, world.ErrFamilyMutationAlreadyMember) ||
		errors.Is(err, world.ErrFamilyMutationAlreadyPending) ||
		errors.Is(err, world.ErrFamilyMutationNotPending) ||
		errors.Is(err, world.ErrFamilyMutationBossCannotWithdraw) ||
		errors.Is(err, world.ErrFamilyMutationFeeUnavailable) ||
		errors.Is(err, world.ErrFamilyMutationApprovalUnsupported) ||
		errors.Is(err, world.ErrFamilyMutationNotBoss) ||
		errors.Is(err, world.ErrFamilyMutationTargetUnavailable) ||
		errors.Is(err, world.ErrFamilyMutationTargetAmbiguous) ||
		errors.Is(err, world.ErrFamilyMutationTargetNotPending) ||
		errors.Is(err, world.ErrFamilyMutationTargetAlreadyMember) ||
		errors.Is(err, world.ErrFamilyMutationTargetNotMember) ||
		errors.Is(err, world.ErrFamilyMutationTargetSelf) ||
		errors.Is(err, world.ErrFamilyMutationMemberLedgerMissing) ||
		errors.Is(err, world.ErrFamilyMutationInsufficientGold) ||
		errors.Is(err, world.ErrFamilyMutationGoldOverflow) ||
		errors.Is(err, world.ErrFamilyStateUnresolved) ||
		errors.Is(err, world.ErrFamilyStateInvalid) ||
		errors.Is(err, world.ErrFamilyMemberInvalid) ||
		errors.Is(err, world.ErrFamilyMemberAbsent) ||
		errors.Is(err, world.ErrFamilyMemberDuplicate) ||
		errors.Is(err, world.ErrFamilyMutationStaleProposal) ||
		errors.Is(err, world.ErrFamilyMutationInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedFamilyNewsLine) ||
		errors.Is(err, session.ErrFamilyNewsAppendContinuationRequired) ||
		errors.Is(err, world.ErrFamilyNewsUnresolved) ||
		errors.Is(err, world.ErrFamilyNewsInvalid) ||
		errors.Is(err, world.ErrFamilyNewsActorAbsent) ||
		errors.Is(err, world.ErrFamilyNewsIdentityUnresolved) ||
		errors.Is(err, world.ErrFamilyNewsStateInvalid) ||
		errors.Is(err, world.ErrFamilyNewsLineInvalid) ||
		errors.Is(err, world.ErrFamilyNewsLimit) ||
		errors.Is(err, world.ErrFamilyNewsStaleProposal) ||
		errors.Is(err, world.ErrFamilyNewsInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedFamilyWarLine) ||
		errors.Is(err, world.ErrFamilyWarUnresolved) ||
		errors.Is(err, world.ErrFamilyWarActorAbsent) ||
		errors.Is(err, world.ErrFamilyWarIdentityUnresolved) ||
		errors.Is(err, world.ErrFamilyWarStateInvalid) ||
		errors.Is(err, world.ErrFamilyWarStaleProposal) ||
		errors.Is(err, world.ErrFamilyWarInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedDMFamilyLine) ||
		errors.Is(err, world.ErrDMFamilyActorAbsent) ||
		errors.Is(err, world.ErrDMFamilyCatalog) ||
		errors.Is(err, world.ErrDMFamilyRandom) ||
		errors.Is(err, world.ErrDMFamilyAllocator) ||
		errors.Is(err, world.ErrDMFamilyFloorUnresolved) ||
		errors.Is(err, world.ErrDMFamilyNPCUnresolved) ||
		errors.Is(err, world.ErrDMFamilyRoomExhausted) ||
		errors.Is(err, world.ErrDMFamilyStaleProposal) ||
		errors.Is(err, world.ErrDMFamilyInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedDMFollowLine) ||
		errors.Is(err, world.ErrDMFollowActorAbsent) ||
		errors.Is(err, world.ErrDMFollowNPCUnresolved) ||
		errors.Is(err, world.ErrDMFollowInvalidVerb) ||
		errors.Is(err, world.ErrDMFollowInvalidName) ||
		errors.Is(err, world.ErrDMFollowInvalidOccurrence) ||
		errors.Is(err, world.ErrDMFollowNotReciprocal) ||
		errors.Is(err, world.ErrDMFollowStaleProposal) ||
		errors.Is(err, world.ErrDMFollowInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedDMActiveLine) ||
		errors.Is(err, world.ErrDMActiveActorAbsent) ||
		errors.Is(err, world.ErrDMActiveStateInvalid) ||
		errors.Is(err, world.ErrDMActiveNPCUnresolved) ||
		errors.Is(err, world.ErrDMActiveIdentityUnresolved) ||
		errors.Is(err, world.ErrDMActiveInvalidVerb) ||
		errors.Is(err, world.ErrDMActiveStaleProposal) ||
		errors.Is(err, world.ErrDMActiveInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedDMEnemyLine) ||
		errors.Is(err, world.ErrDMEnemyActorAbsent) ||
		errors.Is(err, world.ErrDMEnemyStateInvalid) ||
		errors.Is(err, world.ErrDMEnemyNPCUnresolved) ||
		errors.Is(err, world.ErrDMEnemyIdentityUnresolved) ||
		errors.Is(err, world.ErrDMEnemyRelationsUnresolved) ||
		errors.Is(err, world.ErrDMEnemyTargetRequired) ||
		errors.Is(err, world.ErrDMEnemyTargetAbsent) ||
		errors.Is(err, world.ErrDMEnemyTargetNameInvalid) ||
		errors.Is(err, world.ErrDMEnemyInvalidOccurrence) ||
		errors.Is(err, world.ErrDMEnemyInvalidVerb) ||
		errors.Is(err, world.ErrDMEnemyStaleProposal) ||
		errors.Is(err, world.ErrDMEnemyInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedDMCharmLine) ||
		errors.Is(err, world.ErrDMCharmActorAbsent) ||
		errors.Is(err, world.ErrDMCharmStateInvalid) ||
		errors.Is(err, world.ErrDMCharmRelationsUnresolved) ||
		errors.Is(err, world.ErrDMCharmIdentityUnresolved) ||
		errors.Is(err, world.ErrDMCharmTargetRequired) ||
		errors.Is(err, world.ErrDMCharmTargetAbsent) ||
		errors.Is(err, world.ErrDMCharmTargetAmbiguous) ||
		errors.Is(err, world.ErrDMCharmTargetNameInvalid) ||
		errors.Is(err, world.ErrDMCharmInvalidVerb) ||
		errors.Is(err, world.ErrDMCharmStaleProposal) ||
		errors.Is(err, world.ErrDMCharmInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedMoonSetLine) ||
		errors.Is(err, world.ErrMoonSetActorAbsent) ||
		errors.Is(err, world.ErrMoonSetCanonicalInventoryNeeded) ||
		errors.Is(err, world.ErrMoonSetItemNameRequired) ||
		errors.Is(err, world.ErrMoonSetInvalidOccurrence) ||
		errors.Is(err, world.ErrMoonSetRoomAbsent) ||
		errors.Is(err, world.ErrMoonSetRoomNameInvalid) ||
		errors.Is(err, world.ErrMoonSetDescriptionTooLong) ||
		errors.Is(err, world.ErrMoonSetKeyTooLong) ||
		errors.Is(err, world.ErrMoonSetStaleProposal) ||
		errors.Is(err, world.ErrMoonSetInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedZapLine) ||
		errors.Is(err, world.ErrZapActorAbsent) ||
		errors.Is(err, world.ErrZapItemNameRequired) ||
		errors.Is(err, world.ErrZapInvalidOccurrence) ||
		errors.Is(err, world.ErrZapTargetNameRequired) ||
		errors.Is(err, world.ErrZapNPCUnresolved) ||
		errors.Is(err, world.ErrZapSpellUnavailable) ||
		errors.Is(err, world.ErrZapRandom) ||
		errors.Is(err, world.ErrZapRoomItemsRequired) ||
		errors.Is(err, world.ErrZapStaleProposal) ||
		errors.Is(err, world.ErrZapInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedForgeLine) ||
		errors.Is(err, world.ErrForgeActorAbsent) ||
		errors.Is(err, world.ErrForgeFlagsUnresolved) ||
		errors.Is(err, world.ErrForgeNameInvalid) ||
		errors.Is(err, world.ErrForgeStaleProposal) ||
		errors.Is(err, world.ErrForgeInvalidProposal) ||
		errors.Is(err, world.ErrForgeNotReading) ||
		errors.Is(err, world.ErrForgeCatalogUnmigrated) ||
		errors.Is(err, world.ErrForgeSelectArmInput) ||
		errors.Is(err, world.ErrForgeGoldObjectUnmigrated) ||
		errors.Is(err, session.ErrUnsupportedNewForgeLine) ||
		errors.Is(err, world.ErrNewForgeActorAbsent) ||
		errors.Is(err, world.ErrNewForgeFlagsUnresolved) ||
		errors.Is(err, world.ErrNewForgeNameInvalid) ||
		errors.Is(err, world.ErrNewForgeStaleProposal) ||
		errors.Is(err, world.ErrNewForgeInvalidProposal) ||
		errors.Is(err, world.ErrNewForgeNotReading) ||
		errors.Is(err, world.ErrNewForgeCatalogUnmigrated) ||
		errors.Is(err, world.ErrNewForgeSelectArmInput) ||
		errors.Is(err, session.ErrUnsupportedBuyStatesLine) ||
		errors.Is(err, world.ErrBuyStatesActorAbsent) ||
		errors.Is(err, world.ErrBuyStatesGoldUnresolved) ||
		errors.Is(err, world.ErrBuyStatesStatsUnresolved) ||
		errors.Is(err, world.ErrBuyStatesApplyPending) ||
		errors.Is(err, world.ErrBuyStatesInvalidStat) ||
		errors.Is(err, world.ErrBuyStatesStaleProposal) ||
		errors.Is(err, world.ErrBuyStatesInvalidProposal) ||
		errors.Is(err, world.ErrBuyStatesNameInvalid) ||
		errors.Is(err, session.ErrUnsupportedSuicideLine) ||
		errors.Is(err, world.ErrSuicideActorAbsent) ||
		errors.Is(err, world.ErrSuicideNameInvalid) ||
		errors.Is(err, world.ErrSuicideStaleProposal) ||
		errors.Is(err, world.ErrSuicideInvalidProposal) ||
		errors.Is(err, world.ErrMarriageActorAbsent) ||
		errors.Is(err, world.ErrMarriageNotWeddingHall) ||
		errors.Is(err, world.ErrMarriageActorTooYoung) ||
		errors.Is(err, world.ErrMarriageAlreadyMarried) ||
		errors.Is(err, world.ErrMarriageTargetRequired) ||
		errors.Is(err, world.ErrMarriageTargetUnavailable) ||
		errors.Is(err, world.ErrMarriageTargetAmbiguous) ||
		errors.Is(err, world.ErrMarriageTargetInvisible) ||
		errors.Is(err, world.ErrMarriageSameSex) ||
		errors.Is(err, world.ErrMarriageTargetTooYoung) ||
		errors.Is(err, world.ErrMarriageTargetMarried) ||
		errors.Is(err, world.ErrMarriageTargetPendingDifferent) ||
		errors.Is(err, world.ErrMarriageStateInvalid) ||
		errors.Is(err, world.ErrMarriageStaleProposal) ||
		errors.Is(err, world.ErrMarriageInvalidProposal) ||
		errors.Is(err, session.ErrUnsupportedMarriageLine) ||
		errors.Is(err, session.ErrUnsupportedMarriageSendLine) ||
		errors.Is(err, session.ErrUnsupportedDivorceLine) ||
		errors.Is(err, session.ErrUnsupportedVoteLine) ||
		errors.Is(err, world.ErrDivorceActorAbsent) ||
		errors.Is(err, world.ErrDivorceStateInvalid) ||
		errors.Is(err, world.ErrDivorceSpouseKeyInvalid) ||
		errors.Is(err, world.ErrDivorceSpouseUnavailable) ||
		errors.Is(err, world.ErrDivorceSpouseAmbiguous) ||
		errors.Is(err, world.ErrDivorcePlayerStoreUnavailable) ||
		errors.Is(err, world.ErrDivorceStaleProposal) ||
		errors.Is(err, world.ErrDivorceInvalidProposal) ||
		errors.Is(err, world.ErrMarriageSendActorAbsent) ||
		errors.Is(err, world.ErrMarriageSendNotMarried) ||
		errors.Is(err, world.ErrMarriageSendStateInvalid) ||
		errors.Is(err, world.ErrMarriageSendSpouseUnavailable) ||
		errors.Is(err, world.ErrMarriageSendSpouseAmbiguous) ||
		errors.Is(err, world.ErrMarriageSendMessageEmpty) ||
		errors.Is(err, world.ErrMarriageSendMessageInvalid) ||
		errors.Is(err, world.ErrMarriageSendPLECHOUnsupported) ||
		errors.Is(err, world.ErrMarriageSendDescriptorFormat) ||
		errors.Is(err, world.ErrMarriageSendStaleProposal) ||
		errors.Is(err, world.ErrMarriageSendInvalidProposal) ||
		errors.Is(err, world.ErrVoteCatalogUnavailable) ||
		errors.Is(err, world.ErrVoteCatalogInvalid) ||
		errors.Is(err, world.ErrVoteActorAbsent) ||
		errors.Is(err, world.ErrVoteAge) ||
		errors.Is(err, world.ErrVoteRoom) ||
		errors.Is(err, world.ErrVoteNumeric) ||
		errors.Is(err, world.ErrVoteStateUnresolved) ||
		errors.Is(err, world.ErrVoteStaleProposal) ||
		errors.Is(err, world.ErrVoteInvalidProposal) {
		return "아직 구현되지 않은 명령입니다.\r\n", nil
	}
	if err != nil {
		c.ready = false
		return "", err
	}
	if infoCommand {
		// Establish this only after the durable first-page receipt succeeds;
		// receipt replay also restores the prompt/continuation contract after a
		// lost response. The second page gets its own stable receipt ID when
		// the next line is submitted.
		c.infoPending = true
		c.infoContinuationCommandID = ""
	}
	if directional && !receipt.Replayed {
		if after, ok := c.game.snapshot(ctx); ok {
			if beforeOK {
				c.game.publishMovement(before, after, c.lease.ActorID, receipt.NPCChaseIDs)
			}
			if receipt.ArrivalTrapEvent != nil {
				c.game.publishArrivalTrap(after, *receipt.ArrivalTrapEvent)
			}
		}
	}
	if sayCommand && !receipt.Replayed {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishSay(after, c.lease.ActorID, sayText)
		}
	}
	if yellCommand && !receipt.Replayed {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishYell(after, c.lease.ActorID, yellText)
		}
	}
	if broadcastCommand && !receipt.Replayed {
		var result world.BroadcastResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishBroadcast(after, *result.Event)
			}
			// The descriptor cooldown changes only after the receipt commits. A
			// rejected or replayed line must not consume it.
			c.lastBroadcastAt = broadcastOptions.Now
		}
	}
	if emoteCommand && !receipt.Replayed {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishEmote(after, c.lease.ActorID, emote.Alias, emote.Target)
		}
	}
	if expressCommand && !receipt.Replayed {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishExpress(after, c.lease.ActorID, expressText)
		}
	}
	if parsed.Kind == session.CommandLook && !receipt.Replayed {
		if command, ok := session.ParseLookLine(line); ok {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishLookInspect(after, c.lease.ActorID, command.Target, command.Occurrence, hour)
			}
		}
	}
	if lookAtTargetCommand && !receipt.Replayed {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishLookAtTarget(after, c.lease.ActorID, lookAtTarget.Target, lookAtTarget.Occurrence)
		}
	}
	if searchCommand && !receipt.Replayed {
		var result world.SearchResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishSearch(after, c.lease.ActorID, result.Targets)
			}
		}
	}
	if trackCommand && !receipt.Replayed {
		var result world.TrackResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishTrack(after, c.lease.ActorID)
			}
		}
	}
	if hideCommand && !receipt.Replayed {
		var result world.HideResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishHide(after, c.lease.ActorID, result.Succeeded)
			}
		}
	}
	if bribeCommand && !receipt.Replayed {
		var result world.BribeResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishBribe(after, *result.Event)
			}
		}
	}
	if fleeCommand && !receipt.Replayed {
		var result world.FleeResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishFlee(after, result)
				if result.Death != nil {
					c.game.publishFamilyDefeat(after, result.Death.Events)
				}
			}
		}
	}
	if peekCommand && !receipt.Replayed {
		var result world.PeekResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Alert {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishPeek(after, c.lease.ActorID, result)
			}
		}
	}
	if doorCommand && !receipt.Replayed {
		var result world.DoorCommandResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishDoor(after, c.lease.ActorID, result)
			}
		}
	}
	if doorKeyCommand && !receipt.Replayed {
		var result world.DoorKeyCommandResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishDoorKey(after, c.lease.ActorID, result)
			}
		}
	}
	if tradeCommand && !receipt.Replayed {
		var result world.NPCTradeResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishTrade(after, c.lease.ActorID, result)
			}
		}
	}
	if (directMessageCommand || replyCommand) && !receipt.Replayed {
		var result world.DirectMessageResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishDirectMessage(after, *result.Event)
			}
		}
	}
	if stealCommand && !receipt.Replayed {
		var result world.StealResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishSteal(after, *result.Event)
			}
		}
	}
	if teachCommand && !receipt.Replayed {
		var result world.TeachResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishTeach(after, *result.Event)
			}
		}
	}
	if backstabCommand && !receipt.Replayed {
		var result world.BackstabResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishBackstab(after, *result.Event)
			}
		}
	}
	if drinkCommand && !receipt.Replayed {
		var result world.DrinkResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishDrink(after, *result.Event)
			}
		}
	}
	if circleCommand && !receipt.Replayed {
		var result world.CircleResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishCircle(after, *result.Event)
			}
		}
	}
	if bashCommand && !receipt.Replayed {
		var result world.BashResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishBash(after, *result.Event)
			}
		}
	}
	if magicStopCommand && !receipt.Replayed {
		var result world.MagicStopResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishMagicStop(after, *result.Event)
			}
		}
	}
	if poisonCommand && !receipt.Replayed {
		var result world.PoisonResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishPoison(after, *result.Event)
			}
		}
	}
	if turnCommand && !receipt.Replayed {
		var result world.TurnResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishTurn(after, *result.Event)
			}
		}
	}
	if absorbCommand && !receipt.Replayed {
		var result world.AbsorbResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishAbsorb(after, *result.Event)
			}
		}
	}
	if kickCommand && !receipt.Replayed {
		var result world.KickResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishKick(after, *result.Event)
			}
		}
	}
	if useCommand && !receipt.Replayed {
		var result world.UseResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishUse(after, *result.Event)
			}
		}
	}
	if readScrollCommand && !receipt.Replayed {
		var result world.ScrollResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				publishWorldRoomEvent(c.game, after, result.Event.RoomID, result.Event.ActorID, result.Event.ExcludeActorID, result.Event.Text)
			}
		}
	}
	if castCommand && !receipt.Replayed {
		var result world.CastResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil {
			if after, ok := c.game.snapshot(ctx); ok {
				if result.Broadcast && result.Event != nil {
					if world.IsRecallCastSpell(result.SpellName) {
						c.game.publishRecall(after, result)
					} else {
						publishWorldRoomEventExcludingTarget(c.game, after, result.Event.RoomID, result.Event.ActorID, result.Event.ExcludeActorID, result.Event.ExcludeTargetID, result.Event.Text)
					}
				}
				c.game.publishCastTarget(after, result)
			}
		}
	}
	if giveCommand && !receipt.Replayed {
		var result world.GiveResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishGive(after, *result.Event)
			}
		}
	}
	if npcTalkCommand && !receipt.Replayed {
		var result world.NPCTalkResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishNPCTalk(after, *result.Event)
			}
		}
	}
	if groupTalkCommand && !receipt.Replayed {
		var result world.GroupTalkResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishGroupTalk(after, result.Events)
			}
		}
	}
	if familyTalkCommand && !receipt.Replayed {
		var result world.FamilyTalkResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishFamilyTalk(after, result.Events)
			}
		}
	}
	if familyMutationCommand && !receipt.Replayed {
		var result world.FamilyMutationResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && len(result.Events) != 0 {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishFamilyMutation(after, result.Events)
			}
		}
	}
	if familyWarCommand && !receipt.Replayed {
		var result world.FamilyWarResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && len(result.Events) != 0 {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishFamilyWar(after, c.lease.ActorID, result.Events)
			}
		}
	}
	if dmFamilyCommand && !receipt.Replayed {
		var result world.DMFamilyResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && len(result.Events) != 0 {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishDMFamily(after, c.lease.ActorID, result.Events)
			}
		}
	}
	if moonSetCommand && !receipt.Replayed {
		var result world.MoonSetResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && len(result.Events) != 0 {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishMoonSet(after, result.Events)
			}
		}
	}
	if zapCommand && !receipt.Replayed {
		var result world.ZapResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && len(result.Events) != 0 {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishZap(after, result.Events)
			}
		}
	}
	if marriageCommand && !receipt.Replayed {
		var result world.MarriageResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishMarriage(after, result)
			}
		}
	}
	if divorceCommand && !receipt.Replayed {
		var result world.DivorceResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishDivorce(after, result)
			}
		}
	}
	if marriageSendCommand && !receipt.Replayed {
		var result world.MarriageSendResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishMarriageSend(after, *result.Event)
			}
		}
	}
	if returnSquareCommand && !receipt.Replayed {
		var result world.ReturnSquareResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Moved && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishReturnSquare(after, result)
			}
		}
	}
	if itemRenameCommand && !receipt.Replayed {
		var result world.ItemRenameResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Broadcast && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishItemRename(after, *result.Event)
			}
		}
	}
	if rangerPrayCommand && !receipt.Replayed {
		var result world.RangerPrayResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishRangerPray(after, *result.Event)
			}
		}
	}
	if prepareCommand && !receipt.Replayed {
		var result world.PrepareResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishPrepare(after, *result.Event)
			}
		}
	}
	if upDmgCommand && !receipt.Replayed {
		var result world.UpDmgResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishUpDmg(after, *result.Event)
			}
		}
	}
	if powerAccuracyCommand && !receipt.Replayed {
		var result world.PowerAccuracyResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishPowerAccuracy(after, *result.Event)
			}
		}
	}
	if meditateCommand && !receipt.Replayed {
		var result world.MeditateResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishMeditate(after, *result.Event)
			}
		}
	}
	if burnCommand && !receipt.Replayed {
		var result world.BurnResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishBurn(after, *result.Event)
			}
		}
	}
	if studyCommand && !receipt.Replayed {
		var result world.StudyResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil && result.Event != nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishStudy(after, *result.Event)
			}
		}
	}
	if session.IsQuitLine(line) {
		c.closeAfterSubmit = true
	}
	var output string
	if broadcastCommand {
		var result world.BroadcastResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if searchCommand {
		var result world.SearchResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if trackCommand {
		var result world.TrackResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if hideCommand {
		var result world.HideResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if bribeCommand {
		var result world.BribeResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if fleeCommand {
		var result world.FleeResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if peekCommand {
		var result world.PeekResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if shopListCommand {
		var result world.ShopListResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if shopSellCommand {
		var result world.ShopSaleResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if shopPurchaseCommand {
		var result world.ShopPurchaseResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = renderShopPurchaseOutput(result)
		}
	} else if tradeCommand {
		var result world.NPCTradeResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if valueCommand {
		var result world.ValueResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if repairCommand {
		var result world.RepairResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if directMessageCommand {
		var result world.DirectMessageResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if replyCommand {
		var result world.DirectMessageResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if stealCommand {
		var result world.StealResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if teachCommand {
		var result world.TeachResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if backstabCommand {
		var result world.BackstabResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if drinkCommand {
		var result world.DrinkResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if circleCommand {
		var result world.CircleResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if bashCommand {
		var result world.BashResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if magicStopCommand {
		var result world.MagicStopResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if poisonCommand {
		var result world.PoisonResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if turnCommand {
		var result world.TurnResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if absorbCommand {
		var result world.AbsorbResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if kickCommand {
		var result world.KickResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if useCommand {
		var result world.UseResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if changeClassCommand {
		var result world.ChangeClassResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if readScrollCommand {
		var result world.ScrollResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if castCommand {
		var result world.CastResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if propertyInviteCommand {
		var result world.PropertyInviteResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if familyCommand {
		if err = json.Unmarshal(receipt.Response, &output); err != nil {
			// Keep the original receipt error for a malformed response.
		}
	} else if giveCommand {
		var result world.GiveResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if enemyStatusCommand {
		if err = json.Unmarshal(receipt.Response, &output); err != nil {
			// Keep the original receipt error for a malformed response.
		}
	} else if timeCommand {
		if err = json.Unmarshal(receipt.Response, &output); err != nil {
			// Keep the original receipt error for a malformed response.
		}
	} else if selectionCommand {
		var result world.SelectionResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if trainingCommand {
		var result world.TrainingResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if merchantPurchaseCommand {
		var result world.MerchantPurchaseResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if npcTalkCommand {
		var result world.NPCTalkResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if groupTalkCommand {
		var result world.GroupTalkResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if familyTalkCommand {
		var result world.FamilyTalkResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if familyMutationCommand {
		var result world.FamilyMutationResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if familyNewsCommand {
		var result world.FamilyNewsResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if familyWarCommand {
		var result world.FamilyWarResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if dmFamilyCommand {
		var result world.DMFamilyResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if dmFollowCommand {
		var result world.DMFollowResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if dmActiveCommand {
		var result world.DMActiveResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if dmEnemyCommand {
		var result world.DMEnemyResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if dmCharmCommand {
		var result world.DMCharmResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if moonSetCommand {
		var result world.MoonSetResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if zapCommand {
		var result world.ZapResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if forgeCommand {
		var result world.ForgeResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
			// Restore the select_arm case-2 gate after a successful (or
			// replayed) weapon-type prompt so the next player line cannot
			// fall through the ordinary parser.
			if result.Action == world.ForgePrompt {
				c.forgeSelectArmPending = true
				c.forgeSelectArmCommandID = ""
				c.clearForgeMaterial()
				c.clearForgeQuench()
				c.clearForgeName()
				c.clearForgeConfirm()
			}
		}
	} else if newForgeCommand {
		var result world.NewForgeResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
			// Restore the select_newarm case-2 gate after a successful (or
			// replayed) weapon-type prompt so the next player line cannot
			// fall through the ordinary parser. This is not 제련's select_arm.
			if result.Action == world.NewForgePrompt {
				c.clearNewForgeFlow()
				c.newForgeSelectArmPending = true
				c.newForgeSelectArmCommandID = ""
			}
		}
	} else if buyStatesCommand {
		var result world.BuyStatesResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if suicideCommand {
		var result world.SuicideResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
			// Restore suicide case 2 after a successful (or replayed)
			// password prompt so the next player line cannot fall through
			// the ordinary parser.
			if result.Action == world.SuicidePrompt {
				c.suicidePasswordPending = true
				c.suicidePasswordCommandID = ""
				c.passwordSecret = true
			}
		}
	} else if marriageCommand {
		var result world.MarriageResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if marriageSendCommand {
		var result world.MarriageSendResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if divorceCommand {
		var result world.DivorceResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if voteCommand {
		var result world.VoteResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if descriptionCommand {
		var result world.DescriptionResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if playerLookupCommand {
		if err = json.Unmarshal(receipt.Response, &output); err != nil {
			// Keep the original receipt error for a malformed response.
		}
	} else if returnSquareCommand {
		var result world.ReturnSquareResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if compareCommand {
		var result world.CompareResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if objectAppraisalCommand {
		var result world.ObjectAppraisalResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if itemRenameCommand {
		var result world.ItemRenameResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if rangerPrayCommand {
		var result world.RangerPrayResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if prepareCommand {
		var result world.PrepareResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if upDmgCommand {
		var result world.UpDmgResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if powerAccuracyCommand {
		var result world.PowerAccuracyResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if meditateCommand {
		var result world.MeditateResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if aliasCommand {
		var result world.AliasResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if burnCommand {
		var result world.BurnResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if studyCommand {
		var result world.StudyResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if saveCommand {
		var result session.SaveResponse
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = string(result)
		}
	} else if mailCommand {
		var result world.MailResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if memoCommand {
		var result world.MemoResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if notepadCommand {
		var result world.NotepadResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if boardCommand {
		if err = json.Unmarshal(receipt.Response, &output); err != nil {
			// Keep the original receipt error for a malformed response.
		}
	} else if titleCommand {
		var result world.TitleResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if settingsCommand {
		var result world.SettingsResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if doorCommand {
		var result world.DoorCommandResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else if doorKeyCommand {
		var result world.DoorKeyCommandResult
		if err = json.Unmarshal(receipt.Response, &result); err == nil {
			output = result.Response
		}
	} else {
		err = json.Unmarshal(receipt.Response, &output)
	}
	return output, err
}
func (c *worldConnection) Close(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	// Disconnect discards any uncommitted editor input immediately. Durable
	// departure cleanup may need a retry, but an abandoned title/body must not
	// remain attached to that connection while it is sealed.
	c.clearVote()
	c.clearForgeFlow()
	c.clearNewForgeSelectArm()
	c.clearSuicidePassword()
	c.clearCompose()
	c.clearNotepad()
	// `first_ignore` is descriptor-local in the legacy server. Clear it at the
	// connection boundary so a closed descriptor cannot retain names while a
	// cleanup retry is pending.
	c.ignore.Clear()
	c.setReplyTarget("", "")
	if c.passwordChange != nil {
		// Cancel clears the state machine's expected/replacement hashes before
		// this connection can become a pending cleanup retry.
		c.passwordChange.Cancel()
		c.passwordChange = nil
	}
	c.passwordSecret = false
	before, beforeOK := c.game.snapshot(ctx)
	// Ownership remains reserved on every failure; background worker owns retries.
	if err := c.game.cleanup.Enqueue(c.lease); err != nil {
		return
	}
	if beforeOK {
		if player, ok := before.Players[c.lease.ActorID]; ok && player.Online && player.Body.Class <= 11 &&
			!world.PlayerFlagSet(player.Body, 10) && !world.PlayerFlagSet(player.Body, 62) {
			now, _ := c.game.config.Clock()
			c.game.mu.Lock()
			c.game.lastPublicAdmissionAt = now
			c.game.mu.Unlock()
		}
	}
	c.game.unregister(c)
	c.closed = true
	c.ready = false
	_ = c.game.cleanup.Retry(ctx, 5*time.Second)
}

// publishCastTarget delivers locate_player's private scrye notice to the
// exact online target. C print(crt_ptr->fd) is independent of the caster-room
// broadcast_rom, so this path does not require the target to share a room.
func (g *WorldConnector) publishCastTarget(after world.State, result world.CastResult) {
	if result.TargetID == "" || result.TargetText == "" {
		return
	}
	target, ok := after.Players[result.TargetID]
	if !ok || !target.Online {
		return
	}
	if result.TargetName != "" && target.Body.Name != result.TargetName {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		if connection.lease.ActorID != result.TargetID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- result.TargetText:
		default:
		}
	}
}

func (g *WorldConnector) snapshot(ctx context.Context) (world.State, bool) {
	snapshot, err := g.config.Store.LoadWorld(ctx, g.config.WorldID)
	if err != nil {
		return world.State{}, false
	}
	s, err := world.DecodeState(snapshot.State)
	if err != nil {
		return world.State{}, false
	}
	return s, true
}

func (g *WorldConnector) unregister(connection *worldConnection) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.connections[connection]; !exists {
		return
	}
	delete(g.connections, connection)
	if connection.events != nil {
		close(connection.events)
	}
}

var _ GameConnector = (*WorldConnector)(nil)
