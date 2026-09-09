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
	// TalkCatalog is the immutable, server-owned command8.c topic catalog.
	// It is optional so no-topic NPC speech keeps its original behavior; an
	// MTALKS topic request fails closed when this dependency is absent.
	TalkCatalog *world.TalkCatalog
	// MerchantOffers is the server-owned MPURIT stock catalog. Legacy NPC
	// Carry values are never interpreted from a terminal request; an absent
	// catalog makes merchant purchase fail closed at the session boundary.
	MerchantOffers world.MerchantOffers
	Roll           func(int, int) int
	Allocate       func() (string, error)
	HelpFS         fs.FS
	MaxSessions    int
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
}

type playerPhaseSummary struct {
	Now          int32    `json:"now"`
	Hour         int      `json:"hour"`
	Actors       []string `json:"actors"`
	Messages     []string `json:"messages"`
	Deaths       int      `json:"deaths"`
	SaveDue      []string `json:"save_due"`
	Extinguished []string `json:"extinguished"`
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
	if config.WallClock == nil {
		config.WallClock = func() time.Time { return time.Now().In(mudPST) }
	}
	g := &WorldConnector{config: config, connections: map[*worldConnection]struct{}{}, lastVitalSlot: -1, lastRoomResourceSlot: -1, lastNPCResourceSlot: -1, lastNPCMaintenanceSlot: -1}
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
	defer g.commandMu.Unlock()
	return engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
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
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(summary)
		return state, response, err
	})
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
	lastCommand      string
	lastBroadcastAt  int32
	ready, closed    bool
	closeAfterSubmit bool
	infoPending      bool
	// infoContinuationCommandID binds the one [엔터] page to a durable
	// receipt. It remains stable across an uncertain commit/response so a
	// retry cannot render a newer snapshot or create a second page.
	infoContinuationCommandID string
	compose                   *composeDraft
	passwordChange            session.PasswordChanger
	passwordSecret            bool
	// ignore is command9.c's connection-local first_ignore list. It is never
	// serialized with the world snapshot or a command receipt.
	ignore IgnoreList
}

type composeKind uint8

const (
	composeMailSend composeKind = iota + 1
	composeBoardWrite
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
	c.compose.commandID = ""
	c.compose.boardID = 0
	c.compose.timestamp = time.Time{}
	c.compose.kind = 0
	c.compose = nil
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
	if output, handled, err := c.submitComposeLine(ctx, line); handled {
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
	directional := parsed.Kind == session.CommandDirectional
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
	propertyInviteCommand := false
	familyCommand := false
	familyTalkCommand := false
	familyMutationCommand := false
	marriageCommand := false
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
	case session.CommandMarriage:
		marriageCommand = true
		receipt, err = c.game.owners.ExecuteMarriageLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
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
	case session.CommandMerchantPurchase:
		merchantPurchaseCommand = true
		receipt, err = c.game.owners.ExecuteMerchantPurchaseLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.MerchantPurchaseOptions{Offers: c.game.config.MerchantOffers})
	case session.CommandNPCTalk:
		npcTalkCommand = true
		receipt, err = c.game.owners.ExecuteNPCTalkLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.NPCTalkOptions{Catalog: c.game.config.TalkCatalog})
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
	if errors.Is(err, session.ErrUnsupportedLookLine) ||
		errors.Is(err, session.ErrUnsupportedDirectionalLine) ||
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
		errors.Is(err, session.ErrUnsupportedValueLine) ||
		errors.Is(err, session.ErrUnsupportedRepairLine) ||
		errors.Is(err, session.ErrUnsupportedDirectMessageLine) ||
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
		errors.Is(err, world.ErrFamilyMutationStaleProposal) ||
		errors.Is(err, world.ErrFamilyMutationInvalidProposal) ||
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
		errors.Is(err, session.ErrUnsupportedMarriageLine) {
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
	if directional && !receipt.Replayed && beforeOK {
		if after, ok := c.game.snapshot(ctx); ok {
			c.game.publishMovement(before, after, c.lease.ActorID)
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
	if directMessageCommand && !receipt.Replayed {
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
	if marriageCommand && !receipt.Replayed {
		var result world.MarriageResult
		if decodeErr := json.Unmarshal(receipt.Response, &result); decodeErr == nil {
			if after, ok := c.game.snapshot(ctx); ok {
				c.game.publishMarriage(after, result)
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
	} else if marriageCommand {
		var result world.MarriageResult
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
	c.clearCompose()
	// `first_ignore` is descriptor-local in the legacy server. Clear it at the
	// connection boundary so a closed descriptor cannot retain names while a
	// cleanup retry is pending.
	c.ignore.Clear()
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
