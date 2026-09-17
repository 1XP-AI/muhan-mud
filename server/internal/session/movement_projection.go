package session

import (
	"encoding/json"
	"fmt"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// movementReceiptProjection is persisted in the command receipt's private
// projection column. Keeping it separate from Response preserves the public
// terminal response byte-for-byte.
type movementReceiptProjection struct {
	FollowerArrivalTrapEvents []world.ArrivalTrapEvent `json:"follower_arrival_trap_events,omitempty"`
}

func decodeMovementReceiptProjection(receipt *storage.WorldReceipt) error {
	if receipt == nil || receipt.ProjectionDelivered || len(receipt.Projection) == 0 {
		return nil
	}
	var projection movementReceiptProjection
	if err := json.Unmarshal(receipt.Projection, &projection); err != nil {
		return fmt.Errorf("decode movement receipt projection: %w", err)
	}
	if len(projection.FollowerArrivalTrapEvents) == 0 {
		return nil
	}
	receipt.FollowerArrivalTrapEvents = append([]world.ArrivalTrapEvent(nil), projection.FollowerArrivalTrapEvents...)
	return nil
}
