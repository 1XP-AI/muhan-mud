package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorFamilyNewsState() world.State {
	memberFlags := [8]byte{}
	memberFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	member := world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Flags: memberFlags}
	member.Daily[world.FamilyDailySlot].Max = 2
	bossFlags := [8]byte{}
	bossFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	bossFlags[world.FamilyBossFlag/8] |= 1 << (world.FamilyBossFlag % 8)
	boss := world.LegacyMonster{Name: "Boss", Type: 0, RoomID: 1, Flags: bossFlags}
	boss.Daily[world.FamilyDailySlot].Max = 2
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"member", "boss"},
		}},
		Players: map[string]world.PlayerState{
			"member": {Body: member, Online: true},
			"boss":   {Body: boss, Online: true},
		},
		FamilyNews: &world.FamilyNewsState{Bodies: map[int16]string{
			2: world.FamilyNewsHeader + "기존\n",
		}},
	}
}

func TestWorldConnectorSubmitDispatchesFamilyNewsViewAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorFamilyNewsState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:         store,
		WorldID:       "family-news-view",
		Clock:         func() (int32, int) { return 8, 12 },
		MaxSessions:   2,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bossLease, err := connector.owners.Acquire("boss")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(bossLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	memberLease, err := connector.owners.Acquire("member")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(memberLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	bossConn := &worldConnection{game: connector, lease: bossLease, ready: true, events: make(chan string, 4)}
	memberConn := &worldConnection{game: connector, lease: memberLease, ready: true, events: make(chan string, 4)}
	connector.mu.Lock()
	connector.connections[bossConn] = struct{}{}
	connector.connections[memberConn] = struct{}{}
	connector.mu.Unlock()

	viewed, err := bossConn.Submit(context.Background(), "패거리공지")
	if err != nil || !strings.Contains(viewed, "기존") || store.commits != 1 {
		t.Fatalf("view=%q err=%v commits=%d", viewed, err, store.commits)
	}
	select {
	case event := <-memberConn.events:
		t.Fatalf("view fanned out: %q", event)
	default:
	}

	replay, err := bossConn.Submit(context.Background(), "패거리공지")
	if err != nil || replay != viewed {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-memberConn.events:
		t.Fatalf("replay fanned out: %q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesFamilyNewsDeleteAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorFamilyNewsState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:         store,
		WorldID:       "family-news-delete",
		Clock:         func() (int32, int) { return 8, 12 },
		MaxSessions:   2,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bossLease, err := connector.owners.Acquire("boss")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(bossLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	memberLease, err := connector.owners.Acquire("member")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(memberLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	bossConn := &worldConnection{game: connector, lease: bossLease, ready: true, events: make(chan string, 4)}
	memberConn := &worldConnection{game: connector, lease: memberLease, ready: true, events: make(chan string, 4)}
	connector.mu.Lock()
	connector.connections[bossConn] = struct{}{}
	connector.connections[memberConn] = struct{}{}
	connector.mu.Unlock()

	deleted, err := bossConn.Submit(context.Background(), "패거리공지 d")
	if err != nil || deleted != world.FamilyNewsDeletedResponse || store.commits != 1 {
		t.Fatalf("delete=%q err=%v commits=%d", deleted, err, store.commits)
	}
	select {
	case event := <-memberConn.events:
		t.Fatalf("delete fanned out: %q", event)
	default:
	}

	replay, err := bossConn.Submit(context.Background(), "패거리공지 d")
	if err != nil || replay != deleted {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-memberConn.events:
		t.Fatalf("replay fanned out: %q", event)
	default:
	}
}

func TestWorldConnectorFamilyNewsDeleteRejectsNonBossWithoutCommit(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyNewsState(), "member", "boss")
	connector.config.FamilyCatalog = world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}}

	denied, err := connections[0].Submit(context.Background(), "패거리공지 d")
	if err != nil || denied != world.FamilyNewsNotBossResponse {
		t.Fatalf("member delete=%q err=%v", denied, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.FamilyNews.Bodies[2] != world.FamilyNewsHeader+"기존\n" {
		t.Fatalf("non-boss delete mutated news: %q", saved.FamilyNews.Bodies[2])
	}
}

func TestWorldConnectorSubmitDispatchesFamilyNewsAppendContinuation(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorFamilyNewsState())
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:         store,
		WorldID:       "family-news-append",
		Clock:         func() (int32, int) { return 8, 12 },
		MaxSessions:   1,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("member")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.mu.Lock()
	connector.connections[conn] = struct{}{}
	connector.mu.Unlock()

	prompt, err := conn.Submit(context.Background(), "패거리공지 a")
	if err != nil || prompt != world.FamilyNewsAppendPrompt || store.commits != 0 {
		t.Fatalf("prompt=%q err=%v commits=%d", prompt, err, store.commits)
	}

	continued, err := conn.Submit(context.Background(), "새 공지")
	if err != nil || continued != world.FamilyNewsAppendContinuePrompt || store.commits != 1 {
		t.Fatalf("line=%q err=%v commits=%d", continued, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved.FamilyNews.Bodies[2], "기존\n") || !strings.Contains(saved.FamilyNews.Bodies[2], "새 공지\n") {
		t.Fatalf("body=%q", saved.FamilyNews.Bodies[2])
	}

	done, err := conn.Submit(context.Background(), ".")
	if err != nil || done != world.FamilyNewsAppendDoneResponse || store.commits != 1 {
		t.Fatalf("done=%q err=%v commits=%d", done, err, store.commits)
	}

	viewed, err := conn.Submit(context.Background(), "패거리공지")
	if err != nil || !strings.Contains(viewed, "새 공지") || store.commits != 2 {
		t.Fatalf("view=%q err=%v commits=%d", viewed, err, store.commits)
	}
	select {
	case event := <-conn.events:
		t.Fatalf("append fanned out: %q", event)
	default:
	}

	invalid, err := conn.Submit(context.Background(), "패거리공지 z")
	if err != nil || invalid != world.FamilyNewsInvalidOptionResponse {
		t.Fatalf("invalid=%q err=%v", invalid, err)
	}
}

func TestWorldConnectorFamilyNewsAppendReplaySuppressesDuplicateCommit(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorFamilyNewsState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:         store,
		WorldID:       "family-news-append-replay",
		Clock:         func() (int32, int) { return 8, 12 },
		MaxSessions:   1,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("member")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.mu.Lock()
	connector.connections[conn] = struct{}{}
	connector.mu.Unlock()

	if prompt, err := conn.Submit(context.Background(), "패거리공지 a"); err != nil || prompt != world.FamilyNewsAppendPrompt {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
	if line, err := conn.Submit(context.Background(), "한 줄"); err != nil || line != world.FamilyNewsAppendContinuePrompt || store.commits != 1 {
		t.Fatalf("line=%q err=%v commits=%d", line, err, store.commits)
	}
	replay, err := conn.Submit(context.Background(), "한 줄")
	if err != nil || replay != world.FamilyNewsAppendContinuePrompt {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("append replay committed again: commits=%d", commits)
	}
	select {
	case event := <-conn.events:
		t.Fatalf("append replay fanned out: %q", event)
	default:
	}
}

func TestWorldConnectorFamilyNewsAppendDoesNotPrintPromptWhenPersistFails(t *testing.T) {
	raw, err := json.Marshal(connectorFamilyNewsState())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &composeTransientStore{base: base, fail: true}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "family-news-append-fail", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("member")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if prompt, err := conn.Submit(context.Background(), "패거리공지 a"); err != nil || prompt != world.FamilyNewsAppendPrompt {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
	failed, err := conn.Submit(context.Background(), "저장 실패 줄")
	if err != nil {
		t.Fatal(err)
	}
	if failed == world.FamilyNewsAppendContinuePrompt || failed == world.FamilyNewsAppendDoneResponse {
		t.Fatalf("printed success for unsaved line: %q", failed)
	}
	if conn.compose == nil || conn.compose.kind != composeFamilyNewsAppend {
		t.Fatalf("editor dropped after persist failure: %+v", conn.compose)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.FamilyNews.Bodies[2], "저장 실패 줄") {
		t.Fatalf("failed line persisted: %q", saved.FamilyNews.Bodies[2])
	}

	done, err := conn.Submit(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if done == world.FamilyNewsAppendDoneResponse || done == world.FamilyNewsAppendContinuePrompt {
		t.Fatalf("printed done/prompt when line was not saved: %q", done)
	}
	if strings.Contains(done, "공지를 남겼습니다") || strings.Contains(done, "->") {
		t.Fatalf("success marker in unsaved exit: %q", done)
	}
}

func TestWorldConnectorFamilyNewsAppendPinsCommandIDAcrossRetry(t *testing.T) {
	raw, err := json.Marshal(connectorFamilyNewsState())
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &composeTransientStore{base: base, fail: true}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "family-news-append-retry", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("member")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if _, err := conn.Submit(context.Background(), "패거리공지 a"); err != nil {
		t.Fatal(err)
	}
	first, err := conn.Submit(context.Background(), "한 줄")
	if err != nil || first == world.FamilyNewsAppendContinuePrompt {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := conn.Submit(context.Background(), "한 줄")
	if err != nil || second != world.FamilyNewsAppendContinuePrompt || store.commitSeen != 1 {
		t.Fatalf("second=%q err=%v seen=%d attempts=%q", second, err, store.commitSeen, store.attempts)
	}
	if len(store.attempts) != 2 || store.attempts[0] != store.attempts[1] {
		t.Fatalf("command IDs were not pinned: %q", store.attempts)
	}
	saved, err := world.DecodeState(base.state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved.FamilyNews.Bodies[2], "한 줄\n") {
		t.Fatalf("body=%q", saved.FamilyNews.Bodies[2])
	}
}

func TestWorldConnectorFamilyNewsAppendMapsLimitWithoutSuccessPrompt(t *testing.T) {
	state := connectorFamilyNewsState()
	state.FamilyNews.Bodies[2] = strings.Repeat("a", world.MaxFamilyNewsBytes)
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "family-news-limit", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("member")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	if _, err := conn.Submit(context.Background(), "패거리공지 a"); err != nil {
		t.Fatal(err)
	}
	limited, err := conn.Submit(context.Background(), "넘침")
	if err != nil {
		t.Fatal(err)
	}
	if limited == world.FamilyNewsAppendContinuePrompt || limited == world.FamilyNewsAppendDoneResponse || strings.Contains(limited, "아직 구현되지 않은 명령") {
		t.Fatalf("limit response=%q", limited)
	}
	if conn.compose == nil {
		t.Fatal("limit cleared the editor")
	}
	if store.commits != 0 {
		t.Fatalf("limit committed: %d", store.commits)
	}
}
