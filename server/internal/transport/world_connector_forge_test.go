package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type connectorForgeCatalog map[int16]world.LegacyObject

func (connectorForgeCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("monster lookup not used")
}

func (c connectorForgeCatalog) Object(id int16) (world.LegacyObject, error) {
	object, ok := c[id]
	if !ok {
		return world.LegacyObject{}, fmt.Errorf("unmigrated object %d", id)
	}
	return object, nil
}

func connectorForgeWeaponCatalog() connectorForgeCatalog {
	return connectorForgeCatalog{
		900: {Name: "무명도", Weight: 1},
		901: {Name: "무명검", Weight: 1},
		902: {Name: "무명봉", Weight: 1},
		903: {Name: "무명창", Weight: 1},
		904: {Name: "무명궁", Weight: 1},
	}
}

func connectorForgeRoomFlags(rforge bool) (flags [8]byte) {
	if rforge {
		flags[world.ForgeRoomFlag/8] |= 1 << (world.ForgeRoomFlag % 8)
	}
	return flags
}

func connectorForgeState(rforge bool) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "대장간", Flags: connectorForgeRoomFlags(rforge)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
	}
}

func TestWorldConnectorSubmitDispatchesForgePromptAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorForgeState(true))
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "forge", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
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

	output, err := conn.Submit(context.Background(), "제련")
	if err != nil || output != world.ForgePromptResponse || store.commits != 1 {
		t.Fatalf("forge=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeNoBroadcastFlag) ||
		!world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
		t.Fatalf("saved flags=%+v want PREADI without PNOBRD", saved.Players["actor"].Body.Flags)
	}

	replay, err := conn.Submit(context.Background(), "제련")
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

func TestWorldConnectorSubmitForgeMissingRoomFlagIsTypedNoOp(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorForgeState(false))
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "forge-not", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
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

	output, err := conn.Submit(context.Background(), "제련")
	if err != nil || output != world.ForgeNotForgeResponse || store.commits != 1 {
		t.Fatalf("not-forge=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeNoBroadcastFlag) ||
		world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
		t.Fatalf("not-forge set flags=%+v", saved.Players["actor"].Body.Flags)
	}
}

func TestWorldConnectorSubmitForgeDoesNotAdmitNewforge(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorForgeState(true))
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "forge-newforge", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
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
	if err != nil || output != world.NewForgeWrongRoomResponse || store.commits != 1 {
		t.Fatalf("newforge=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.forgeSelectArmPending || conn.newForgeSelectArmPending {
		t.Fatalf("무기만들기 started 제련 pending=%t newforge=%t", conn.forgeSelectArmPending, conn.newForgeSelectArmPending)
	}
}

func connectorForgeReady(t *testing.T, store *connectorCommandStore, catalog world.SpawnCatalog, gold int32) *worldConnection {
	t.Helper()
	state := connectorForgeState(true)
	actor := state.Players["actor"]
	actor.Body.Gold = gold
	state.Players["actor"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "forge-select-arm", Catalog: catalog,
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

func TestWorldConnectorSubmitForgeSelectArmLoadsTemplatesThroughSubmit(t *testing.T) {
	for _, line := range []string{"1", "2", "3", "4", "5"} {
		store := &connectorCommandStore{}
		conn := connectorForgeReady(t, store, connectorForgeWeaponCatalog(), 50000)
		if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse || store.commits != 1 {
			t.Fatalf("line %q start=%q err=%v commits=%d", line, output, err, store.commits)
		}
		if !conn.forgeSelectArmPending {
			t.Fatalf("line %q did not arm select_arm continuation", line)
		}
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.ForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("line %q select=%q err=%v commits=%d", line, output, err, store.commits)
		}
		if conn.forgeSelectArmPending {
			t.Fatalf("line %q left select_arm case 2 pending after material prompt", line)
		}
		if !conn.forgeMaterialPending || conn.forgeObjectID == 0 {
			t.Fatalf("line %q did not arm material continuation object=%d", line, conn.forgeObjectID)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("line %q gold=%d", line, body.Gold)
		}
		if world.PlayerFlagSet(body, world.ForgeNoBroadcastFlag) || !world.PlayerFlagSet(body, world.ForgeReadingFlag) {
			t.Fatalf("line %q flags=%+v", line, body.Flags)
		}
	}
}

func TestWorldConnectorSubmitForgeSelectArmInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeReady(t, store, connectorForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse || store.commits != 1 {
		t.Fatalf("start=%q err=%v commits=%d", output, err, store.commits)
	}
	for _, line := range []string{"6", "0", "x", "도", "무기만들기", ""} {
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.ForgeRepromptResponse {
			t.Fatalf("invalid %q output=%q err=%v", line, output, err)
		}
		if !conn.forgeSelectArmPending {
			t.Fatalf("invalid %q cleared select_arm continuation", line)
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
	if err != nil || output != world.ForgeMaterialResponse || store.commits != 8 {
		t.Fatalf("choice after invalid=%q err=%v commits=%d", output, err, store.commits)
	}
}

func TestWorldConnectorSubmitForgeSelectArmRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorForgeState(true)
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
		Store: store, WorldID: "forge-select-arm-retry", Catalog: connectorForgeWeaponCatalog(),
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
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "2"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.forgeSelectArmPending || conn.forgeSelectArmCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.forgeSelectArmCommandID
	output, err := conn.Submit(context.Background(), "2")
	if err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.forgeSelectArmPending || conn.forgeSelectArmCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if !conn.forgeMaterialPending || conn.forgeObjectID != 901 {
		t.Fatalf("retry did not arm material continuation object=%d", conn.forgeObjectID)
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

func TestWorldConnectorSubmitForgeSelectArmFailClosedWithoutCatalog(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeReady(t, store, nil, 50000)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse || store.commits != 1 {
		t.Fatalf("start=%q err=%v commits=%d", output, err, store.commits)
	}
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 1 {
		t.Fatalf("missing catalog=%q err=%v commits=%d", output, err, store.commits)
	}
	if conn.forgeSelectArmPending {
		t.Fatal("fail-closed catalog left select_arm pending")
	}
	if conn.forgeMaterialPending {
		t.Fatal("fail-closed catalog armed material continuation")
	}
}

func TestWorldConnectorSubmitForgeSelectArmDoesNotInterceptWithoutStart(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeReady(t, store, connectorForgeWeaponCatalog(), 50000)
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("idle digit=%q err=%v commits=%d", output, err, store.commits)
	}
}

func connectorForgeMaterialReady(t *testing.T, store *connectorCommandStore, catalog world.SpawnCatalog, gold int32, class byte) *worldConnection {
	t.Helper()
	state := connectorForgeState(true)
	actor := state.Players["actor"]
	actor.Body.Gold = gold
	actor.Body.Class = class
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	state.Players["actor"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "forge-select-material", Catalog: catalog,
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

func TestWorldConnectorSubmitForgeSelectMaterialSetsSdiceThroughSubmit(t *testing.T) {
	cases := []struct {
		arm, material string
		dice          int16
		sum           int32
	}{
		{"1", "1", world.ForgeSteelDice, world.ForgeSteelCost},
		{"2", "2", world.ForgePreciousDice, world.ForgePreciousCost},
		{"4", "3", world.ForgeDiamondDice, world.ForgeDiamondCost},
	}
	for _, tt := range cases {
		store := &connectorCommandStore{}
		conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 50000, 4)
		if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse || store.commits != 1 {
			t.Fatalf("arm %q start=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.arm); err != nil || output != world.ForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("arm %q select=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.forgeSelectArmPending || !conn.forgeMaterialPending {
			t.Fatalf("arm %q pending select=%t material=%t", tt.arm, conn.forgeSelectArmPending, conn.forgeMaterialPending)
		}
		output, err := conn.Submit(context.Background(), tt.material)
		if err != nil || output != world.ForgeQuenchResponse || store.commits != 3 {
			t.Fatalf("arm %q material=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.forgeMaterialPending {
			t.Fatalf("arm %q left material pending after quench prompt", tt.arm)
		}
		if !conn.forgeQuenchPending || conn.forgeObjectID == 0 || conn.forgeSum != tt.sum {
			t.Fatalf("arm %q did not arm quench continuation object=%d sum=%d", tt.arm, conn.forgeObjectID, conn.forgeSum)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("arm %q gold=%d", tt.arm, body.Gold)
		}
		if saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 {
			t.Fatalf("arm %q invented items", tt.arm)
		}
	}
}

func TestWorldConnectorSubmitForgeSelectMaterialInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 50000, 4)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	for _, line := range []string{"6", "0", "4", "x", "도", "무기만들기", ""} {
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.ForgeRepromptResponse {
			t.Fatalf("invalid %q output=%q err=%v", line, output, err)
		}
		if !conn.forgeMaterialPending {
			t.Fatalf("invalid %q cleared material continuation", line)
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
	if err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("choice after invalid=%q err=%v", output, err)
	}
	if conn.forgeMaterialPending {
		t.Fatal("successful material remained pending")
	}
	if !conn.forgeQuenchPending || conn.forgeSum != world.ForgeSteelCost {
		t.Fatalf("successful material did not arm quench sum=%d", conn.forgeSum)
	}
}

func TestWorldConnectorSubmitForgeSelectMaterialDeniesRestrictedClass(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 300000, world.ForgeClericClass)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "3")
	if err != nil || output != world.ForgeMaterialDeniedResponse {
		t.Fatalf("denied=%q err=%v", output, err)
	}
	if !conn.forgeMaterialPending {
		t.Fatal("class deny cleared material continuation")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 300000 {
		t.Fatalf("denied gold=%d", saved.Players["actor"].Body.Gold)
	}
	output, err = conn.Submit(context.Background(), "1")
	if err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("steel after deny=%q err=%v", output, err)
	}
}

func TestWorldConnectorSubmitForgeSelectMaterialRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorForgeState(true)
		actor := state.Players["actor"]
		actor.Body.Gold = 50000
		actor.Body.Class = 4
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
		Store: store, WorldID: "forge-select-material-retry", Catalog: connectorForgeWeaponCatalog(),
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
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "2"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "1"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.forgeMaterialPending || conn.forgeMaterialCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.forgeMaterialCommandID
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.forgeMaterialPending || conn.forgeMaterialCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if !conn.forgeQuenchPending || conn.forgeObjectID != 901 || conn.forgeSum != world.ForgeSteelCost {
		t.Fatalf("retry did not arm quench continuation object=%d sum=%d", conn.forgeObjectID, conn.forgeSum)
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

func TestWorldConnectorSubmitForgeSelectMaterialFailClosedWithoutObjectGraph(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeReady(t, store, connectorForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "1")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("missing graph=%q err=%v", output, err)
	}
	if conn.forgeMaterialPending {
		t.Fatal("fail-closed graph left material pending")
	}
	if conn.forgeQuenchPending {
		t.Fatal("fail-closed graph armed quench continuation")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("fail-closed charged gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestWorldConnectorSubmitForgeSelectQuenchSetsShotsThroughSubmit(t *testing.T) {
	cases := []struct {
		arm, material, quench string
		shots                 int16
		materialSum           int32
		quenchCost            int32
	}{
		{"1", "1", "1", world.ForgeQuench100Shots, world.ForgeSteelCost, world.ForgeQuench100Cost},
		{"2", "2", "2", world.ForgeQuench200Shots, world.ForgePreciousCost, world.ForgeQuench200Cost},
		{"4", "3", "5", world.ForgeQuench500Shots, world.ForgeDiamondCost, world.ForgeQuench500Cost},
	}
	for _, tt := range cases {
		store := &connectorCommandStore{}
		conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 50000, 4)
		if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse || store.commits != 1 {
			t.Fatalf("arm %q start=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.arm); err != nil || output != world.ForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("arm %q select=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.material); err != nil || output != world.ForgeQuenchResponse || store.commits != 3 {
			t.Fatalf("arm %q material=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.forgeMaterialPending || !conn.forgeQuenchPending || conn.forgeSum != tt.materialSum {
			t.Fatalf("arm %q pending material=%t quench=%t sum=%d", tt.arm, conn.forgeMaterialPending, conn.forgeQuenchPending, conn.forgeSum)
		}
		output, err := conn.Submit(context.Background(), tt.quench)
		if err != nil || output != world.ForgeNameResponse || store.commits != 4 {
			t.Fatalf("arm %q quench=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.forgeQuenchPending {
			t.Fatalf("arm %q left quench pending after name prompt", tt.arm)
		}
		if !conn.forgeNamePending || conn.forgeObjectID == 0 || conn.forgeQuenchChoice == 0 {
			t.Fatalf("arm %q did not arm name continuation object=%d quench=%d", tt.arm, conn.forgeObjectID, conn.forgeQuenchChoice)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("arm %q gold=%d", tt.arm, body.Gold)
		}
		if saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 {
			t.Fatalf("arm %q invented items", tt.arm)
		}
	}
}

func TestWorldConnectorSubmitForgeSelectQuenchInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 50000, 4)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	for _, line := range []string{"6", "0", "x", "도", "무기만들기", ""} {
		output, err := conn.Submit(context.Background(), line)
		if err != nil || output != world.ForgeRepromptResponse {
			t.Fatalf("invalid %q output=%q err=%v", line, output, err)
		}
		if !conn.forgeQuenchPending {
			t.Fatalf("invalid %q cleared quench continuation", line)
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
	if err != nil || output != world.ForgeNameResponse {
		t.Fatalf("choice after invalid=%q err=%v", output, err)
	}
	if conn.forgeQuenchPending {
		t.Fatal("successful quench remained pending")
	}
	if !conn.forgeNamePending {
		t.Fatal("successful quench did not arm name continuation")
	}
}

func TestWorldConnectorSubmitForgeSelectQuenchRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorForgeState(true)
		actor := state.Players["actor"]
		actor.Body.Gold = 50000
		actor.Body.Class = 4
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
		Store: store, WorldID: "forge-select-quench-retry", Catalog: connectorForgeWeaponCatalog(),
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
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "2"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "4"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.forgeQuenchPending || conn.forgeQuenchCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.forgeQuenchCommandID
	output, err := conn.Submit(context.Background(), "4")
	if err != nil || output != world.ForgeNameResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.forgeQuenchPending || conn.forgeQuenchCommandID != "" {
		t.Fatal("successful continuation remained pending")
	}
	if !conn.forgeNamePending {
		t.Fatal("successful quench retry did not arm name continuation")
	}
	if len(store.commands) < 4 || store.commands[len(store.commands)-2] != wantID || store.commands[len(store.commands)-1] != wantID {
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

func TestWorldConnectorSubmitForgeSelectConfirmChargesGoldAndAddsWeapon(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 100000, 4)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.ForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	if conn.forgeNamePending || !conn.forgeConfirmPending || conn.forgeWeaponName != "불의검" {
		t.Fatalf("confirm pending=%t namePending=%t weapon=%q", conn.forgeConfirmPending, conn.forgeNamePending, conn.forgeWeaponName)
	}
	output, err := conn.Submit(context.Background(), "예")
	if err != nil || output != world.ForgeGiveResponse {
		t.Fatalf("confirm=%q err=%v", output, err)
	}
	if conn.forgeConfirmPending || conn.forgeNamePending {
		t.Fatal("successful confirm left a forge continuation armed")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	wantGold := int32(100000 - (world.ForgeSteelCost + world.ForgeQuench100Cost))
	if saved.Players["actor"].Body.Gold != wantGold {
		t.Fatalf("gold=%d want %d", saved.Players["actor"].Body.Gold, wantGold)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
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

func TestWorldConnectorSubmitForgeSelectQuenchFailClosedWithoutObjectGraph(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeReady(t, store, connectorForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("missing graph material=%q err=%v", output, err)
	}
	if conn.forgeQuenchPending {
		t.Fatal("fail-closed material armed quench continuation")
	}
}

func TestWorldConnectorSubmitForgeSelectNameSetsNameThroughSubmit(t *testing.T) {
	cases := []struct {
		arm, material, quench, name string
	}{
		{"1", "1", "1", "abc"},
		{"2", "2", "2", "불의검"},
		{"4", "3", "5", "12345678901234567890"},
	}
	for _, tt := range cases {
		store := &connectorCommandStore{}
		conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 50000, 4)
		if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse || store.commits != 1 {
			t.Fatalf("arm %q start=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.arm); err != nil || output != world.ForgeMaterialResponse || store.commits != 2 {
			t.Fatalf("arm %q select=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.material); err != nil || output != world.ForgeQuenchResponse || store.commits != 3 {
			t.Fatalf("arm %q material=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if output, err := conn.Submit(context.Background(), tt.quench); err != nil || output != world.ForgeNameResponse || store.commits != 4 {
			t.Fatalf("arm %q quench=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.forgeQuenchPending || !conn.forgeNamePending {
			t.Fatalf("arm %q pending quench=%t name=%t", tt.arm, conn.forgeQuenchPending, conn.forgeNamePending)
		}
		output, err := conn.Submit(context.Background(), tt.name)
		if err != nil || output != world.ForgeConfirmResponse || store.commits != 5 {
			t.Fatalf("arm %q name=%q err=%v commits=%d", tt.arm, output, err, store.commits)
		}
		if conn.forgeNamePending || !conn.forgeConfirmPending || conn.forgeWeaponName != tt.name {
			t.Fatalf("arm %q namePending=%t confirmPending=%t weapon=%q", tt.arm, conn.forgeNamePending, conn.forgeConfirmPending, conn.forgeWeaponName)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		body := saved.Players["actor"].Body
		if body.Gold != 50000 {
			t.Fatalf("arm %q gold=%d", tt.arm, body.Gold)
		}
		if saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 {
			t.Fatalf("arm %q invented items", tt.arm)
		}
	}
}

func TestWorldConnectorSubmitForgeSelectNameInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 50000, 4)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	cases := []struct {
		line, response string
	}{
		{"ab", world.ForgeNameShortResponse},
		{"", world.ForgeNameShortResponse},
		{"123456789012345678901", world.ForgeNameLongResponse},
		{"ab(c", world.ForgeNameParenResponse},
		{"검)", world.ForgeNameParenResponse},
	}
	for _, tt := range cases {
		output, err := conn.Submit(context.Background(), tt.line)
		if err != nil || output != tt.response {
			t.Fatalf("invalid %q output=%q err=%v", tt.line, output, err)
		}
		if !conn.forgeNamePending {
			t.Fatalf("invalid %q cleared name continuation", tt.line)
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
	if err != nil || output != world.ForgeConfirmResponse {
		t.Fatalf("name after invalid=%q err=%v", output, err)
	}
	if conn.forgeNamePending || !conn.forgeConfirmPending {
		t.Fatalf("successful name namePending=%t confirmPending=%t", conn.forgeNamePending, conn.forgeConfirmPending)
	}
}

func TestWorldConnectorSubmitForgeSelectNameRetriesSameCommandIDWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorForgeState(true)
		actor := state.Players["actor"]
		actor.Body.Gold = 50000
		actor.Body.Class = 4
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
		Store: store, WorldID: "forge-select-name-retry", Catalog: connectorForgeWeaponCatalog(),
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
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "2"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "4"); err != nil || output != world.ForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "불의검"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.forgeNamePending || conn.forgeNameCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.forgeNameCommandID
	output, err := conn.Submit(context.Background(), "불의검")
	if err != nil || output != world.ForgeConfirmResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.forgeNamePending || conn.forgeNameCommandID != "" || !conn.forgeConfirmPending {
		t.Fatalf("successful name namePending=%t confirmPending=%t", conn.forgeNamePending, conn.forgeConfirmPending)
	}
	if len(store.commands) < 5 || store.commands[len(store.commands)-2] != wantID || store.commands[len(store.commands)-1] != wantID {
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

func TestWorldConnectorSubmitForgeSelectNameFailClosedWithoutObjectGraph(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeReady(t, store, connectorForgeWeaponCatalog(), 50000)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("missing graph material=%q err=%v", output, err)
	}
	if conn.forgeNamePending {
		t.Fatal("fail-closed material armed name continuation")
	}
}

func TestWorldConnectorSubmitForgeSelectConfirmInsufficientGoldDoesNotCharge(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 50000, 4)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.ForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "예")
	if err != nil || output != world.ForgeTooPoorResponse {
		t.Fatalf("poor=%q err=%v", output, err)
	}
	if conn.forgeConfirmPending {
		t.Fatal("too-poor left confirm pending")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("poor gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
		t.Fatal("too-poor left PREADI set")
	}
}

func TestWorldConnectorSubmitForgeSelectConfirmCancelDoesNotCharge(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 100000, 4)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.ForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "아니오")
	if err != nil || output != world.ForgeCancelResponse {
		t.Fatalf("cancel=%q err=%v", output, err)
	}
	if conn.forgeConfirmPending {
		t.Fatal("cancel left confirm pending")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 100000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("cancel gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
}

func TestWorldConnectorSubmitForgeSelectConfirmRetriesSameCommandIDWithoutRecharge(t *testing.T) {
	raw, err := json.Marshal(func() world.State {
		state := connectorForgeState(true)
		actor := state.Players["actor"]
		actor.Body.Gold = 100000
		actor.Body.Class = 4
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
		Store: store, WorldID: "forge-select-confirm-retry", Catalog: connectorForgeWeaponCatalog(),
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
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.ForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	store.failOnce = true
	if output, err := conn.Submit(context.Background(), "예"); err == nil || output != "" {
		t.Fatalf("transient continuation output=%q err=%v", output, err)
	}
	if !conn.forgeConfirmPending || conn.forgeConfirmCommandID == "" {
		t.Fatal("failed continuation did not retain pending receipt identity")
	}
	wantID := conn.forgeConfirmCommandID
	output, err := conn.Submit(context.Background(), "예")
	if err != nil || output != world.ForgeGiveResponse {
		t.Fatalf("retried continuation output=%q err=%v", output, err)
	}
	if conn.forgeConfirmPending || conn.forgeConfirmCommandID != "" {
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
	wantGold := int32(100000 - (world.ForgeSteelCost + world.ForgeQuench100Cost))
	if saved.Players["actor"].Body.Gold != wantGold {
		t.Fatalf("retry gold=%d want %d", saved.Players["actor"].Body.Gold, wantGold)
	}
	if len(saved.Players["actor"].Items.Items) != 1 {
		t.Fatalf("retry items=%+v", saved.Players["actor"].Items)
	}
}

func TestWorldConnectorSubmitForgeDoesNotAdmitNewforgeAfterConfirm(t *testing.T) {
	store := &connectorCommandStore{}
	conn := connectorForgeMaterialReady(t, store, connectorForgeWeaponCatalog(), 100000, 4)
	if output, err := conn.Submit(context.Background(), "제련"); err != nil || output != world.ForgePromptResponse {
		t.Fatalf("start=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeMaterialResponse {
		t.Fatalf("select=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeQuenchResponse {
		t.Fatalf("material=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "1"); err != nil || output != world.ForgeNameResponse {
		t.Fatalf("quench=%q err=%v", output, err)
	}
	if output, err := conn.Submit(context.Background(), "불의검"); err != nil || output != world.ForgeConfirmResponse {
		t.Fatalf("name=%q err=%v", output, err)
	}
	output, err := conn.Submit(context.Background(), "무기만들기")
	if err != nil || output != world.ForgeCancelResponse {
		t.Fatalf("newforge during confirm=%q err=%v", output, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 100000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("newforge gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
}
