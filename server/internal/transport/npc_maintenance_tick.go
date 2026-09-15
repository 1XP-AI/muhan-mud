package transport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

// npcMaintenanceTick is the exact request retained while a maintenance
// command has an unknown durable outcome. Retrying must reuse all three
// values; resampling the clock could turn one cadence into two transitions.
type npcMaintenanceTick struct {
	slot      int64
	now       int32
	commandID string
}

type npcMaintenanceTickRequest struct {
	Kind string `json:"kind"`
	Slot int64  `json:"slot"`
	Now  int32  `json:"now"`
}

func npcMaintenanceIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("NPC maintenance interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("NPC maintenance interval must be at least one second")
	}
	return seconds, nil
}

func npcMaintenanceCommandID(slot int64) string {
	return fmt.Sprintf("npc-maintenance-%d", slot)
}

// RunNPCMaintenanceTick executes the bounded, pre-combat NPC maintenance
// phase for the next deterministic cadence slot. A failed durable attempt
// keeps the exact slot, timestamp, and command ID for a later retry.
func (g *WorldConnector) RunNPCMaintenanceTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	if g == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC maintenance connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC maintenance tick context")
	}
	seconds, err := npcMaintenanceIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	if g.config.Clock == nil {
		return storage.WorldReceipt{}, false, errors.New("NPC maintenance connector clock is unavailable")
	}
	now, _ := g.config.Clock()
	pending := g.pendingNPCMaintenance
	if pending == nil {
		slot := int64(now) / seconds
		if slot <= g.lastNPCMaintenanceSlot {
			return storage.WorldReceipt{}, false, nil
		}
		slotNow := slot * seconds
		if slotNow < -2147483648 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("NPC maintenance slot timestamp overflow")
		}
		pending = &npcMaintenanceTick{slot: slot, now: int32(slotNow), commandID: npcMaintenanceCommandID(slot)}
		g.pendingNPCMaintenance = pending
	}
	receipt, err := g.runNPCMaintenancePhaseAt(ctx, pending.commandID, pending.slot, pending.now)
	if err != nil {
		return storage.WorldReceipt{}, true, err
	}
	g.lastNPCMaintenanceSlot = pending.slot
	g.pendingNPCMaintenance = nil
	return receipt, true, nil
}

// RunNPCMaintenanceScheduler owns only this bounded maintenance phase on a
// wall-clock cadence. It is intentionally independent from combat ordering;
// process-level scheduler start wiring remains a separate host concern.
func (g *WorldConnector) RunNPCMaintenanceScheduler(ctx context.Context, interval time.Duration) error {
	if g == nil {
		return errors.New("nil NPC maintenance scheduler connector")
	}
	if ctx == nil {
		return errors.New("nil NPC maintenance scheduler context")
	}
	if _, err := npcMaintenanceIntervalSeconds(interval); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _, err := g.RunNPCMaintenanceTick(ctx, interval)
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

func (g *WorldConnector) runNPCMaintenancePhaseAt(ctx context.Context, commandID string, slot int64, now int32) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC maintenance connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC maintenance phase context")
	}
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing NPC maintenance phase command ID")
	}
	g.commandMu.Lock()
	defer g.commandMu.Unlock()
	return session.ExecuteNPCMaintenanceReceipt(ctx, g.config.Store, g.config.WorldID, commandID, session.NPCMaintenanceOptions{
		Slot: slot,
		Now:  now,
		Roll: g.config.Roll,
	})
}
