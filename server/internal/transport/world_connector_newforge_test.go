package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type connectorNewForgeCatalog map[int16]world.LegacyObject

func (connectorNewForgeCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("monster lookup not used")
}

func (c connectorNewForgeCatalog) Object(id int16) (world.LegacyObject, error) {
	object, ok := c[id]
	if !ok {
		return world.LegacyObject{}, fmt.Errorf("unmigrated object %d", id)
	}
	return object, nil
}

func connectorNewForgeWeaponCatalog() connectorNewForgeCatalog {
	return connectorNewForgeCatalog{
		900: {Name: "무명도", Weight: 1},
		901: {Name: "무명검", Weight: 1},
		902: {Name: "무명봉", Weight: 1},
		903: {Name: "무명창", Weight: 1},
		904: {Name: "무명궁", Weight: 1},
	}
}

func connectorNewForgeRoomFlags(rforge bool) (flags [8]byte) {
	if rforge {
		flags[world.NewForgeRoomFlag/8] |= 1 << (world.NewForgeRoomFlag % 8)
	}
	return flags
}

func connectorNewForgeState(roomID int16, rforge bool) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			roomID: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: roomID, Name: "대장간", Flags: connectorNewForgeRoomFlags(rforge)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: roomID}, Online: true},
		},
	}
}

func connectorNewForgeReady(t *testing.T, store *connectorCommandStore, roomID int16, rforge bool) *worldConnection {
	t.Helper()
	raw, err := json.Marshal(connectorNewForgeState(roomID, rforge))
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.connections[conn] = struct{}{}
	return conn
}

func TestWorldConnectorSubmitDispatchesNewForgePromptAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorNewForgeState(world.NewForgeRoomID, true))
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.connections[conn] = struct{}{}

	output, err := conn.Submit(context.Background(), "무기만들기")
	if err != nil || output != world.NewForgePromptResponse || store.commits != 1 {
		t.Fatalf("newforge=%q err=%v commits=%d", output, err, store.commits)
	}
	if !conn.newForgeSelectArmPending || conn.forgeSelectArmPending {
		t.Fatalf("pending newforge=%t forge=%t", conn.newForgeSelectArmPending, conn.forgeSelectArmPending)
	}
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeNoBroadcastFlag) ||
		!world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatalf("saved flags=%+v want PREADI without PNOBRD", saved.Players["actor"].Body.Flags)
	}

	replay, err := conn.Submit(context.Background(), "무기만들기")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	replayed, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil || replayed.Players["actor"].Body.Flags != saved.Players["actor"].Body.Flags {
		t.Fatalf("replay mutated flags err=%v", err)
	}
}

func TestWorldConnectorSubmitNewForgeMissingRoomFlagIsTypedNoOp(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeReady(t, store, world.NewForgeRoomID, false)
	output, err := conn.Submit(context.Background(), "무기만들기")
	if err != nil || output != world.NewForgeNotForgeResponse || store.commits != 1 {
		t.Fatalf("not-forge=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.newForgeSelectArmPending || conn.forgeSelectArmPending {
		t.Fatalf("not-forge armed pending newforge=%t forge=%t", conn.newForgeSelectArmPending, conn.forgeSelectArmPending)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeNoBroadcastFlag) ||
		world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatalf("not-forge set flags=%+v", saved.Players["actor"].Body.Flags)
	}
}

func TestWorldConnectorSubmitNewForgeWrongRoomIsTypedNoOp(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeReady(t, store, 1, true)
	output, err := conn.Submit(context.Background(), "무기만들기")
	if err != nil || output != world.NewForgeWrongRoomResponse || store.commits != 1 {
		t.Fatalf("wrong-room=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.newForgeSelectArmPending || conn.forgeSelectArmPending {
		t.Fatalf("wrong-room armed pending newforge=%t forge=%t", conn.newForgeSelectArmPending, conn.forgeSelectArmPending)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatalf("wrong-room set PREADI flags=%+v", saved.Players["actor"].Body.Flags)
	}
}

func TestWorldConnectorSubmitNewForgeUnmigratedFlagsFailClosed(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: world.NewForgeRoomID}, Online: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-unmigrated", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	connector.connections[conn] = struct{}{}

	output, err := conn.Submit(context.Background(), "무기만들기")
	if err == nil || store.commits != 0 {
		t.Fatalf("unmigrated=%q err=%v commits=%d", output, err, store.commits)
	}
}

func connectorNewForgeSelectArmReady(t *testing.T, store *connectorCommandStore, catalog world.SpawnCatalog, gold int32) *worldConnection {
	t.Helper()
	state := connectorNewForgeState(world.NewForgeRoomID, true)
	actor := state.Players["actor"]
	actor.Body.Gold = gold
	state.Players["actor"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-select-arm", Catalog: catalog,
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.connections[conn] = struct{}{}
	return conn
}

func connectorNewForgeConfirmReady(t *testing.T, store *connectorCommandStore, catalog world.SpawnCatalog, gold int32) *worldConnection {
	t.Helper()
	state := connectorNewForgeState(world.NewForgeRoomID, true)
	actor := state.Players["actor"]
	actor.Body.Gold = gold
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	state.Players["actor"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-select-confirm", Catalog: catalog,
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.connections[conn] = struct{}{}
	return conn
}

func TestWorldConnectorSubmitNewForgeSelectArmLoadsTemplatesThroughSubmit(t *testing.T) {
	for _, line := range []string{"1", "2", "3", "4", "5"} {
		store := &connectorCommandStore{}
		conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
		if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse || store.commits != 1 {
			t.Fatalf("line %q start=%q err=%v commits=%d", line, output, err, store.commits)
		}
		if !conn.newForgeSelectArmPending || conn.forgeSelectArmPending {
			t.Fatalf("line %q pending newforge=%t forge=%t", line, conn.newForgeSelectArmPending, conn.forgeSelectArmPending)
		}
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.NewForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("line %q select=%q err=%v commits=%d", line, output, err, store.commits)
		}
		if output == world.ForgeMaterialResponse {
			t.Fatalf("line %q reused 제련 material prompt", line)
		}
		if conn.newForgeSelectArmPending {
			t.Fatalf("line %q left select_newarm case 2 pending after material prompt", line)
		}
		if !conn.newForgeMaterialPending || conn.newForgeObjectID == 0 {
			t.Fatalf("line %q did not arm select_newarm case 3 object=%d", line, conn.newForgeObjectID)
		}
		if conn.forgeSelectArmPending || conn.forgeMaterialPending {
			t.Fatalf("line %q armed 제련 continuation", line)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("line %q gold=%d", line, body.Gold)
		}
		if world.PlayerFlagSet(body, world.NewForgeNoBroadcastFlag) || !world.PlayerFlagSet(body, world.NewForgeReadingFlag) {
			t.Fatalf("line %q flags=%+v", line, body.Flags)
		}
	}
}

func TestWorldConnectorSubmitNewForgeSelectArmInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse || store.commits != 1 {
		t.Fatalf("start=%q err=%v commits=%d", output, err, store.commits)
	}
	for _, line := range []string{"6", "0", "x", "도", "제련", ""} {
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.NewForgeRepromptResponse {
			t.Fatalf("invalid %q output=%q err=%v", line, output, err)
		}
		if !conn.newForgeSelectArmPending {
			t.Fatalf("invalid %q cleared select_newarm continuation", line)
		}
		if conn.forgeSelectArmPending {
			t.Fatalf("invalid %q armed 제련 pending", line)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("invalid %q gold=%d", line, saved.Players["actor"].Body.Gold)
		}
	}
	if store.commits != 7 {
		t.Fatalf("commits=%d want start plus six invalid receipts", store.commits)
	}
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != world.NewForgeMaterialResponse || store.commits != 8 {
		t.Fatalf("choice after invalid=%q err=%v commits=%d", output, err, store.commits)
	}
}

func TestWorldConnectorSubmitNewForgeSelectArmRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorNewForgeState(world.NewForgeRoomID, true)
		actor := state.Players["actor"]
		actor.Body.Gold = 50000
		state.Players["actor"] = actor
		return state
	}())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &infoContinuationRetryStore{base: base}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-select-arm-retry", Catalog: connectorNewForgeWeaponCatalog(),
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "2"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.newForgeSelectArmPending || conn.newForgeSelectArmCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.newForgeSelectArmCommandID
	output, err := conn.Submit(context.Background(), "2")
	if err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.newForgeSelectArmPending || conn.newForgeSelectArmCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if !conn.newForgeMaterialPending || conn.newForgeObjectID != 901 {
		t.Fatalf("retry did not arm select_newarm case 3 object=%d", conn.newForgeObjectID)
	}
	if conn.forgeSelectArmPending || conn.forgeMaterialPending {
		t.Fatal("retry armed 제련 continuation")
	}
	if len(store.commands) != 3 || store.commands[1] != wantID || store.commands[2] != wantID {
		t.Fatalf("continuation command IDs=%v want repeated %q", store.commands, wantID)
	}
	if _, commits := base.snapshot(); commits != 2 {
		t.Fatalf("commits=%d want start plus one continuation", commits)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("retry charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectArmFailClosedWithoutCatalog(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, nil, 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse || store.commits != 1 {
		t.Fatalf("start=%q err=%v commits=%d", output, err, store.commits)
	}
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 1 {
		t.Fatalf("missing catalog=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.newForgeSelectArmPending {
		t.Fatal("fail-closed catalog left select_newarm pending")
	}
	if conn.newForgeMaterialPending {
		t.Fatal("fail-closed catalog armed select_newarm case 3")
	}
	if conn.forgeSelectArmPending || conn.forgeMaterialPending {
		t.Fatal("fail-closed catalog armed 제련 continuation")
	}
}

func TestWorldConnectorSubmitNewForgeSelectArmDoesNotInterceptWithoutStart(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("idle digit=%q err=%v commits=%d", output, err, store.commits)
	}
}

func TestWorldConnectorSubmitNewForgeSelectMaterialSetsDiceThroughSubmit(t *testing.T) {
	cases := []struct {
		arm, material string
		dice          int16
		sum           int32
		objectID      int16
	}{
		{"1", "1", world.NewForgeEmeraldDiceCount, world.NewForgeEmeraldCost, 900},
		{"2", "2", world.NewForgeTitaniumDiceCount, world.NewForgeTitaniumCost, 901},
		{"4", "3", world.NewForgeIllusionDiceCount, world.NewForgeIllusionCost, 903},
	}
	for _, tt := range cases {
		store := &connectorCommandStore{}
		conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
		if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse || store.commits != 1 {
			t.Fatalf("arm %q start=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.arm); err != nil || output != world.NewForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("arm %q select=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.newForgeSelectArmPending || !conn.newForgeMaterialPending || conn.newForgeObjectID != tt.objectID {
			t.Fatalf("arm %q pending select=%t material=%t object=%d", tt.arm, conn.newForgeSelectArmPending, conn.newForgeMaterialPending, conn.newForgeObjectID)
		}
		output, err := conn.Submit(context.Background(), tt.material)
		if err != nil || output != world.NewForgeQuenchResponse || store.commits != 3 {
			t.Fatalf("arm %q material=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output == world.ForgeQuenchResponse {
			t.Fatalf("arm %q reused 제련 quench prompt", tt.arm)
		}
		if conn.newForgeMaterialPending {
			t.Fatalf("arm %q left material pending after quench prompt", tt.arm)
		}
		if !conn.newForgeQuenchPending {
			t.Fatalf("arm %q did not arm select_newarm case 4", tt.arm)
		}
		if conn.forgeQuenchPending || conn.forgeMaterialPending {
			t.Fatalf("arm %q armed 제련 continuation", tt.arm)
		}
		if conn.newForgeObjectID != tt.objectID || conn.newForgeSum != tt.sum {
			t.Fatalf("arm %q forge2 identity object=%d sum=%d", tt.arm, conn.newForgeObjectID, conn.newForgeSum)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("arm %q gold=%d", tt.arm, body.Gold)
		}
		if world.PlayerFlagSet(body, world.NewForgeNoBroadcastFlag) || !world.PlayerFlagSet(body, world.NewForgeReadingFlag) {
			t.Fatalf("arm %q flags=%+v", tt.arm, body.Flags)
		}
	}
}

func TestWorldConnectorSubmitNewForgeSelectMaterialInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	for _, line := range []string{"6", "0", "4", "x", "도", "제련", ""} {
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.NewForgeRepromptResponse {
			t.Fatalf("invalid %q output=%q err=%v", line, output, err)
		}
		if !conn.newForgeMaterialPending {
			t.Fatalf("invalid %q cleared select_newarm case 3", line)
		}
		if conn.forgeMaterialPending || conn.forgeSelectArmPending {
			t.Fatalf("invalid %q armed 제련 pending", line)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("invalid %q gold=%d", line, saved.Players["actor"].Body.Gold)
		}
	}
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("choice after invalid=%q err=%v", output, err)
	}
	if conn.newForgeMaterialPending {
		t.Fatal("successful material remained pending")
	}
	if !conn.newForgeQuenchPending {
		t.Fatal("successful material did not arm select_newarm case 4")
	}
	if conn.forgeQuenchPending {
		t.Fatal("successful material armed 제련 quench")
	}
	if conn.newForgeSum != world.NewForgeEmeraldCost {
		t.Fatalf("successful material sum=%d", conn.newForgeSum)
	}
}

func TestWorldConnectorSubmitNewForgeSelectMaterialRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorNewForgeState(world.NewForgeRoomID, true)
		actor := state.Players["actor"]
		actor.Body.Gold = 50000
		state.Players["actor"] = actor
		return state
	}())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &infoContinuationRetryStore{base: base}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-select-material-retry", Catalog: connectorNewForgeWeaponCatalog(),
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "2"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "1"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.newForgeMaterialPending || conn.newForgeMaterialCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.newForgeMaterialCommandID
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.newForgeMaterialPending || conn.newForgeMaterialCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if !conn.newForgeQuenchPending {
		t.Fatal("retry did not arm select_newarm case 4")
	}
	if conn.forgeQuenchPending || conn.forgeMaterialPending {
		t.Fatal("retry armed 제련 continuation")
	}
	if conn.newForgeObjectID != 901 || conn.newForgeSum != world.NewForgeEmeraldCost {
		t.Fatalf("retry did not keep forge2 identity object=%d sum=%d", conn.newForgeObjectID, conn.newForgeSum)
	}
	if len(store.commands) < 3 || store.commands[len(store.commands)-2] != wantID || store.commands[len(store.commands)-1] != wantID {
		t.Fatalf("continuation command IDs=%v want repeated %q", store.commands, wantID)
	}
	if _, commits := base.snapshot(); commits != 3 {
		t.Fatalf("commits=%d want start plus weapon plus one material", commits)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("retry charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectMaterialFailClosedWithoutCatalog(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	conn.game.config.Catalog = nil
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("missing catalog=%q err=%v", output, err)
	}
	if conn.newForgeMaterialPending {
		t.Fatal("fail-closed catalog left select_newarm case 3 pending")
	}
	if conn.newForgeQuenchPending {
		t.Fatal("fail-closed catalog left select_newarm case 4 pending")
	}
	if conn.forgeQuenchPending || conn.forgeMaterialPending {
		t.Fatal("fail-closed catalog armed 제련 continuation")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("fail-closed charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectMaterialDoesNotInterceptWithoutStart(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("idle digit=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.newForgeMaterialPending {
		t.Fatal("idle digit armed select_newarm case 3")
	}
}

func TestWorldConnectorSubmitNewForgeSelectQuenchSetsShotsThroughSubmit(t *testing.T) {
	cases := []struct {
		arm, material, quench string
		shots                 int16
		matSum, cost          int32
		objectID              int16
	}{
		{"1", "1", "1", world.NewForgeQuench100Shots, world.NewForgeEmeraldCost, world.NewForgeQuench100Cost, 900},
		{"2", "2", "2", world.NewForgeQuench200Shots, world.NewForgeTitaniumCost, world.NewForgeQuench200Cost, 901},
		{"4", "3", "5", world.NewForgeQuench500Shots, world.NewForgeIllusionCost, world.NewForgeQuench500Cost, 903},
	}
	for _, tt := range cases {
		store := &connectorCommandStore{}
		conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
		if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse || store.commits != 1 {
			t.Fatalf("arm %q start=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.arm); err != nil || output != world.NewForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("arm %q select=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.material); err != nil || output != world.NewForgeQuenchResponse || store.commits != 3 {
			t.Fatalf("arm %q material=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.newForgeMaterialPending || !conn.newForgeQuenchPending || conn.newForgeObjectID != tt.objectID || conn.newForgeSum != tt.matSum {
			t.Fatalf("arm %q pending material=%t quench=%t object=%d sum=%d", tt.arm, conn.newForgeMaterialPending, conn.newForgeQuenchPending, conn.newForgeObjectID, conn.newForgeSum)
		}
		output, err := conn.Submit(context.Background(), tt.quench)
		if err != nil || output != world.NewForgeNameResponse || store.commits != 4 {
			t.Fatalf("arm %q quench=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.newForgeQuenchPending || !conn.newForgeNamePending {
			t.Fatalf("arm %q pending quench=%t name=%t", tt.arm, conn.newForgeQuenchPending, conn.newForgeNamePending)
		}
		if conn.forgeNamePending || conn.forgeQuenchPending {
			t.Fatalf("arm %q armed 제련 continuation", tt.arm)
		}
		if conn.newForgeObjectID != tt.objectID || conn.newForgeSum != tt.matSum || conn.newForgeQuenchChoice == 0 {
			t.Fatalf("arm %q identity object=%d sum=%d quench=%d", tt.arm, conn.newForgeObjectID, conn.newForgeSum, conn.newForgeQuenchChoice)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("arm %q gold=%d", tt.arm, body.Gold)
		}
		if world.PlayerFlagSet(body, world.NewForgeNoBroadcastFlag) || !world.PlayerFlagSet(body, world.NewForgeReadingFlag) {
			t.Fatalf("arm %q flags=%+v", tt.arm, body.Flags)
		}
	}
}

func TestWorldConnectorSubmitNewForgeSelectQuenchInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	for _, line := range []string{"6", "0", "x", "도", "제련", ""} {
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.NewForgeRepromptResponse {
			t.Fatalf("invalid %q output=%q err=%v", line, output, err)
		}
		if !conn.newForgeQuenchPending {
			t.Fatalf("invalid %q cleared select_newarm case 4", line)
		}
		if conn.forgeQuenchPending || conn.forgeNamePending {
			t.Fatalf("invalid %q armed 제련 pending", line)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("invalid %q gold=%d", line, saved.Players["actor"].Body.Gold)
		}
	}
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("choice after invalid=%q err=%v", output, err)
	}
	if conn.newForgeQuenchPending || !conn.newForgeNamePending {
		t.Fatalf("successful quench quenchPending=%t namePending=%t", conn.newForgeQuenchPending, conn.newForgeNamePending)
	}
	if conn.forgeNamePending {
		t.Fatal("successful quench armed 제련 name")
	}
	if conn.newForgeSum != world.NewForgeEmeraldCost || conn.newForgeQuenchChoice != 1 {
		t.Fatalf("successful quench identity sum=%d choice=%d", conn.newForgeSum, conn.newForgeQuenchChoice)
	}
}

func TestWorldConnectorSubmitNewForgeSelectQuenchRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorNewForgeState(world.NewForgeRoomID, true)
		actor := state.Players["actor"]
		actor.Body.Gold = 50000
		state.Players["actor"] = actor
		return state
	}())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &infoContinuationRetryStore{base: base}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-select-quench-retry", Catalog: connectorNewForgeWeaponCatalog(),
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "2"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "1"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.newForgeQuenchPending || conn.newForgeQuenchCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.newForgeQuenchCommandID
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.newForgeQuenchPending || conn.newForgeQuenchCommandID != "" || !conn.newForgeNamePending {
		t.Fatalf("successful continuation quenchPending=%t namePending=%t", conn.newForgeQuenchPending, conn.newForgeNamePending)
	}
	if conn.forgeNamePending || conn.forgeQuenchPending {
		t.Fatal("retry armed 제련 continuation")
	}
	if conn.newForgeObjectID != 901 || conn.newForgeSum != world.NewForgeEmeraldCost || conn.newForgeQuenchChoice != 1 {
		t.Fatalf("retry did not keep forge2 identity object=%d sum=%d quench=%d", conn.newForgeObjectID, conn.newForgeSum, conn.newForgeQuenchChoice)
	}
	if len(store.commands) < 3 || store.commands[len(store.commands)-2] != wantID || store.commands[len(store.commands)-1] != wantID {
		t.Fatalf("continuation command IDs=%v want repeated %q", store.commands, wantID)
	}
	if _, commits := base.snapshot(); commits != 4 {
		t.Fatalf("commits=%d want start plus weapon plus material plus one quench", commits)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("retry charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectQuenchFailClosedWithoutCatalog(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	conn.game.config.Catalog = nil
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("missing catalog=%q err=%v", output, err)
	}
	if conn.newForgeQuenchPending {
		t.Fatal("fail-closed catalog left select_newarm case 4 pending")
	}
	if conn.forgeNamePending || conn.forgeQuenchPending {
		t.Fatal("fail-closed catalog armed 제련 continuation")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("fail-closed charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectQuenchDoesNotInterceptWithoutStart(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("idle digit=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.newForgeQuenchPending {
		t.Fatal("idle digit armed select_newarm case 4")
	}
}

func TestWorldConnectorSubmitNewForgeSelectNameSetsNameThroughSubmit(t *testing.T) {
	cases := []struct {
		arm, material, quench, name string
		objectID                    int16
		matSum                      int32
	}{
		{"1", "1", "1", "abc", 900, world.NewForgeEmeraldCost},
		{"2", "2", "2", "불의검", 901, world.NewForgeTitaniumCost},
		{"4", "3", "5", "12345678901234567890", 903, world.NewForgeIllusionCost},
	}
	for _, tt := range cases {
		store := &connectorCommandStore{}
		conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
		if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse || store.commits != 1 {
			t.Fatalf("arm %q start=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.arm); err != nil || output != world.NewForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("arm %q select=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.material); err != nil || output != world.NewForgeQuenchResponse || store.commits != 3 {
			t.Fatalf("arm %q material=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.quench); err != nil || output != world.NewForgeNameResponse || store.commits != 4 {
			t.Fatalf("arm %q quench=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.newForgeQuenchPending || !conn.newForgeNamePending {
			t.Fatalf("arm %q pending quench=%t name=%t", tt.arm, conn.newForgeQuenchPending, conn.newForgeNamePending)
		}
		output, err := conn.Submit(context.Background(), tt.name)
		if err != nil || output != world.NewForgeConfirmResponse || store.commits != 5 {
			t.Fatalf("arm %q name=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.newForgeNamePending || conn.forgeConfirmPending || !conn.newForgeConfirmPending {
			t.Fatalf("arm %q namePending=%t forgeConfirm=%t newforgeConfirm=%t", tt.arm, conn.newForgeNamePending, conn.forgeConfirmPending, conn.newForgeConfirmPending)
		}
		if conn.newForgeWeaponName != tt.name {
			t.Fatalf("arm %q weapon=%q", tt.arm, conn.newForgeWeaponName)
		}
		if conn.newForgeObjectID != tt.objectID || conn.newForgeSum != tt.matSum {
			t.Fatalf("arm %q identity object=%d sum=%d", tt.arm, conn.newForgeObjectID, conn.newForgeSum)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("arm %q gold=%d", tt.arm, body.Gold)
		}
		if len(body.Inventory) != 0 || saved.Players["actor"].Items != nil {
			t.Fatalf("arm %q invented inventory", tt.arm)
		}
	}
}

func TestWorldConnectorSubmitNewForgeSelectNameInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	cases := []struct {
		line, response string
	}{
		{"ab", world.NewForgeNameShortResponse},
		{"", world.NewForgeNameShortResponse},
		{"123456789012345678901", world.NewForgeNameLongResponse},
		{"ab(c", world.NewForgeNameParenResponse},
		{"검)", world.NewForgeNameParenResponse},
	}
	for _, tt := range cases {
		output, err := conn.Submit(context.Background(), tt.line)
		if err != nil || output != tt.response {
			t.Fatalf("invalid %q output=%q err=%v", tt.line, output, err)
		}
		if !conn.newForgeNamePending {
			t.Fatalf("invalid %q cleared select_newarm case 5", tt.line)
		}
		if conn.forgeNamePending || conn.forgeConfirmPending {
			t.Fatalf("invalid %q armed 제련 pending", tt.line)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("invalid %q gold=%d", tt.line, saved.Players["actor"].Body.Gold)
		}
	}
	output, err := conn.Submit(context.Background(), "불의검")
	if err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("name after invalid=%q err=%v", output, err)
	}
	if conn.newForgeNamePending || conn.forgeConfirmPending || !conn.newForgeConfirmPending {
		t.Fatalf("successful name namePending=%t forgeConfirm=%t newforgeConfirm=%t", conn.newForgeNamePending, conn.forgeConfirmPending, conn.newForgeConfirmPending)
	}
}

func TestWorldConnectorSubmitNewForgeSelectNameRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorNewForgeState(world.NewForgeRoomID, true)
		actor := state.Players["actor"]
		actor.Body.Gold = 50000
		state.Players["actor"] = actor
		return state
	}())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &infoContinuationRetryStore{base: base}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-select-name-retry", Catalog: connectorNewForgeWeaponCatalog(),
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "2"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "불의검"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.newForgeNamePending || conn.newForgeNameCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.newForgeNameCommandID
	output, err := conn.Submit(context.Background(), "불의검")
	if err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.newForgeNamePending || conn.newForgeNameCommandID != "" || conn.forgeConfirmPending || !conn.newForgeConfirmPending {
		t.Fatalf("successful name namePending=%t forgeConfirm=%t newforgeConfirm=%t", conn.newForgeNamePending, conn.forgeConfirmPending, conn.newForgeConfirmPending)
	}
	if conn.newForgeObjectID != 901 || conn.newForgeSum != world.NewForgeEmeraldCost || conn.newForgeQuenchChoice != 1 {
		t.Fatalf("retry did not keep forge2 identity object=%d sum=%d quench=%d", conn.newForgeObjectID, conn.newForgeSum, conn.newForgeQuenchChoice)
	}
	if len(store.commands) < 4 || store.commands[len(store.commands)-2] != wantID || store.commands[len(store.commands)-1] != wantID {
		t.Fatalf("continuation command IDs=%v want repeated %q", store.commands, wantID)
	}
	if _, commits := base.snapshot(); commits != 5 {
		t.Fatalf("commits=%d want start plus weapon plus material plus quench plus one name", commits)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("retry charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectNameFailClosedWithoutCatalog(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	conn.game.config.Catalog = nil
	output, err := conn.Submit(context.Background(), "abc")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("missing catalog=%q err=%v", output, err)
	}
	if conn.newForgeNamePending {
		t.Fatal("fail-closed catalog left select_newarm case 5 pending")
	}
	if conn.forgeNamePending || conn.forgeConfirmPending {
		t.Fatal("fail-closed catalog armed 제련 continuation")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("fail-closed charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectNameDoesNotInterceptWithoutStart(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	output, err := conn.Submit(context.Background(), "불의검")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("idle name=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.newForgeNamePending {
		t.Fatal("idle name armed select_newarm case 5")
	}
}

func TestWorldConnectorSubmitNewForgeSelectNameInterceptsMoveAndForgeAliases(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "도")
	if err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("도 as name=%q err=%v", output, err)
	}
	if conn.newForgeNamePending || conn.forgeSelectArmPending || !conn.newForgeConfirmPending {
		t.Fatalf("도 as name left pending name=%t forge=%t newforgeConfirm=%t", conn.newForgeNamePending, conn.forgeSelectArmPending, conn.newForgeConfirmPending)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("도 as name charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeSelectConfirmChargesGoldAndAddsWeapon(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeConfirmReady(t, store, connectorNewForgeWeaponCatalog(), 2000000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	if conn.newForgeNamePending || !conn.newForgeConfirmPending || conn.forgeConfirmPending || conn.newForgeWeaponName != "불의검" {
		t.Fatalf("confirm pending=%t namePending=%t forgeConfirm=%t weapon=%q", conn.newForgeConfirmPending, conn.newForgeNamePending, conn.forgeConfirmPending, conn.newForgeWeaponName)
	}
	output, err := conn.Submit(context.Background(), "예")
	if err != nil || output != world.NewForgeGiveResponse {
		t.Fatalf("confirm=%q err=%v", output, err)
	}
	if conn.newForgeConfirmPending || conn.newForgeNamePending || conn.forgeConfirmPending {
		t.Fatal("successful confirm left a continuation armed")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	wantGold := int32(2000000 - (world.NewForgeEmeraldCost + world.NewForgeQuench100Cost))
	if saved.Players["actor"].Body.Gold != wantGold {
		t.Fatalf("gold=%d want %d", saved.Players["actor"].Body.Gold, wantGold)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatal("confirm left PREADI set")
	}
	if saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 1 {
		t.Fatalf("items=%+v", saved.Players["actor"].Items)
	}
	select {
	case text := <-conn.events:
		t.Fatalf("actor received broadcast %q", text)
	default:
	}
}

func TestWorldConnectorSubmitNewForgeSelectConfirmInsufficientGoldDoesNotCharge(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeConfirmReady(t, store, connectorNewForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "예")
	if err != nil || output != world.NewForgeTooPoorResponse {
		t.Fatalf("poor=%q err=%v", output, err)
	}
	if conn.newForgeConfirmPending {
		t.Fatal("too-poor left confirm pending")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("poor gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatal("too-poor left PREADI set")
	}
}

func TestWorldConnectorSubmitNewForgeSelectConfirmCancelDoesNotCharge(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeConfirmReady(t, store, connectorNewForgeWeaponCatalog(), 2000000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "아니오")
	if err != nil || output != world.NewForgeCancelResponse {
		t.Fatalf("cancel=%q err=%v", output, err)
	}
	if conn.newForgeConfirmPending {
		t.Fatal("cancel left confirm pending")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 2000000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("cancel gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
}

func TestWorldConnectorSubmitNewForgeSelectConfirmRetriesSameCommandIDWithoutRecharge(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorNewForgeState(world.NewForgeRoomID, true)
		actor := state.Players["actor"]
		actor.Body.Gold = 2000000
		actor.Items = &world.ItemCollection{Items: map[string]world.Item{}}
		state.Players["actor"] = actor
		return state
	}())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &infoContinuationRetryStore{base: base}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "newforge-select-confirm-retry", Catalog: connectorNewForgeWeaponCatalog(),
		Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "예"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.newForgeConfirmPending || conn.newForgeConfirmCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.newForgeConfirmCommandID
	output, err := conn.Submit(context.Background(), "예")
	if err != nil || output != world.NewForgeGiveResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.newForgeConfirmPending || conn.newForgeConfirmCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if len(store.commands) < 6 || store.commands[len(store.commands)-2] != wantID || store.commands[len(store.commands)-1] != wantID {
		t.Fatalf("continuation command IDs=%v want repeated %q", store.commands, wantID)
	}
	if _, commits := base.snapshot(); commits != 6 {
		t.Fatalf("commits=%d want start plus weapon plus material plus quench plus name plus one confirm", commits)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	wantGold := int32(2000000 - (world.NewForgeEmeraldCost + world.NewForgeQuench100Cost))
	if saved.Players["actor"].Body.Gold != wantGold {
		t.Fatalf("retry gold=%d want %d", saved.Players["actor"].Body.Gold, wantGold)
	}
	if len(saved.Players["actor"].Items.Items) != 1 {
		t.Fatalf("retry items=%+v", saved.Players["actor"].Items)
	}
}

func TestWorldConnectorSubmitNewForgeSelectConfirmFailClosedWithoutObjectGraph(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeSelectArmReady(t, store, connectorNewForgeWeaponCatalog(), 2000000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	if !conn.newForgeConfirmPending {
		t.Fatal("successful name did not arm confirm continuation")
	}
	output, err := conn.Submit(context.Background(), "예")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("missing graph confirm=%q err=%v", output, err)
	}
	if conn.newForgeConfirmPending {
		t.Fatal("fail-closed confirm left select_newarm case 6 pending")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 2000000 {
		t.Fatalf("fail-closed charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitNewForgeDoesNotAdmitForgeAfterConfirm(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorNewForgeConfirmReady(t, store, connectorNewForgeWeaponCatalog(), 2000000)
	if output, err := conn.Submit(context.Background(), "무기만들기"); err != nil || output != world.NewForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.NewForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.NewForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "제련")
	if err != nil || output != world.NewForgeCancelResponse {
		t.Fatalf("제련 during confirm=%q err=%v", output, err)
	}
	if conn.newForgeConfirmPending || conn.forgeSelectArmPending {
		t.Fatalf("제련 during confirm left pending newforge=%t forge=%t", conn.newForgeConfirmPending, conn.forgeSelectArmPending)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 2000000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("제련 gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
}
