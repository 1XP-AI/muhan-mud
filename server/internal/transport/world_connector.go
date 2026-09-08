package transport

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type WorldConnectorConfig struct {
	Store       engine.CommandStore
	WorldID     string
	Clock       func() (int32, int)
	WallClock   func() time.Time
	Catalog     world.SpawnCatalog
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
	mu            sync.Mutex
	commandMu     sync.Mutex
	tickMu        sync.Mutex
	config        WorldConnectorConfig
	owners        session.Ownership
	cleanup       *session.CleanupQueue
	connections   map[*worldConnection]struct{}
	stopping      bool
	lastVitalSlot int64
	pendingVital  *playerVitalTick
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
	if config.WallClock == nil {
		config.WallClock = func() time.Time { return time.Now().In(mudPST) }
	}
	g := &WorldConnector{config: config, connections: map[*worldConnection]struct{}{}, lastVitalSlot: -1}
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
	connection := &worldConnection{game: g, lease: lease, events: make(chan string, 32)}
	now, hour := g.config.Clock()
	receipt, err := g.owners.EnterWorld(ctx, g.config.Store, g.config.WorldID, "enter-"+rand.Text(), lease, now, world.SceneOptions{ViewOptions: world.ViewOptions{Hour: hour}}, g.config.Catalog, g.config.Roll, g.config.Allocate)
	if err != nil {
		return connection, "", err
	} // handler must Close even on Open error
	var entry world.RoomEntry
	if err = json.Unmarshal(receipt.Response, &entry); err != nil {
		return connection, "", err
	}
	connection.ready = true
	g.mu.Lock()
	g.connections[connection] = struct{}{}
	g.mu.Unlock()
	return connection, entry.Scene, nil
}

type worldConnection struct {
	mu               sync.Mutex
	game             *WorldConnector
	lease            session.SessionLease
	events           chan string
	ready, closed    bool
	closeAfterSubmit bool
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

func (c *worldConnection) Submit(ctx context.Context, line string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || !c.ready {
		return "", errors.New("world connection not ready")
	}
	c.game.commandMu.Lock()
	defer c.game.commandMu.Unlock()
	now, hour := c.game.config.Clock()
	before, beforeOK := c.game.snapshot(ctx)
	commandID := "command-" + rand.Text()
	parsed, parseErr := session.ParseCommand(line)
	if errors.Is(parseErr, session.ErrCommandTooManyTokens) {
		return "명령이 너무 깁니다.\r\n", nil
	}
	if parseErr != nil {
		return "명령을 이해할 수 없습니다.\r\n", nil
	}
	directional := parsed.Kind == session.CommandDirectional
	sayText, sayCommand := "", false
	var receipt storage.WorldReceipt
	var err error
	switch parsed.Kind {
	case session.CommandLook:
		receipt, err = c.game.owners.ExecuteLookLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, hour)
	case session.CommandDirectional:
		receipt, err = c.game.owners.ExecuteDirectionalLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, now, hour, world.SceneOptions{}, c.game.config.Catalog, c.game.config.Roll, c.game.config.Allocate)
	case session.CommandAttack:
		receipt, err = c.game.owners.ExecuteAttackLineWithOptions(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, c.game.config.Roll, session.AttackOptions{Now: now, Allocate: c.game.config.Allocate})
	case session.CommandStatus:
		receipt, err = c.game.owners.ExecuteStatusLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandFollow:
		receipt, err = c.game.owners.ExecuteFollowLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandItems:
		receipt, err = c.game.owners.ExecuteItemsLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandSay:
		sayText, sayCommand = session.SayLineText(line)
		receipt, err = c.game.owners.ExecuteSayLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandSocial:
		receipt, err = c.game.owners.ExecuteSocialLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandItemMutation:
		receipt, err = c.game.owners.ExecuteItemMutationLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandEquipment:
		receipt, err = c.game.owners.ExecuteEquipmentLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandBank:
		receipt, err = c.game.owners.ExecuteBankLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line)
	case session.CommandRead:
		receipt, err = c.game.owners.ExecuteReadLine(ctx, c.game.config.Store, c.game.config.WorldID, commandID, c.lease, line, session.ReadLineOptions{GameHour: hour, WallClock: c.game.config.WallClock()})
	case session.CommandInfo:
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
		errors.Is(err, session.ErrUnsupportedReadLine) ||
		errors.Is(err, session.ErrUnsupportedInfoLine) ||
		errors.Is(err, session.ErrUnsupportedHelpLine) ||
		errors.Is(err, session.ErrUnsupportedQuitLine) {
		return "아직 구현되지 않은 명령입니다.\r\n", nil
	}
	if err != nil {
		c.ready = false
		return "", err
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
	if session.IsQuitLine(line) {
		c.closeAfterSubmit = true
	}
	var output string
	err = json.Unmarshal(receipt.Response, &output)
	return output, err
}
func (c *worldConnection) Close(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	// Ownership remains reserved on every failure; background worker owns retries.
	if err := c.game.cleanup.Enqueue(c.lease); err != nil {
		return
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
