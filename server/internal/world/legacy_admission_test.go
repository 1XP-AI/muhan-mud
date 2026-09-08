package world

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestAdmitLegacyRoomRequiresExplicitPolicyForNoncanonicalText(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00100")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdmitLegacyRoom(raw, LegacyRoomAdmissionPolicy{}); err == nil {
		t.Fatal("admitted invalid text without policy")
	} else {
		var issueErr LegacyRoomAdmissionError
		if !errors.As(err, &issueErr) || issueErr.Issue.Kind != "invalid-euc-kr" {
			t.Fatalf("wrong policy error: %v", err)
		}
	}
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("strict decoder accepted noncanonical text")
	}
}

func TestAdmitLegacyRoomPreservesSourceEvidence(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00100")
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), raw...)
	admitted, err := AdmitLegacyRoom(raw, LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if len(admitted.Evidence.Issues) == 0 || !bytes.Equal(admitted.Evidence.Source, before) {
		t.Fatal("missing original source evidence")
	}
	raw[0] ^= 0xff
	if !bytes.Equal(admitted.Evidence.Source, before) {
		t.Fatal("admission aliases caller bytes")
	}
	if admitted.Room.ID != 100 || admitted.Evidence.Consumed == 0 {
		t.Fatalf("unexpected admitted room: %+v", admitted)
	}
}

func TestAdmitLegacyRoomNeverRelaxesStructuralValidation(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdmitLegacyRoom(raw[:len(raw)-1], LegacyRoomCompatibilityPolicy); err == nil {
		t.Fatal("compatibility policy accepted truncation")
	}
}

func TestAdmitLegacyRoomQuarantinesEmptyMonsterPlaceholders(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r06/r06406")
	if err != nil {
		t.Fatal(err)
	}
	policy := LegacyRoomAdmissionPolicy{}
	if _, err := AdmitLegacyRoom(raw, policy); err == nil {
		t.Fatal("admitted empty monster placeholder without policy")
	} else {
		var issueErr LegacyRoomAdmissionError
		if !errors.As(err, &issueErr) || issueErr.Issue.Kind != "empty-monster-placeholder" {
			t.Fatalf("wrong placeholder policy error: %v", err)
		}
	}
	admitted, err := AdmitLegacyRoom(raw, LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	for _, monster := range admitted.Room.Monsters {
		if monster.Name == "" {
			t.Fatal("empty monster placeholder leaked into admitted runtime room")
		}
	}
	found := false
	for _, issue := range admitted.Evidence.Issues {
		found = found || issue.Kind == "empty-monster-placeholder"
	}
	if !found {
		t.Fatal("missing placeholder evidence")
	}
}

func TestLegacyRoomCatalogFollowsCPathAndRecordsHistoricalArtifacts(t *testing.T) {
	catalog, err := LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	// The checked-in tree has 3,216 files, of which 2,341 occupy the exact
	// rooms/r%02d/r%05d path used by load_rom. The remainder are retained as
	// evidence but cannot override the canonical definition.
	if catalog.Len() != 2341 || len(catalog.Ignored()) != 875 {
		t.Fatalf("unexpected catalog shape: %s ignored=%v", catalog, catalog.Ignored()[:smallest(3, len(catalog.Ignored()))])
	}
	room, ok := catalog.Room(1)
	if !ok || room.Name != "무한대전" || len(room.Exits) != 1 || room.Exits[0].Destination != 1001 {
		t.Fatalf("wrong starting room: %+v", room)
	}
	if _, ok := catalog.Room(1001); !ok {
		t.Fatal("canonical fallback room missing")
	}
	room9000, ok := catalog.Room(9000)
	if !ok || room9000.ID != 9000 {
		t.Fatalf("path identity was not applied to room 9000: %+v", room9000)
	}
	evidence9000, ok := catalog.Evidence(9000)
	if !ok || len(evidence9000.Issues) != 1 || evidence9000.Issues[0].Kind != "header-id-mismatch" {
		t.Fatalf("missing room 9000 identity evidence: %+v", evidence9000.Issues)
	}
	state, err := catalog.NewState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Players) != 0 || state.Rooms[1].Resource.Name != "무한대전" {
		t.Fatalf("unexpected initial catalog state: %+v", state.Rooms[1])
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	evidence, ok := catalog.Evidence(1)
	if !ok || len(evidence.Source) == 0 || evidence.SHA256 == [32]byte{} {
		t.Fatal("missing starting-room evidence")
	}
	for _, issue := range catalog.Ignored() {
		if issue.Kind != "noncanonical-path" {
			t.Fatalf("unexpected ignored issue: %+v", issue)
		}
	}
}

func smallest(a, b int) int {
	if a < b {
		return a
	}
	return b
}
