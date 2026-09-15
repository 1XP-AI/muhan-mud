package transport

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresFifthLanesAbilityTitlePersistAndReplay(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	const roomID int16 = 200
	players := []string{"haste", "pray", "prepare", "up-dmg", "title"}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{roomID: serviceRoomState(roomID, "광장", [8]byte{}, players...)},
		Players: map[string]world.PlayerState{
			"haste":   {Body: world.LegacyMonster{Name: "Haste", Type: 0, Class: world.RangerClass, Level: 4, RoomID: roomID}, Online: true},
			"pray":    {Body: world.LegacyMonster{Name: "Pray", Type: 0, Class: world.ClericClass, Level: 4, RoomID: roomID}, Online: true},
			"prepare": {Body: world.LegacyMonster{Name: "Prepare", Type: 0, Class: 4, Level: 4, RoomID: roomID}, Online: true},
			"up-dmg":  {Body: world.LegacyMonster{Name: "UpDmg", Type: 0, Class: world.InvincibleClass, Level: 4, RoomID: roomID}, Online: true},
			"title":   {Body: world.LegacyMonster{Name: "Title", Type: 0, Class: 4, Level: 4, RoomID: roomID}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-fifth-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	roll := func(low, high int) int {
		if low == 1 && high == 100 {
			return 1
		}
		return high
	}

	hasteLease := serviceCommandLease(t, owners, "haste")
	firstHaste, err := owners.ExecuteRangerPrayLine(ctx, store, worldID, "fifth-haste-1", hasteLease, "활보법", session.RangerPrayOptions{Now: 2000, Roll: roll})
	if err != nil || firstHaste.Replayed || firstHaste.Revision != 1 {
		t.Fatalf("haste=%+v err=%v", firstHaste, err)
	}
	replayHaste, err := owners.ExecuteRangerPrayLine(ctx, store, worldID, "fifth-haste-1", hasteLease, "활보법", session.RangerPrayOptions{Now: 2000, Roll: func(int, int) int { t.Fatal("haste replay rerolled"); return 0 }})
	if err != nil || !replayHaste.Replayed || string(replayHaste.Response) != string(firstHaste.Response) {
		t.Fatalf("haste replay=%+v err=%v", replayHaste, err)
	}

	prayLease := serviceCommandLease(t, owners, "pray")
	if receipt, err := owners.ExecutePrayLine(ctx, store, worldID, "fifth-pray-1", prayLease, "신원법", 2000, roll); err != nil || receipt.Revision != 2 {
		t.Fatalf("pray=%+v err=%v", receipt, err)
	}
	prepareLease := serviceCommandLease(t, owners, "prepare")
	if receipt, err := owners.ExecutePrepareLine(ctx, store, worldID, "fifth-prepare-1", prepareLease, "경계", 2000); err != nil || receipt.Revision != 3 {
		t.Fatalf("prepare=%+v err=%v", receipt, err)
	}
	upDmgLease := serviceCommandLease(t, owners, "up-dmg")
	if receipt, err := owners.ExecuteUpDmgLineWithOptions(ctx, store, worldID, "fifth-up-dmg-1", upDmgLease, "잠력격발", session.UpDmgOptions{Now: 2000, Roll: roll}); err != nil || receipt.Revision != 4 {
		t.Fatalf("up_dmg=%+v err=%v", receipt, err)
	}
	titleLease := serviceCommandLease(t, owners, "title")
	if receipt, err := owners.ExecuteTitleLine(ctx, store, worldID, "fifth-title-1", titleLease, "칭호 새 수호자"); err != nil || receipt.Revision != 5 {
		t.Fatalf("title=%+v err=%v", receipt, err)
	}

	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 5 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["title"].Title != "새 수호자" || saved.Players["haste"].Body.Flags[world.HasteFlag/8]&(1<<(world.HasteFlag%8)) == 0 {
		t.Fatalf("saved fifth lanes state=%+v", saved.Players)
	}
}
