package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// StartWorld is an explicit cold-start takeover. No sessions/ticks may run until
// it returns a confirmed writer. Keep one unique boot ID across uncertain startup
// retries; never call this to recover an ordinary command error or reconnect.
// A failure returns no usable writer, even if the claim already committed.
func StartWorld(ctx context.Context, root *storage.Postgres, worldID, bootID string) (*storage.Postgres, storage.WorldReceipt, error) {
	writer, err := root.ClaimWorldWriter(ctx, worldID, bootID)
	if err != nil {
		return nil, storage.WorldReceipt{}, err
	}
	request, err := json.Marshal(struct{ Kind, Boot string }{"cold-start", bootID})
	if err != nil {
		return nil, storage.WorldReceipt{}, err
	}
	digest := sha256.Sum256(request)
	receipt, err := Execute(ctx, writer, worldID, "startup-"+hex.EncodeToString(digest[:]), request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, disconnected, err := state.RecoverOffline()
		if err != nil {
			return nil, nil, err
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(disconnected)
		return saved, response, err
	})
	if err != nil {
		return nil, storage.WorldReceipt{}, err
	}
	return writer, receipt, nil
}

// SeedWorldFromCatalog creates the first Go world snapshot from a reviewed
// legacy room catalog. It is an explicit, one-shot provisioning boundary: it
// never claims a writer, imports players, or converts legacy NPC/item graphs.
// Re-running against an existing world fails through the storage uniqueness
// constraint instead of silently replacing state.
func SeedWorldFromCatalog(ctx context.Context, root *storage.Postgres, worldID string, catalog world.LegacyRoomCatalog) error {
	if root == nil || worldID == "" {
		return fmt.Errorf("missing world seed target")
	}
	state, err := catalog.NewState()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return root.CreateWorld(ctx, worldID, raw)
}

// SeedCanonicalWorldFromCatalog provisions a reviewed room catalog after the
// explicit identity conversion steps have completed. NPC IDs are allocated in
// stable room/instance order; item IDs share one namespace across room floors
// and NPC inventories. This remains a one-shot provisioning boundary and does
// not claim a writer or overwrite an existing world.
func SeedCanonicalWorldFromCatalog(ctx context.Context, root *storage.Postgres, worldID string, catalog world.LegacyRoomCatalog) error {
	if root == nil || worldID == "" {
		return fmt.Errorf("missing world seed target")
	}
	state, err := catalog.NewState()
	if err != nil {
		return err
	}
	npcNext := 0
	state, err = state.ImportNPCs(func() (string, error) {
		npcNext++
		return fmt.Sprintf("npc-%08d", npcNext), nil
	})
	if err != nil {
		return fmt.Errorf("canonical NPC seed: %w", err)
	}
	itemNext := 0
	allocateItem := func() (string, error) {
		itemNext++
		return fmt.Sprintf("item-%08d", itemNext), nil
	}
	state, err = state.ImportRoomItems(allocateItem)
	if err != nil {
		return fmt.Errorf("canonical room item seed: %w", err)
	}
	state, err = state.ImportNPCItems(allocateItem)
	if err != nil {
		return fmt.Errorf("canonical NPC item seed: %w", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return root.CreateWorld(ctx, worldID, raw)
}
