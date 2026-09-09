package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"
)

func readPlayerSnapshotFixture(t *testing.T, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile("../../../tests/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	encoded = bytes.TrimSpace(encoded)
	raw := make([]byte, hex.DecodedLen(len(encoded)))
	if _, err := hex.Decode(raw, encoded); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return raw
}

func TestPlayerSnapshotV1FixturesAreCanonicalAndPortable(t *testing.T) {
	cases := []struct {
		name       string
		playerName string
		objects    int
		sha256     string
	}{
		{name: "player_snapshot_v1_legacy_decoder_canonical.hex", playerName: "s1-fixture", objects: 2, sha256: "695b0958cc72fee809f513c41cb043d2608d3b70355c6181eb2cf2a054c50ed4"},
		{name: "player_snapshot_v1_legacy_decoder_minimal.hex", playerName: "s1-minimal", objects: 0, sha256: "679b58c3e3c176edd2871dd624536165321043b2d241339883ceb74e915ed377"},
		{name: "player_snapshot_v1_legacy_decoder_persisted_graph.hex", playerName: "s1-persisted", objects: 4, sha256: "23a84447b99bbdeb4f2d976d742a2e991cb1a822ce880e352883afc267cc86e8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := readPlayerSnapshotFixture(t, tc.name)
			inspection, err := InspectPlayerSnapshotV1(raw)
			if err != nil {
				t.Fatal(err)
			}
			if inspection.SHA256 != sha256.Sum256(raw) {
				t.Fatal("inspection digest does not cover source bytes")
			}
			if got := hex.EncodeToString(inspection.SHA256[:]); got != tc.sha256 {
				t.Fatalf("source digest=%s want=%s", got, tc.sha256)
			}
			if got := string(inspection.Snapshot.Name[:bytes.IndexByte(inspection.Snapshot.Name[:], 0)]); got != tc.playerName {
				t.Fatalf("name=%q want=%q", got, tc.playerName)
			}
			if got := len(inspection.Snapshot.Inventory.Nodes); got != tc.objects {
				t.Fatalf("inventory nodes=%d want=%d", got, tc.objects)
			}
			canonical, err := EncodePlayerSnapshotV1(inspection.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonical, raw) {
				t.Fatal("fixture is not byte-for-byte canonical after Go re-encoding")
			}
		})
	}
}

func TestPlayerSnapshotV1AdditionalCanonicalFixturesRoundTrip(t *testing.T) {
	for _, name := range []string{
		"player_snapshot_v1_canonical.hex",
		"player_snapshot_v1_one_inventory_item.hex",
		"player_snapshot_v1_tree_inventory.hex",
	} {
		t.Run(name, func(t *testing.T) {
			raw := readPlayerSnapshotFixture(t, name)
			snapshot, err := DecodePlayerSnapshotV1(raw)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := EncodePlayerSnapshotV1(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, canonical) {
				t.Fatalf("%s changed during canonical re-encoding", name)
			}
		})
	}
}

func TestPlayerSnapshotV1RejectsEnvelopeAndGraphTampering(t *testing.T) {
	canonical := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_canonical.hex")
	cases := []struct {
		name   string
		mutate func([]byte)
		want   error
	}{
		{name: "digest", mutate: func(raw []byte) { raw[len(raw)-1] ^= 0x01 }, want: ErrPlayerSnapshotDigestMismatch},
		{name: "magic", mutate: func(raw []byte) { raw[0] ^= 0x01 }, want: ErrPlayerSnapshotMalformed},
	}
	// Truncation and trailing bytes resize the input and are checked explicitly;
	// a closure cannot replace a caller-owned slice.
	truncated := append([]byte(nil), canonical[:len(canonical)-1]...)
	if _, err := DecodePlayerSnapshotV1(truncated); !errors.Is(err, ErrPlayerSnapshotTruncated) {
		t.Fatalf("truncated err=%v", err)
	}
	trailing := append(append([]byte(nil), canonical...), 0x7f)
	if _, err := DecodePlayerSnapshotV1(trailing); !errors.Is(err, ErrPlayerSnapshotTrailingBytes) {
		t.Fatalf("trailing err=%v", err)
	}
	for _, tc := range cases[2:] {
		t.Run(tc.name, func(t *testing.T) {
			raw := append([]byte(nil), canonical...)
			tc.mutate(raw)
			_, err := DecodePlayerSnapshotV1(raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}

	decoded, err := DecodePlayerSnapshotV1(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Inventory.Nodes) != 2 || decoded.Inventory.Nodes[1].ParentIndex == nil || *decoded.Inventory.Nodes[1].ParentIndex != 0 {
		t.Fatalf("unexpected canonical graph: %+v", decoded.Inventory.Nodes)
	}
	badGraph := decoded
	badGraph.Inventory.Nodes[1].ChildIndex = 1
	if _, err := EncodePlayerSnapshotV1(badGraph); !errors.Is(err, ErrPlayerSnapshotGraphInvalid) {
		t.Fatalf("bad sibling index err=%v", err)
	}
}

func TestPlayerSnapshotV1ConvertsToCanonicalGoPlayerAndItems(t *testing.T) {
	raw := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_canonical.hex")
	snapshot, err := DecodePlayerSnapshotV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	nextID := 0
	state, err := snapshot.ToPlayerState(func() (string, error) {
		nextID++
		return "item-" + string(rune('0'+nextID)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Body.Name != "s1-fixture" || state.Body.HPMax != 20 || state.Body.HPCurrent != 20 || state.Body.MPMax != 10 || state.Body.MPCurrent != 10 {
		t.Fatalf("body scalar conversion=%+v", state.Body)
	}
	if state.Body.Experience != 123456 || state.Body.Gold != -789 || state.Body.RoomID != -9 {
		t.Fatalf("body numeric conversion=%+v", state.Body)
	}
	if state.Items == nil || len(state.Items.Items) != 2 || !reflect.DeepEqual(state.Items.Inventory, []string{"item-1"}) {
		t.Fatalf("items=%+v", state.Items)
	}
	if err := state.Items.Validate(); err != nil {
		t.Fatal(err)
	}
	root := state.Items.Items["item-1"]
	if root.Object.Name != "s1-root" || !reflect.DeepEqual(root.Contents, []string{"item-2"}) {
		t.Fatalf("unexpected root=%+v", root)
	}
}

func TestPlayerSnapshotV1ConversionRejectsNarrowLegacyOverflow(t *testing.T) {
	raw := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_minimal.hex")
	snapshot, err := DecodePlayerSnapshotV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Experience = math.MaxInt32 + 1
	if _, err := snapshot.ToLegacyMonster(); !errors.Is(err, ErrPlayerSnapshotNumericOverflow) {
		t.Fatalf("experience overflow err=%v", err)
	}
	snapshot.Experience = 0
	snapshot.Daily[0].LastTime = math.MaxInt32 + 1
	if _, err := snapshot.ToLegacyMonster(); !errors.Is(err, ErrPlayerSnapshotNumericOverflow) {
		t.Fatalf("daily overflow err=%v", err)
	}
}

func TestAdmitPlayerSnapshotRequiresExplicitIdentityAndPreservesOfflineState(t *testing.T) {
	raw := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_minimal.hex")
	snapshot, err := DecodePlayerSnapshotV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	base := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}},
		},
		Players: map[string]PlayerState{},
	}
	snapshot.RoomNumber = 1
	got, err := base.AdmitPlayerSnapshot("legacy-player-1", snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	player, ok := got.Players["legacy-player-1"]
	if !ok || player.Online || player.Body.Name != "S1-minimal" || player.Items == nil {
		t.Fatalf("admitted player=%+v ok=%v", player, ok)
	}
	if len(got.Rooms[1].PlayerIDs) != 0 {
		t.Fatal("offline imported player must not occupy a room")
	}
	if _, err := base.AdmitPlayerSnapshot("", snapshot, nil); !errors.Is(err, ErrPlayerSnapshotAdmission) {
		t.Fatalf("missing explicit ID err=%v", err)
	}
	if _, err := got.AdmitPlayerSnapshot("another-id", snapshot, nil); !errors.Is(err, ErrPlayerSnapshotIdentityConflict) {
		t.Fatalf("duplicate canonical name err=%v", err)
	}
	snapshot.RoomNumber = 99
	if _, err := base.AdmitPlayerSnapshot("legacy-player-2", snapshot, nil); !errors.Is(err, ErrPlayerSnapshotAdmission) {
		t.Fatalf("missing room err=%v", err)
	}
}

func TestAdmitPlayerSnapshotDoesNotPublishOnAllocatorFailure(t *testing.T) {
	raw := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_canonical.hex")
	snapshot, err := DecodePlayerSnapshotV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	base := State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}}},
		Players: map[string]PlayerState{},
	}
	before := base
	_, err = base.AdmitPlayerSnapshot("legacy-player-1", snapshot, func() (string, error) {
		return "", errors.New("allocator unavailable")
	})
	if !errors.Is(err, ErrPlayerSnapshotItemConversion) {
		t.Fatalf("allocator err=%v", err)
	}
	if !reflect.DeepEqual(base, before) {
		t.Fatal("failed admission changed source state")
	}
}
