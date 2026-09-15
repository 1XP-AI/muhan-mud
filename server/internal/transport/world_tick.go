package transport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

// playerVitalTick is the exact input retained while a durable command has an
// unknown outcome. Retrying must reuse both the command ID and the timestamp
// that was placed in the request; recomputing either value could create a
// second state transition after a timeout.
type playerVitalTick struct {
	slot      int64
	now       int32
	hour      int
	commandID string
}

func playerVitalIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("player vital interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("player vital interval must be at least one second")
	}
	return seconds, nil
}

func playerVitalSlot(now int32, interval time.Duration) (int64, error) {
	seconds, err := playerVitalIntervalSeconds(interval)
	if err != nil {
		return 0, err
	}
	return int64(now) / seconds, nil
}

func playerVitalCommandID(slot int64) string {
	return fmt.Sprintf("player-vitals-%d", slot)
}

// RunPlayerVitalTick attempts the next deterministic player update slot. The
// bool is false when the current slot was already durably completed. A failed
// attempt leaves an exact pending request in memory so the next call retries
// the same command ID and request instead of creating a second transition.
func (g *WorldConnector) RunPlayerVitalTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil player tick context")
	}
	seconds, err := playerVitalIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	now, hour := g.config.Clock()
	pending := g.pendingVital
	if pending == nil {
		slot := int64(now) / seconds
		if slot <= g.lastVitalSlot {
			return storage.WorldReceipt{}, false, nil
		}
		// Bind the durable request to the slot boundary rather than the exact
		// wall-clock sample. A fresh connector in the same slot must derive the
		// same request and receive the existing receipt instead of conflicting
		// on a different timestamp after a restart.
		slotNow := slot * seconds
		if slotNow < -2147483648 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("player vital slot timestamp overflow")
		}
		pending = &playerVitalTick{slot: slot, now: int32(slotNow), hour: hour, commandID: playerVitalCommandID(slot)}
		g.pendingVital = pending
	}
	receipt, err := g.runPlayerVitalPhaseAt(ctx, pending.commandID, pending.now, pending.hour)
	if err != nil {
		return storage.WorldReceipt{}, true, err
	}
	g.lastVitalSlot = pending.slot
	g.pendingVital = nil
	return receipt, true, nil
}

// RunPlayerVitalScheduler owns the currently implemented player update phase
// on a wall-clock cadence. It exits cleanly on context cancellation. A
// transient database or writer error is intentionally not fatal: the pending
// slot is retained and the next cadence retries the same durable command.
// NPC AI, random room spawn, game-clock progression, and combat rounds remain
// separate scheduler slices and are not claimed by this method.
func (g *WorldConnector) RunPlayerVitalScheduler(ctx context.Context, interval time.Duration) error {
	if ctx == nil {
		return errors.New("nil player scheduler context")
	}
	if _, err := playerVitalIntervalSeconds(interval); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _, err := g.RunPlayerVitalTick(ctx, interval)
		if err != nil && ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
