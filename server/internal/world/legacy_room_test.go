package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalStartingRoomHeader(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		t.Fatal(err)
	}
	room, err := DecodeLegacyRoomHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	if room.ID != 1 || room.Name != "무한대전" || len(room.Exits) != 1 || room.Exits[0].Name != "밑" || room.Exits[0].Destination != 1001 || room.BodyOffset != 528 {
		t.Fatalf("wrong original room: %+v", room)
	}
	// Old pointer addresses do not affect semantic output.
	for i := 84; i < 96; i++ {
		raw[i] = 0xff
	}
	again, err := DecodeLegacyRoomHeader(raw)
	if err != nil || again.Name != room.Name || again.ID != room.ID {
		t.Fatal("pointer bytes interpreted")
	}
}

func TestTrackedRoomHeaderCorpus(t *testing.T) {
	paths, err := filepath.Glob("../../../rooms/r*/r?????")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("room corpus missing")
	}
	failed := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeLegacyRoomHeader(raw); err != nil {
			failed++
			if failed <= 3 {
				t.Logf("unsupported header: %s", path)
			}
		}
	}
	if failed > 0 {
		t.Fatalf("unsupported headers: %d/%d; decoder must not be enabled for world admission", failed, len(paths))
	}
	t.Logf("decoded %d original room headers (body not validated)", len(paths))
}

func TestRoomHeaderRejectsTruncationAndCounts(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 82, 479, 480, 483, 527} {
		if _, err := DecodeLegacyRoomHeader(raw[:size]); err == nil {
			t.Fatalf("accepted %d bytes", size)
		}
	}
	for _, count := range []uint32{0xffffffff, 65535} {
		copyRaw := append([]byte{}, raw...)
		binary.LittleEndian.PutUint32(copyRaw[480:], count)
		if _, err := DecodeLegacyRoomHeader(copyRaw); err == nil {
			t.Fatal("invalid exit count accepted")
		}
	}
}

func TestRoomBodyCorpusExceptionAudit(t *testing.T) {
	paths, err := filepath.Glob("../../../rooms/r*/r?????")
	if err != nil || len(paths) == 0 {
		t.Fatal("missing corpus", err)
	}
	if len(paths) != 3216 {
		t.Fatalf("unexpected corpus size: got %d, want 3216", len(paths))
	}
	expected := make(map[string]legacyRoomExceptionFixture, len(legacyRoomExceptionFixtures))
	for _, fixture := range legacyRoomExceptionFixtures {
		if _, exists := expected[fixture.path]; exists {
			t.Fatalf("duplicate fixture path: %s", fixture.path)
		}
		expected[fixture.path] = fixture
	}
	issueKinds := map[string]int{}
	issueRooms := 0
	strictFailures := 0
	totalIssues := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), raw...)
		report, err := InspectLegacyRoom(raw)
		if err != nil {
			t.Fatal(path, err)
		}
		_, strictErr := DecodeLegacyRoom(raw)
		if strictErr != nil {
			strictFailures++
		}
		if !bytes.Equal(raw, before) {
			t.Fatalf("%s: inspection mutated caller bytes", path)
		}
		relative, err := filepath.Rel("../../../rooms", path)
		if err != nil {
			t.Fatal(err)
		}
		relative = filepath.ToSlash(relative)
		fixture, audited := expected[relative]
		if len(report.Issues) == 0 {
			if audited {
				t.Fatalf("fixture unexpectedly has no issues: %s", relative)
			}
			continue
		}
		if !audited {
			t.Fatalf("untracked room exception: %s: %+v", relative, report.Issues)
		}
		issueRooms++
		if len(raw) != fixture.size || report.Consumed != fixture.consumed {
			t.Fatalf("%s: size/consumed drift: got %d/%d, want %d/%d", relative, len(raw), report.Consumed, fixture.size, fixture.consumed)
		}
		digest := sha256.Sum256(raw)
		if fmt.Sprintf("%x", digest) != fixture.sha256 || report.SHA256 != digest || !bytes.Equal(report.Source, raw) {
			t.Fatalf("%s: raw evidence drift", relative)
		}
		if got := legacyRoomIssueSpec(report.Issues); got != fixture.issues {
			t.Fatalf("%s: issue drift: got %q, want %q", relative, got, fixture.issues)
		}
		if strictErr == nil {
			t.Fatalf("%s: strict decoder accepted audited exception", relative)
		}
		if _, err := AdmitLegacyRoom(raw, LegacyRoomAdmissionPolicy{}); err == nil {
			t.Fatalf("%s: zero-value admission policy accepted audited exception", relative)
		}
		for _, issue := range report.Issues {
			issueKinds[issue.Kind]++
			totalIssues++
		}
	}
	if issueRooms != len(legacyRoomExceptionFixtures) {
		t.Fatalf("audited room count drift: got %d, want %d", issueRooms, len(legacyRoomExceptionFixtures))
	}
	if strictFailures != len(legacyRoomExceptionFixtures) {
		t.Fatalf("strict exception count drift: got %d, want %d", strictFailures, len(legacyRoomExceptionFixtures))
	}
	if totalIssues != 100 {
		t.Fatalf("strict issue count drift: got %d, want 100", totalIssues)
	}
	for kind, want := range map[string]int{
		"invalid-euc-kr":          80,
		"missing-text-terminator": 13,
		"trailing-data":           7,
	} {
		if issueKinds[kind] != want {
			t.Fatalf("%s issue count drift: got %d, want %d", kind, issueKinds[kind], want)
		}
	}
}

func legacyRoomIssueSpec(issues []LegacyIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("%s@%d", issue.Kind, issue.Offset))
	}
	return strings.Join(parts, ",")
}
