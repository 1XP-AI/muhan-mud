package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestValidateFullDataDryRunFlagsRequiresExplicitMode(t *testing.T) {
	if _, err := validateFullDataDryRunFlags(false, "", "", "family.json", "", "", ""); !errors.Is(err, errFullDataDryRunOptInRequired) {
		t.Fatalf("source without opt-in err=%v", err)
	}
	options, err := validateFullDataDryRunFlags(true, "rooms", "players.json", "family.json", "memos.json", "bank.json", "vote.json")
	if err != nil {
		t.Fatalf("enabled options err=%v", err)
	}
	if options.RoomsPath != "rooms" || options.PlayerManifestPath != "players.json" || options.SocialFamilyManifestPath != "family.json" || options.SocialMemoManifestPath != "memos.json" || options.BankManifestPath != "bank.json" || options.VoteManifestPath != "vote.json" {
		t.Fatalf("enabled options=%+v", options)
	}
}

func TestRunFullDataDryRunReportsNamedMissingFamilies(t *testing.T) {
	report, err := RunFullDataDryRun(fullDataDryRunOptions{})
	if err == nil {
		t.Fatal("empty source set unexpectedly passed")
	}
	if report.Status != "incomplete" || report.Result != "missing-input" || report.Complete || report.ExitCode == 0 {
		t.Fatalf("missing-input report=%+v", report)
	}
	wantFamilies := []string{
		fullDataFamilyRooms,
		fullDataFamilyNPCItems,
		fullDataFamilyPlayers,
		fullDataFamilySocialNames,
		fullDataFamilySocialMemos,
		fullDataFamilySocial,
		fullDataFamilyBank,
		fullDataFamilyVote,
	}
	for _, family := range wantFamilies {
		if !containsFullDataString(report.MissingSourceFamilies, family) {
			t.Fatalf("missing source family %q not reported: %v", family, report.MissingSourceFamilies)
		}
	}
	if report.Validation.Status != "incomplete" {
		t.Fatalf("missing source validation status=%q", report.Validation.Status)
	}
	if report.DatabaseOpened || report.ListenerStarted || report.OutputsWritten {
		t.Fatalf("empty dry-run crossed side-effect boundary: %+v", report)
	}
	if report.Sources[3].Diagnostics == nil || len(report.Sources[3].Diagnostics) == 0 {
		t.Fatalf("social source diagnostics did not identify missing manifests: %+v", report.Sources[3])
	}
}

func TestRunFullDataDryRunOutputIsDeterministic(t *testing.T) {
	roomsPath := filepath.Join(fullDataTestRepoRoot(t), "rooms")
	options := fullDataDryRunOptions{RoomsPath: roomsPath}
	first, firstErr := RunFullDataDryRun(options)
	second, secondErr := RunFullDataDryRun(options)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("partial source set unexpectedly passed: first=%v second=%v", firstErr, secondErr)
	}
	firstRaw, err := MarshalFullDataDryRunReport(first)
	if err != nil {
		t.Fatalf("marshal first report: %v", err)
	}
	secondRaw, err := MarshalFullDataDryRunReport(second)
	if err != nil {
		t.Fatalf("marshal second report: %v", err)
	}
	if !bytes.Equal(firstRaw, secondRaw) {
		t.Fatalf("reports differ across identical runs:\nfirst=%s\nsecond=%s", firstRaw, secondRaw)
	}
	if first.Sources[0].Status != "valid" || first.Sources[0].SourceSHA256 == "" || first.Sources[0].Counts.CanonicalRooms == 0 {
		t.Fatalf("reviewed rooms evidence missing: %+v", first.Sources[0])
	}
	if first.Sources[1].Status != "valid" || first.Sources[1].CanonicalSHA256 == "" || first.Sources[1].Counts.ItemNodes == 0 {
		t.Fatalf("canonical NPC/item graph evidence missing: %+v", first.Sources[1])
	}
}

func TestFinishFullDataCrossChecksSurfacesOwnershipAndDuplicateResults(t *testing.T) {
	report := newFullDataDryRunReport(fullDataDryRunOptions{})
	aggregate := fullDataAggregate{
		itemOwners: map[string][]string{"item-1": {"room:1", "player:p1"}},
		playerIDs:  map[string]string{"p1": "player"},
		playerRefs: map[string][]string{},
		worldIDs:   map[string]string{},
	}
	finishFullDataCrossChecks(&report, &aggregate)
	if report.Validation.Status != "fail" {
		t.Fatalf("ownership conflict did not fail validation: %+v", report.Validation)
	}
	if !containsFullDataString(report.Validation.DuplicateItemIDs, "item-1") {
		t.Fatalf("duplicate item result missing: %+v", report.Validation)
	}
	if len(report.Validation.OwnershipErrors) != 1 || !strings.Contains(report.Validation.OwnershipErrors[0], "item-1") {
		t.Fatalf("ownership result missing: %+v", report.Validation.OwnershipErrors)
	}
}

func TestFinishFullDataCrossChecksSurfacesCanonicalIdentityNameDivergence(t *testing.T) {
	report := newFullDataDryRunReport(fullDataDryRunOptions{})
	aggregate := fullDataAggregate{
		itemOwners:  map[string][]string{},
		playerIDs:   map[string]string{"p1": "player"},
		playerNames: map[string]string{"p1": "Alice"},
		playerRefs:  map[string][]string{"p1": {"bank-owner", "social-family-member", "social-memo-sender", "vote-ballot"}},
		playerNameRefs: map[string][]fullDataNamedPlayerReference{
			"p1": {
				{Source: "social-family-member", Name: "Carol"},
				{Source: "social-memo-sender", Name: "Dave"},
				{Source: "bank-owner", Name: "Bob"},
				{Source: "vote-ballot", Name: "Eve"},
			},
		},
		worldIDs: map[string]string{},
	}
	finishFullDataCrossChecks(&report, &aggregate)
	if report.Validation.Status != "fail" {
		t.Fatalf("identity divergence did not fail validation: %+v", report.Validation)
	}
	want := []string{
		"player p1 name mismatch: player=Alice bank-owner=Bob",
		"player p1 name mismatch: player=Alice social-family-member=Carol",
		"player p1 name mismatch: player=Alice social-memo-sender=Dave",
		"player p1 name mismatch: player=Alice vote-ballot=Eve",
	}
	if len(report.Validation.IdentityMismatches) != len(want) {
		t.Fatalf("identity mismatch count=%v want=%v", report.Validation.IdentityMismatches, want)
	}
	for index, expected := range want {
		if report.Validation.IdentityMismatches[index] != expected {
			t.Fatalf("identity mismatch[%d]=%q want=%q", index, report.Validation.IdentityMismatches[index], expected)
		}
	}
}

func TestFullDataDryRunRequiresNamedCompletionEvidenceGate(t *testing.T) {
	report := newFullDataDryRunReport(fullDataDryRunOptions{})
	report.MissingSourceFamilies = nil
	report.Validation.Status = "pass"
	for index := range report.Sources {
		report.Sources[index].Status = "valid"
	}
	if err := finishFullDataDryRunResult(&report, false); err != nil {
		t.Fatalf("local validation unexpectedly failed: %v", err)
	}
	if report.Status != "incomplete" || report.Result != "local-input-validation" || report.Complete || report.ExitCode != 0 {
		t.Fatalf("local-only report=%+v", report)
	}
	if report.EvidenceGate.Name != fullDataCompletionEvidenceGate || report.EvidenceGate.Satisfied || report.EvidenceGate.Status != "not-satisfied" {
		t.Fatalf("evidence gate=%+v", report.EvidenceGate)
	}
	if !hasFullDataCheck(report.Validation.Checks, fullDataCompletionEvidenceGate, "incomplete") {
		t.Fatalf("missing unsatisfied evidence check: %+v", report.Validation.Checks)
	}

	complete := newFullDataDryRunReport(fullDataDryRunOptions{})
	complete.MissingSourceFamilies = nil
	complete.Validation.Status = "pass"
	for index := range complete.Sources {
		complete.Sources[index].Status = "valid"
	}
	if err := finishFullDataDryRunResult(&complete, true); err != nil {
		t.Fatalf("satisfied evidence gate failed: %v", err)
	}
	if complete.Status != "pass" || complete.Result != "validated" || !complete.Complete || complete.ExitCode != 0 || !complete.EvidenceGate.Satisfied {
		t.Fatalf("gated report=%+v", complete)
	}
}

func TestNPCItemGraphCheckFailsOnItemLoss(t *testing.T) {
	if got := npcItemGraphCheckStatus(10, 9); got != "fail" {
		t.Fatalf("item loss status=%q want fail", got)
	}
	if got := npcItemGraphCheckStatus(10, 10); got != "pass" {
		t.Fatalf("matching item count status=%q want pass", got)
	}
}

func TestInspectFullDataPlayersRejectsSnapshotIdentityAndRoomDrift(t *testing.T) {
	t.Run("snapshot name", func(t *testing.T) {
		dir := t.TempDir()
		manifestPath, _, _, _ := writePlayerSnapshotManifestFixture(t, dir, "Bob", "Alice")
		report := newFullDataDryRunReport(fullDataDryRunOptions{PlayerManifestPath: manifestPath})
		aggregate := fullDataAggregate{
			itemOwners:     map[string][]string{},
			playerIDs:      map[string]string{},
			playerNames:    map[string]string{},
			playerRefs:     map[string][]string{},
			playerNameRefs: map[string][]fullDataNamedPlayerReference{},
			worldIDs:       map[string]string{},
		}

		inspectFullDataPlayers(fullDataDryRunOptions{PlayerManifestPath: manifestPath}, &report, &aggregate)
		if report.Sources[2].Status != "invalid" {
			t.Fatalf("snapshot name drift status=%q want invalid", report.Sources[2].Status)
		}
		if !hasFullDataCheck(report.Validation.Checks, "player_manifest", "fail") {
			t.Fatalf("snapshot name drift check=%+v", report.Validation.Checks)
		}
		if len(aggregate.playerIDs) != 0 {
			t.Fatalf("snapshot name drift populated aggregate identities=%v", aggregate.playerIDs)
		}
	})

	t.Run("room reference", func(t *testing.T) {
		dir := t.TempDir()
		manifestPath, _, _, _ := writePlayerSnapshotManifestFixture(t, dir, "Alice", "Alice")
		report := newFullDataDryRunReport(fullDataDryRunOptions{PlayerManifestPath: manifestPath})
		aggregate := fullDataAggregate{
			state:          &world.State{Rooms: map[int16]world.RoomState{}},
			itemOwners:     map[string][]string{},
			playerIDs:      map[string]string{},
			playerNames:    map[string]string{},
			playerRefs:     map[string][]string{},
			playerNameRefs: map[string][]fullDataNamedPlayerReference{},
			worldIDs:       map[string]string{},
		}

		inspectFullDataPlayers(fullDataDryRunOptions{PlayerManifestPath: manifestPath}, &report, &aggregate)
		if report.Sources[2].Status != "invalid" {
			t.Fatalf("room reference drift status=%q want invalid", report.Sources[2].Status)
		}
		if !hasFullDataCheck(report.Validation.Checks, "player_manifest", "fail") {
			t.Fatalf("room reference drift check=%+v", report.Validation.Checks)
		}
		if len(aggregate.playerIDs) != 0 {
			t.Fatalf("room reference drift populated aggregate identities=%v", aggregate.playerIDs)
		}
	})
}

func TestFullDataDryRunCLIIsDatabaseAndListenerFreeAndMutuallyExclusive(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "muhan")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = fullDataTestPackageDir(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	runMissing := func() ([]byte, []byte, error) {
		command := exec.Command(binary, "-full-data-dry-run")
		command.Dir = t.TempDir()
		command.Env = environmentWithoutDatabaseOrListener()
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
	firstOutput, firstErrOutput, err := runMissing()
	if err == nil {
		t.Fatal("empty full-data dry-run unexpectedly succeeded")
	}
	secondOutput, secondErrOutput, secondErr := runMissing()
	if secondErr == nil {
		t.Fatal("second empty full-data dry-run unexpectedly succeeded")
	}
	if !bytes.Equal(firstOutput, secondOutput) {
		t.Fatalf("CLI reports are not deterministic:\nfirst=%s\nsecond=%s", firstOutput, secondOutput)
	}
	for _, stderr := range [][]byte{firstErrOutput, secondErrOutput} {
		text := string(stderr)
		if strings.Contains(text, "DATABASE_URL") || strings.Contains(strings.ToLower(text), "listening") {
			t.Fatalf("full-data dry-run crossed DB/listener boundary: %s", text)
		}
	}
	var report FullDataDryRunReport
	if err := json.Unmarshal(firstOutput, &report); err != nil {
		t.Fatalf("decode full-data report: %v\n%s", err, firstOutput)
	}
	if report.Mode != fullDataDryRunMode || report.Result != "missing-input" || report.ExitCode == 0 || report.DatabaseOpened || report.ListenerStarted || report.OutputsWritten {
		t.Fatalf("CLI report=%+v", report)
	}

	conflict := exec.Command(binary, "-full-data-dry-run", "-migrate")
	conflict.Env = environmentWithoutDatabaseOrListener()
	conflictOutput, conflictErr := conflict.CombinedOutput()
	if conflictErr == nil {
		t.Fatalf("full-data dry-run + migrate unexpectedly succeeded: %s", conflictOutput)
	}
	if !strings.Contains(string(conflictOutput), "cannot be combined") {
		t.Fatalf("mutual exclusion output=%s", conflictOutput)
	}
	if strings.Contains(string(conflictOutput), "DATABASE_URL") || strings.Contains(strings.ToLower(string(conflictOutput)), "listening") {
		t.Fatalf("mutual exclusion crossed DB/listener boundary: %s", conflictOutput)
	}

	markerConflict := exec.Command(binary, "-full-data-dry-run", "-inspect-bank-snapshot-dry-run")
	markerConflict.Dir = t.TempDir()
	markerConflict.Env = environmentWithoutDatabaseOrListener()
	markerOutput, markerErr := markerConflict.CombinedOutput()
	if markerErr == nil {
		t.Fatalf("full-data dry-run + bank inspection marker unexpectedly succeeded: %s", markerOutput)
	}
	if !strings.Contains(string(markerOutput), "cannot be combined") {
		t.Fatalf("bank inspection marker bypassed mutual exclusion: %s", markerOutput)
	}
	if strings.Contains(string(markerOutput), "{\n") {
		t.Fatalf("bank inspection marker entered full-data JSON mode: %s", markerOutput)
	}
}

func fullDataTestPackageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}

func fullDataTestRepoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join(fullDataTestPackageDir(t), "..", "..", ".."))
}

func containsFullDataString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasFullDataCheck(checks []fullDataCheckReport, name, status string) bool {
	for _, check := range checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
