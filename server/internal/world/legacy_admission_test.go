package world

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/encoding/korean"
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

func TestAdmitLegacyRoomTrailingDataKeepsOriginalBytesAndDecodesThroughConsumed(t *testing.T) {
	if len(legacyTrailingDataAdmissionFixtures) != 7 {
		t.Fatalf("trailing-data conversion fixtures: got %d, want 7", len(legacyTrailingDataAdmissionFixtures))
	}
	seen := map[string]legacyRoomExceptionFixture{}
	for _, fixture := range legacyRoomExceptionFixtures {
		if !trailingDataOnlyIssueSpec(fixture.issues) {
			continue
		}
		seen[fixture.path] = fixture
	}
	if len(seen) != 7 {
		t.Fatalf("audit trailing-data-only rooms: got %d, want 7", len(seen))
	}
	for _, fixture := range legacyTrailingDataAdmissionFixtures {
		audited, ok := seen[fixture.path]
		if !ok || audited != fixture {
			t.Fatalf("conversion fixture drift: %+v vs audit %+v", fixture, audited)
		}
		raw, err := os.ReadFile("../../../rooms/" + fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), raw...)
		report, err := InspectLegacyRoom(raw)
		if err != nil {
			t.Fatal(fixture.path, err)
		}
		if !bytes.Equal(raw, before) {
			t.Fatalf("%s: inspection mutated caller bytes", fixture.path)
		}
		if len(raw) != fixture.size || report.Consumed != fixture.consumed || report.Consumed >= len(raw) {
			t.Fatalf("%s: size/consumed drift: got %d/%d, want %d/%d", fixture.path, len(raw), report.Consumed, fixture.size, fixture.consumed)
		}
		digest := sha256.Sum256(raw)
		if fmt.Sprintf("%x", digest) != fixture.sha256 || report.SHA256 != digest || !bytes.Equal(report.Source, raw) {
			t.Fatalf("%s: raw evidence drift", fixture.path)
		}
		if got := legacyRoomIssueSpec(report.Issues); got != fixture.issues {
			t.Fatalf("%s: issue drift: got %q, want %q", fixture.path, got, fixture.issues)
		}
		if !nonzeroTail(raw[report.Consumed:]) {
			t.Fatalf("%s: leftover tail is empty; conversion must not invent a trim", fixture.path)
		}
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatalf("%s: strict decoder accepted trailing data", fixture.path)
		}
		prefix, err := DecodeLegacyRoom(append([]byte(nil), raw[:report.Consumed]...))
		if err != nil {
			t.Fatalf("%s: prefix through Consumed must remain strictly decodable: %v", fixture.path, err)
		}
		if _, err := AdmitLegacyRoom(raw, LegacyRoomAdmissionPolicy{}); err == nil {
			t.Fatalf("%s: zero-value admission policy accepted trailing data", fixture.path)
		} else {
			var issueErr LegacyRoomAdmissionError
			if !errors.As(err, &issueErr) || issueErr.Issue.Kind != "trailing-data" {
				t.Fatalf("%s: wrong zero-value policy error: %v", fixture.path, err)
			}
		}
		admitted, err := AdmitLegacyRoom(raw, LegacyRoomTrailingDataPolicy)
		if err != nil {
			t.Fatal(fixture.path, err)
		}
		if !bytes.Equal(admitted.Evidence.Source, before) || admitted.Evidence.Consumed != fixture.consumed || len(admitted.Evidence.Source) != fixture.size {
			t.Fatalf("%s: admission truncated or replaced original bytes", fixture.path)
		}
		if admitted.Evidence.SHA256 != digest || !bytes.Equal(admitted.Evidence.Source[admitted.Evidence.Consumed:], before[fixture.consumed:]) {
			t.Fatalf("%s: leftover tail was not preserved as original evidence", fixture.path)
		}
		if got := legacyRoomIssueSpec(admitted.Evidence.Issues); got != fixture.issues {
			t.Fatalf("%s: admitted evidence issue drift: got %q, want %q", fixture.path, got, fixture.issues)
		}
		if !reflect.DeepEqual(admitted.Room, prefix) {
			t.Fatalf("%s: admitted room was not decoded only through Consumed", fixture.path)
		}
		raw[0] ^= 0xff
		if !bytes.Equal(admitted.Evidence.Source, before) {
			t.Fatalf("%s: admission aliases caller bytes", fixture.path)
		}
	}
}

func TestAdmitLegacyRoomTrailingDataExampleR00173LeftoverAfter751(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00173")
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), raw...)
	report, err := InspectLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2295 || report.Consumed != 751 || len(raw)-report.Consumed != 1544 {
		t.Fatalf("r00173 leftover drift: size=%d consumed=%d tail=%d", len(raw), report.Consumed, len(raw)-report.Consumed)
	}
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("DecodeLegacyRoom accepted r00173 trailing data")
	}
	prefix, err := DecodeLegacyRoom(raw[:751])
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := AdmitLegacyRoom(raw, LegacyRoomTrailingDataPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(admitted.Evidence.Source, before) || admitted.Evidence.Consumed != 751 {
		t.Fatal("r00173 admission did not keep original bytes through Consumed 751")
	}
	if !reflect.DeepEqual(admitted.Room, prefix) || admitted.Room.ID != 173 {
		t.Fatalf("r00173 admitted room was not the Consumed prefix: %+v", admitted.Room)
	}
}

func TestAdmitLegacyRoomTrailingDataPolicyRejectsInvalidTextAndMissingTerminator(t *testing.T) {
	if LegacyRoomTrailingDataPolicy.AllowInvalidEUCKR || LegacyRoomTrailingDataPolicy.AllowMissingTextTerminator || !LegacyRoomTrailingDataPolicy.AllowTrailingData {
		t.Fatal("trailing-data policy must not admit other text issues")
	}
	for _, tc := range []struct {
		path string
		kind string
	}{
		{path: "../../../rooms/r00/r00100", kind: "invalid-euc-kr"},
		{path: "../../../rooms/r03/r03438", kind: "missing-text-terminator"},
	} {
		raw, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AdmitLegacyRoom(raw, LegacyRoomTrailingDataPolicy); err == nil {
			t.Fatalf("%s: trailing-data policy admitted %s", tc.path, tc.kind)
		} else {
			var issueErr LegacyRoomAdmissionError
			if !errors.As(err, &issueErr) || issueErr.Issue.Kind != tc.kind {
				t.Fatalf("%s: wrong trailing-data policy error: %v", tc.path, err)
			}
		}
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatalf("%s: strict decoder accepted %s", tc.path, tc.kind)
		}
	}
}

func TestAdmitLegacyRoomMissingTextTerminatorKeepsOriginalBytesAndReadsToFieldBoundary(t *testing.T) {
	if len(legacyMissingTextTerminatorAdmissionFixtures) != 6 {
		t.Fatalf("missing-text-terminator conversion fixtures: got %d, want 6", len(legacyMissingTextTerminatorAdmissionFixtures))
	}
	seen := map[string]legacyRoomExceptionFixture{}
	terminatorIssues := 0
	for _, fixture := range legacyRoomExceptionFixtures {
		for _, part := range strings.Split(fixture.issues, ",") {
			if strings.HasPrefix(part, "missing-text-terminator@") {
				terminatorIssues++
			}
		}
		if !missingTextTerminatorOnlyIssueSpec(fixture.issues) {
			continue
		}
		seen[fixture.path] = fixture
	}
	if terminatorIssues != 13 {
		t.Fatalf("audit missing-text-terminator issues: got %d, want 13", terminatorIssues)
	}
	if len(seen) != 6 {
		t.Fatalf("audit missing-text-terminator-only rooms: got %d, want 6", len(seen))
	}
	for _, fixture := range legacyMissingTextTerminatorAdmissionFixtures {
		audited, ok := seen[fixture.path]
		if !ok || audited != fixture {
			t.Fatalf("conversion fixture drift: %+v vs audit %+v", fixture, audited)
		}
		raw, err := os.ReadFile("../../../rooms/" + fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), raw...)
		report, err := InspectLegacyRoom(raw)
		if err != nil {
			t.Fatal(fixture.path, err)
		}
		if !bytes.Equal(raw, before) {
			t.Fatalf("%s: inspection mutated caller bytes", fixture.path)
		}
		if len(raw) != fixture.size || report.Consumed != fixture.consumed || report.Consumed != len(raw) {
			t.Fatalf("%s: size/consumed drift: got %d/%d, want %d/%d", fixture.path, len(raw), report.Consumed, fixture.size, fixture.consumed)
		}
		digest := sha256.Sum256(raw)
		if fmt.Sprintf("%x", digest) != fixture.sha256 || report.SHA256 != digest || !bytes.Equal(report.Source, raw) {
			t.Fatalf("%s: raw evidence drift", fixture.path)
		}
		if got := legacyRoomIssueSpec(report.Issues); got != fixture.issues {
			t.Fatalf("%s: issue drift: got %q, want %q", fixture.path, got, fixture.issues)
		}
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatalf("%s: strict decoder accepted missing text terminator", fixture.path)
		}
		if _, err := AdmitLegacyRoom(raw, LegacyRoomAdmissionPolicy{}); err == nil {
			t.Fatalf("%s: zero-value admission policy accepted missing text terminator", fixture.path)
		} else {
			var issueErr LegacyRoomAdmissionError
			if !errors.As(err, &issueErr) || issueErr.Issue.Kind != "missing-text-terminator" {
				t.Fatalf("%s: wrong zero-value policy error: %v", fixture.path, err)
			}
		}
		admitted, err := AdmitLegacyRoom(raw, LegacyRoomMissingTextTerminatorPolicy)
		if err != nil {
			t.Fatal(fixture.path, err)
		}
		if !bytes.Equal(admitted.Evidence.Source, before) || admitted.Evidence.Consumed != fixture.consumed || len(admitted.Evidence.Source) != fixture.size {
			t.Fatalf("%s: admission truncated or replaced original bytes", fixture.path)
		}
		if admitted.Evidence.SHA256 != digest {
			t.Fatalf("%s: admission did not keep original SHA-256", fixture.path)
		}
		if got := legacyRoomIssueSpec(admitted.Evidence.Issues); got != fixture.issues {
			t.Fatalf("%s: admitted evidence issue drift: got %q, want %q", fixture.path, got, fixture.issues)
		}
		outputs := collectLegacyUseOutputs(admitted.Room)
		for _, issue := range admitted.Evidence.Issues {
			if issue.Kind != "missing-text-terminator" {
				t.Fatalf("%s: conversion admitted non-terminator issue %+v", fixture.path, issue)
			}
			if issue.Offset < 0 || issue.Offset+legacyCFixedTextFieldBytes > len(before) {
				t.Fatalf("%s: unterminated field at %d exceeds C field boundary", fixture.path, issue.Offset)
			}
			field := admitted.Evidence.Source[issue.Offset : issue.Offset+legacyCFixedTextFieldBytes]
			if bytes.IndexByte(field, 0) >= 0 {
				t.Fatalf("%s: admission synthesized a NUL inside the C field at %d", fixture.path, issue.Offset)
			}
			if !bytes.Equal(field, before[issue.Offset:issue.Offset+legacyCFixedTextFieldBytes]) {
				t.Fatalf("%s: admission changed original field bytes at %d", fixture.path, issue.Offset)
			}
			decoded, err := korean.EUCKR.NewDecoder().Bytes(field)
			if err != nil {
				t.Fatalf("%s: C field-boundary decode failed at %d: %v", fixture.path, issue.Offset, err)
			}
			if !legacyUseOutputsContain(outputs, string(decoded)) {
				t.Fatalf("%s: unterminated field at %d was not admitted through the C field boundary", fixture.path, issue.Offset)
			}
		}
		raw[0] ^= 0xff
		if !bytes.Equal(admitted.Evidence.Source, before) {
			t.Fatalf("%s: admission aliases caller bytes", fixture.path)
		}
	}
}

func TestAdmitLegacyRoomMissingTextTerminatorExampleR03438UseOutputAt15352(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r03/r03438")
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), raw...)
	report, err := InspectLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 23909 || report.Consumed != 23909 || report.Issues[0].Kind != "missing-text-terminator" || report.Issues[0].Offset != 15352 {
		t.Fatalf("r03438 terminator drift: size=%d consumed=%d issues=%v", len(raw), report.Consumed, report.Issues)
	}
	field := raw[15352 : 15352+legacyCFixedTextFieldBytes]
	if bytes.IndexByte(field, 0) >= 0 || field[len(field)-1] != 0xbf {
		t.Fatalf("r03438 use_output is not the original unterminated C field: last=%02x nuls=%d", field[len(field)-1], bytes.Count(field, []byte{0}))
	}
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("DecodeLegacyRoom accepted r03438 missing text terminator")
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(append([]byte(nil), field...))
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := AdmitLegacyRoom(raw, LegacyRoomMissingTextTerminatorPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(admitted.Evidence.Source, before) || admitted.Evidence.Consumed != 23909 {
		t.Fatal("r03438 admission did not keep original bytes")
	}
	if !bytes.Equal(admitted.Evidence.Source[15352:15352+legacyCFixedTextFieldBytes], field) {
		t.Fatal("r03438 admission synthesized a terminator or trimmed the C field")
	}
	if !legacyUseOutputsContain(collectLegacyUseOutputs(admitted.Room), string(decoded)) || admitted.Room.ID != 3438 {
		t.Fatalf("r03438 admitted room did not read use_output to the C field boundary: %+v", admitted.Room.ID)
	}
}

func TestAdmitLegacyRoomMissingTextTerminatorPolicyRejectsInvalidTextAndTrailingData(t *testing.T) {
	if LegacyRoomMissingTextTerminatorPolicy.AllowInvalidEUCKR || LegacyRoomMissingTextTerminatorPolicy.AllowTrailingData || !LegacyRoomMissingTextTerminatorPolicy.AllowMissingTextTerminator {
		t.Fatal("missing-text-terminator policy must not admit other body issues")
	}
	if LegacyRoomTrailingDataPolicy.AllowMissingTextTerminator {
		t.Fatal("trailing-data policy must stay unwidened")
	}
	for _, tc := range []struct {
		path string
		kind string
	}{
		{path: "../../../rooms/r00/r00100", kind: "invalid-euc-kr"},
		{path: "../../../rooms/r00/r00173", kind: "trailing-data"},
		{path: "../../../rooms/r03/r03388", kind: "invalid-euc-kr"},
	} {
		raw, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AdmitLegacyRoom(raw, LegacyRoomMissingTextTerminatorPolicy); err == nil {
			t.Fatalf("%s: missing-text-terminator policy admitted %s", tc.path, tc.kind)
		} else {
			var issueErr LegacyRoomAdmissionError
			if !errors.As(err, &issueErr) || issueErr.Issue.Kind != tc.kind {
				t.Fatalf("%s: wrong missing-text-terminator policy error: %v", tc.path, err)
			}
		}
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatalf("%s: strict decoder accepted %s", tc.path, tc.kind)
		}
	}
}

func TestAdmitLegacyRoomInvalidEUCKRKeepsOriginalBytesAndUsesReplacementPreview(t *testing.T) {
	if len(legacyInvalidEUCKRAdmissionFixtures) != 50 {
		t.Fatalf("invalid-euc-kr conversion fixtures: got %d, want 50", len(legacyInvalidEUCKRAdmissionFixtures))
	}
	seen := map[string]legacyRoomExceptionFixture{}
	eucKRIssues := 0
	for _, fixture := range legacyRoomExceptionFixtures {
		for _, part := range strings.Split(fixture.issues, ",") {
			if strings.HasPrefix(part, "invalid-euc-kr@") {
				eucKRIssues++
			}
		}
		if !hasInvalidEUCKRIssueSpec(fixture.issues) {
			continue
		}
		seen[fixture.path] = fixture
	}
	if eucKRIssues != 80 {
		t.Fatalf("audit invalid-euc-kr issues: got %d, want 80", eucKRIssues)
	}
	if len(seen) != 50 {
		t.Fatalf("audit invalid-euc-kr rooms: got %d, want 50", len(seen))
	}
	for _, fixture := range legacyInvalidEUCKRAdmissionFixtures {
		audited, ok := seen[fixture.path]
		if !ok || audited != fixture {
			t.Fatalf("conversion fixture drift: %+v vs audit %+v", fixture, audited)
		}
		raw, err := os.ReadFile("../../../rooms/" + fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), raw...)
		report, err := InspectLegacyRoom(raw)
		if err != nil {
			t.Fatal(fixture.path, err)
		}
		if !bytes.Equal(raw, before) {
			t.Fatalf("%s: inspection mutated caller bytes", fixture.path)
		}
		if len(raw) != fixture.size || report.Consumed != fixture.consumed || report.Consumed != len(raw) {
			t.Fatalf("%s: size/consumed drift: got %d/%d, want %d/%d", fixture.path, len(raw), report.Consumed, fixture.size, fixture.consumed)
		}
		digest := sha256.Sum256(raw)
		if fmt.Sprintf("%x", digest) != fixture.sha256 || report.SHA256 != digest || !bytes.Equal(report.Source, raw) {
			t.Fatalf("%s: raw evidence drift", fixture.path)
		}
		if got := legacyRoomIssueSpec(report.Issues); got != fixture.issues {
			t.Fatalf("%s: issue drift: got %q, want %q", fixture.path, got, fixture.issues)
		}
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatalf("%s: strict decoder accepted invalid EUC-KR", fixture.path)
		}
		if _, err := AdmitLegacyRoom(raw, LegacyRoomAdmissionPolicy{}); err == nil {
			t.Fatalf("%s: zero-value admission policy accepted invalid EUC-KR", fixture.path)
		} else {
			var issueErr LegacyRoomAdmissionError
			if !errors.As(err, &issueErr) || issueErr.Issue.Kind != "invalid-euc-kr" {
				t.Fatalf("%s: wrong zero-value policy error: %v", fixture.path, err)
			}
		}
		admitted, err := AdmitLegacyRoom(raw, LegacyRoomInvalidEUCKRPolicy)
		if err != nil {
			t.Fatal(fixture.path, err)
		}
		if !bytes.Equal(admitted.Evidence.Source, before) || admitted.Evidence.Consumed != fixture.consumed || len(admitted.Evidence.Source) != fixture.size {
			t.Fatalf("%s: admission truncated or replaced original bytes", fixture.path)
		}
		if admitted.Evidence.SHA256 != digest {
			t.Fatalf("%s: admission did not keep original SHA-256", fixture.path)
		}
		if got := legacyRoomIssueSpec(admitted.Evidence.Issues); got != fixture.issues {
			t.Fatalf("%s: admitted evidence issue drift: got %q, want %q", fixture.path, got, fixture.issues)
		}
		texts := collectLegacyDecodedTexts(admitted.Room)
		for _, issue := range admitted.Evidence.Issues {
			switch issue.Kind {
			case "invalid-euc-kr":
				field := legacyNULTerminatedText(before, issue.Offset)
				if field == nil {
					t.Fatalf("%s: invalid-euc-kr field at %d lost its NUL", fixture.path, issue.Offset)
				}
				if !bytes.Equal(admitted.Evidence.Source[issue.Offset:issue.Offset+len(field)+1], before[issue.Offset:issue.Offset+len(field)+1]) {
					t.Fatalf("%s: admission changed original field bytes at %d", fixture.path, issue.Offset)
				}
				preview, err := korean.EUCKR.NewDecoder().Bytes(append([]byte(nil), field...))
				if err != nil {
					t.Fatalf("%s: documented U+FFFD preview failed at %d: %v", fixture.path, issue.Offset, err)
				}
				if !strings.ContainsRune(string(preview), '\ufffd') {
					t.Fatalf("%s: invalid-euc-kr at %d decoded without U+FFFD", fixture.path, issue.Offset)
				}
				if !legacyDecodedTextsContain(texts, string(preview)) {
					t.Fatalf("%s: U+FFFD preview at %d was not admitted as game text", fixture.path, issue.Offset)
				}
			case "missing-text-terminator":
				if fixture.path != "r03/r03388" {
					t.Fatalf("%s: conversion admitted terminator issue outside mixed room: %+v", fixture.path, issue)
				}
				if issue.Offset < 0 || issue.Offset+legacyCFixedTextFieldBytes > len(before) {
					t.Fatalf("%s: unterminated field at %d exceeds C field boundary", fixture.path, issue.Offset)
				}
				field := admitted.Evidence.Source[issue.Offset : issue.Offset+legacyCFixedTextFieldBytes]
				if bytes.IndexByte(field, 0) >= 0 {
					t.Fatalf("%s: admission synthesized a NUL inside the C field at %d", fixture.path, issue.Offset)
				}
				if !bytes.Equal(field, before[issue.Offset:issue.Offset+legacyCFixedTextFieldBytes]) {
					t.Fatalf("%s: admission changed original unterminated field bytes at %d", fixture.path, issue.Offset)
				}
			default:
				t.Fatalf("%s: conversion admitted non-euc-kr issue %+v", fixture.path, issue)
			}
		}
		raw[0] ^= 0xff
		if !bytes.Equal(admitted.Evidence.Source, before) {
			t.Fatalf("%s: admission aliases caller bytes", fixture.path)
		}
	}
}

func TestAdmitLegacyRoomInvalidEUCKRExampleR00100DescriptionC9A6(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00100")
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), raw...)
	report, err := InspectLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 810 || report.Consumed != 810 || report.Issues[0].Kind != "invalid-euc-kr" || report.Issues[0].Offset != 588 {
		t.Fatalf("r00100 euc-kr drift: size=%d consumed=%d issues=%v", len(raw), report.Consumed, report.Issues)
	}
	if !bytes.Equal(raw[710:712], []byte{0xc9, 0xa6}) {
		t.Fatalf("r00100 description lost original c9 a6 bytes: %x", raw[710:712])
	}
	field := legacyNULTerminatedText(raw, 588)
	if field == nil {
		t.Fatal("r00100 description is not NUL-terminated")
	}
	preview, err := korean.EUCKR.NewDecoder().Bytes(append([]byte(nil), field...))
	if err != nil || !strings.ContainsRune(string(preview), '\ufffd') {
		t.Fatalf("r00100 documented preview must be U+FFFD substitution: err=%v preview=%q", err, preview)
	}
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("DecodeLegacyRoom accepted r00100 invalid EUC-KR")
	}
	admitted, err := AdmitLegacyRoom(raw, LegacyRoomInvalidEUCKRPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(admitted.Evidence.Source, before) || admitted.Evidence.Consumed != 810 {
		t.Fatal("r00100 admission did not keep original bytes")
	}
	if !bytes.Equal(admitted.Evidence.Source[710:712], []byte{0xc9, 0xa6}) {
		t.Fatal("r00100 admission replaced original c9 a6 bytes")
	}
	if admitted.Room.LongDescription != string(preview) || admitted.Room.ID != 100 {
		t.Fatalf("r00100 admitted description invented Hangul or dropped U+FFFD: id=%d desc=%q", admitted.Room.ID, admitted.Room.LongDescription)
	}
	if !strings.ContainsRune(admitted.Room.LongDescription, '\ufffd') {
		t.Fatal("r00100 game string used invented Hangul instead of U+FFFD preview")
	}
}

func TestAdmitLegacyRoomInvalidEUCKRMixedR03388KeepsOriginalBytes(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r03/r03388")
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), raw...)
	report, err := InspectLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 25824 || report.Consumed != 25824 || legacyRoomIssueSpec(report.Issues) != "invalid-euc-kr@1248,invalid-euc-kr@10504,missing-text-terminator@21760" {
		t.Fatalf("r03388 mixed drift: size=%d consumed=%d issues=%v", len(raw), report.Consumed, report.Issues)
	}
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("DecodeLegacyRoom accepted r03388 mixed issues")
	}
	if _, err := AdmitLegacyRoom(raw, LegacyRoomMissingTextTerminatorPolicy); err == nil {
		t.Fatal("missing-text-terminator policy admitted mixed r03388")
	}
	if _, err := AdmitLegacyRoom(raw, LegacyRoomTrailingDataPolicy); err == nil {
		t.Fatal("trailing-data policy admitted mixed r03388")
	}
	admitted, err := AdmitLegacyRoom(raw, LegacyRoomInvalidEUCKRPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if LegacyRoomInvalidEUCKRPolicy.AllowMissingTextTerminator || LegacyRoomInvalidEUCKRPolicy.AllowTrailingData {
		t.Fatal("invalid-euc-kr policy must not set terminator or trailing-data flags")
	}
	if !bytes.Equal(admitted.Evidence.Source, before) || admitted.Evidence.Consumed != 25824 || admitted.Room.ID != 3388 {
		t.Fatal("r03388 admission did not keep original bytes")
	}
	texts := collectLegacyDecodedTexts(admitted.Room)
	for _, offset := range []int{1248, 10504} {
		field := legacyNULTerminatedText(before, offset)
		if field == nil {
			t.Fatalf("r03388 invalid-euc-kr field at %d lost its NUL", offset)
		}
		if !bytes.Equal(admitted.Evidence.Source[offset:offset+len(field)+1], before[offset:offset+len(field)+1]) {
			t.Fatalf("r03388 admission changed original bytes at %d", offset)
		}
		preview, err := korean.EUCKR.NewDecoder().Bytes(append([]byte(nil), field...))
		if err != nil || !strings.ContainsRune(string(preview), '\ufffd') {
			t.Fatalf("r03388 preview at %d must be U+FFFD: err=%v preview=%q", offset, err, preview)
		}
		if !legacyDecodedTextsContain(texts, string(preview)) {
			t.Fatalf("r03388 U+FFFD preview at %d was not admitted as game text", offset)
		}
	}
	term := admitted.Evidence.Source[21760 : 21760+legacyCFixedTextFieldBytes]
	if bytes.IndexByte(term, 0) >= 0 || term[len(term)-1] != 0xbf {
		t.Fatalf("r03388 terminator field was synthesized or trimmed: last=%02x nuls=%d", term[len(term)-1], bytes.Count(term, []byte{0}))
	}
	if !bytes.Equal(term, before[21760:21760+legacyCFixedTextFieldBytes]) {
		t.Fatal("r03388 admission changed original unterminated use_output bytes")
	}
}

func TestAdmitLegacyRoomInvalidEUCKRPolicyRejectsTrailingDataAndTerminatorOnly(t *testing.T) {
	if LegacyRoomInvalidEUCKRPolicy.AllowTrailingData || LegacyRoomInvalidEUCKRPolicy.AllowMissingTextTerminator || !LegacyRoomInvalidEUCKRPolicy.AllowInvalidEUCKR {
		t.Fatal("invalid-euc-kr policy must not admit other body issues")
	}
	if LegacyRoomTrailingDataPolicy.AllowInvalidEUCKR || LegacyRoomMissingTextTerminatorPolicy.AllowInvalidEUCKR {
		t.Fatal("shipped trailing-data and terminator policies must stay unwidened")
	}
	for _, tc := range []struct {
		path string
		kind string
	}{
		{path: "../../../rooms/r00/r00173", kind: "trailing-data"},
		{path: "../../../rooms/r03/r03438", kind: "missing-text-terminator"},
	} {
		raw, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AdmitLegacyRoom(raw, LegacyRoomInvalidEUCKRPolicy); err == nil {
			t.Fatalf("%s: invalid-euc-kr policy admitted %s", tc.path, tc.kind)
		} else {
			var issueErr LegacyRoomAdmissionError
			if !errors.As(err, &issueErr) || issueErr.Issue.Kind != tc.kind {
				t.Fatalf("%s: wrong invalid-euc-kr policy error: %v", tc.path, err)
			}
		}
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatalf("%s: strict decoder accepted %s", tc.path, tc.kind)
		}
	}
}

func hasInvalidEUCKRIssueSpec(spec string) bool {
	if spec == "" {
		return false
	}
	for _, part := range strings.Split(spec, ",") {
		if strings.HasPrefix(part, "invalid-euc-kr@") {
			return true
		}
	}
	return false
}

func legacyNULTerminatedText(raw []byte, offset int) []byte {
	if offset < 0 || offset >= len(raw) {
		return nil
	}
	end := bytes.IndexByte(raw[offset:], 0)
	if end < 0 {
		return nil
	}
	return raw[offset : offset+end]
}

func collectLegacyDecodedTexts(room LegacyRoom) []string {
	var out []string
	add := func(value string) { out = append(out, value) }
	add(room.Name)
	add(room.Track)
	add(room.ShortDescription)
	add(room.LongDescription)
	add(room.ObjectDescription)
	var walk func([]LegacyObject)
	walk = func(objs []LegacyObject) {
		for _, object := range objs {
			add(object.Name)
			add(object.Description)
			add(object.UseOutput)
			for _, key := range object.Keys {
				add(key)
			}
			walk(object.Contents)
		}
	}
	walk(room.Objects)
	for _, monster := range room.Monsters {
		add(monster.Name)
		add(monster.Description)
		add(monster.Talk)
		for _, key := range monster.Keys {
			add(key)
		}
		walk(monster.Inventory)
	}
	return out
}

func legacyDecodedTextsContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func trailingDataOnlyIssueSpec(spec string) bool {
	if spec == "" {
		return false
	}
	for _, part := range strings.Split(spec, ",") {
		if !strings.HasPrefix(part, "trailing-data@") {
			return false
		}
	}
	return true
}

func missingTextTerminatorOnlyIssueSpec(spec string) bool {
	if spec == "" {
		return false
	}
	for _, part := range strings.Split(spec, ",") {
		if !strings.HasPrefix(part, "missing-text-terminator@") {
			return false
		}
	}
	return true
}

func collectLegacyUseOutputs(room LegacyRoom) []string {
	var out []string
	var walk func([]LegacyObject)
	walk = func(objs []LegacyObject) {
		for _, object := range objs {
			out = append(out, object.UseOutput)
			walk(object.Contents)
		}
	}
	walk(room.Objects)
	for _, monster := range room.Monsters {
		walk(monster.Inventory)
	}
	return out
}

func legacyUseOutputsContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func nonzeroTail(tail []byte) bool {
	for _, b := range tail {
		if b != 0 {
			return true
		}
	}
	return false
}

func smallest(a, b int) int {
	if a < b {
		return a
	}
	return b
}
