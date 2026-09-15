package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVoteStateManifestFlags(t *testing.T) {
	if options, err := validateVoteStateManifestImportFlags("", false, false); err != nil || options != (voteStateManifestImportOptions{}) {
		t.Fatalf("default import options=%+v err=%v", options, err)
	}
	if _, err := validateVoteStateManifestImportFlags("", true, false); !errors.Is(err, errVoteManifestDryRunRequired) {
		t.Fatalf("missing manifest dry-run err=%v", err)
	}
	if _, err := validateVoteStateManifestImportFlags("", false, true); !errors.Is(err, errVoteManifestApplyRequired) {
		t.Fatalf("missing manifest apply err=%v", err)
	}
	if _, err := validateVoteStateManifestImportFlags("manifest.json", true, true); !errors.Is(err, errVoteManifestApplyDryRun) {
		t.Fatalf("apply+dry-run err=%v", err)
	}
	if options, err := validateVoteStateManifestImportFlags("manifest.json", false, false); err != nil || !options.DryRun || options.Apply {
		t.Fatalf("path-only options=%+v err=%v", options, err)
	}
	if options, err := validateVoteStateManifestImportFlags("manifest.json", false, true); err != nil || options.DryRun || !options.Apply {
		t.Fatalf("apply options=%+v err=%v", options, err)
	}

	if options, err := validateVoteStateManifestBuildFlags("", "", "", "", false); err != nil || options != (voteStateManifestBuildOptions{}) {
		t.Fatalf("default build options=%+v err=%v", options, err)
	}
	if _, err := validateVoteStateManifestBuildFlags("root", "", "mapping.json", "out.json", false); !errors.Is(err, errVoteManifestBuildIssueRequired) {
		t.Fatalf("missing ISSUE err=%v", err)
	}
	if _, err := validateVoteStateManifestBuildFlags("root", "ISSUE", "", "out.json", false); !errors.Is(err, errVoteManifestBuildMappingNeeded) {
		t.Fatalf("missing mapping err=%v", err)
	}
	if _, err := validateVoteStateManifestBuildFlags("root", "ISSUE", "mapping.json", "", false); !errors.Is(err, errVoteManifestBuildOutputNeeded) {
		t.Fatalf("missing output err=%v", err)
	}
	if options, err := validateVoteStateManifestBuildFlags("root", "ISSUE", "mapping.json", "", true); err != nil || !options.DryRun {
		t.Fatalf("dry-run build options=%+v err=%v", options, err)
	}
	if _, err := validateVoteStateManifestBuildFlags("", "ISSUE", "", "", false); !errors.Is(err, errVoteManifestBuildRootRequired) {
		t.Fatalf("root-required err=%v", err)
	}
}

func TestBuildVoteStateManifestBindsRawFilesToIssueAndIdentity(t *testing.T) {
	fixture := makeVoteManifestFixture(t)
	result, err := buildVoteStateManifest(voteStateManifestBuildOptions{
		Root: fixture.root, IssueFile: fixture.issuePath, MappingPath: fixture.mappingPath,
		OutputPath: fixture.outputPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.WorldID != "world-vote" || result.Batch.CommandID != "vote-command" || result.Batch.ExpectedRevision != 4 || result.Ballots != 1 {
		t.Fatalf("result=%+v", result)
	}
	ballot, ok := result.Batch.Votes.Ballots["player-alice"]
	if !ok || ballot.CatalogDigest != result.Batch.CatalogDigest || !bytes.Equal(ballot.Choices, []byte("A")) {
		t.Fatalf("ballot=%+v digest=%s", ballot, result.Batch.CatalogDigest)
	}
	if bytes.Contains(result.Raw, []byte(fixture.root)) || bytes.Contains(result.Raw, []byte("player/vote/Alice_v")) {
		t.Fatalf("manifest leaked source path: %s", result.Raw)
	}
	var manifest voteStateManifest
	if err := decodeSingleJSON(result.Raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != voteStateManifestVersion || manifest.Kind != voteStateManifestKind || manifest.CatalogDigest != result.Batch.CatalogDigest || len(manifest.Ballots) != 1 || manifest.Ballots[0].LegacyName != "Alice" || manifest.Ballots[0].PlayerID != "player-alice" || manifest.Ballots[0].Choices != "A" || !validVoteManifestDigest(manifest.Ballots[0].SourceSHA256) {
		t.Fatalf("manifest=%+v", manifest)
	}
	if err := writeVoteStateManifest(fixture.outputPath, result.Raw, fixture.mappingPath, fixture.root); err != nil {
		t.Fatal(err)
	}
	if err := writeVoteStateManifest(fixture.outputPath, result.Raw, fixture.mappingPath, fixture.root); err != nil {
		t.Fatalf("same-byte replay: %v", err)
	}
	loaded, err := readVoteStateManifest(fixture.outputPath)
	if err != nil || loaded.WorldID != result.Batch.WorldID || loaded.CommandID != result.Batch.CommandID || len(loaded.Votes.Ballots) != 1 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if info, err := os.Stat(fixture.outputPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode=%v err=%v", info, err)
	}
	if err := os.WriteFile(fixture.outputPath, []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(writeVoteStateManifest(fixture.outputPath, result.Raw, fixture.mappingPath, fixture.root), errVoteManifestBuildConflict) {
		t.Fatal("changed output was accepted")
	}
}

func TestVoteStateManifestRejectsMappingAndManifestDrift(t *testing.T) {
	fixture := makeVoteManifestFixture(t)
	mappingRaw, err := os.ReadFile(fixture.mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	unknownMapping := filepath.Join(fixture.mappingDir, "unknown.json")
	if err := os.WriteFile(unknownMapping, append(mappingRaw[:len(mappingRaw)-1], []byte(`,"password":"bad"}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildVoteStateManifest(voteStateManifestBuildOptions{Root: fixture.root, IssueFile: fixture.issuePath, MappingPath: unknownMapping, OutputPath: fixture.outputPath}); err == nil {
		t.Fatal("unknown mapping field was accepted")
	}

	result, err := buildVoteStateManifest(voteStateManifestBuildOptions{Root: fixture.root, IssueFile: fixture.issuePath, MappingPath: fixture.mappingPath, OutputPath: fixture.outputPath})
	if err != nil {
		t.Fatal(err)
	}
	var manifest voteStateManifest
	if err := decodeSingleJSON(result.Raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Ballots[0].Choices = "H"
	badChoicesPath := filepath.Join(fixture.mappingDir, "bad-choices.json")
	badChoices, _ := json.Marshal(manifest)
	if err := os.WriteFile(badChoicesPath, append(badChoices, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readVoteStateManifest(badChoicesPath); err == nil {
		t.Fatal("invalid choice was accepted")
	}
	manifest.Ballots[0].Choices = "A"
	manifest.Ballots[0].SourceSHA256 = strings.Repeat("0", 64)
	badSourcePath := filepath.Join(fixture.mappingDir, "bad-source.json")
	badSource, _ := json.Marshal(manifest)
	if err := os.WriteFile(badSourcePath, append(badSource, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readVoteStateManifest(badSourcePath); err != nil {
		t.Fatal("well-formed source digest should be accepted as review evidence: ", err)
	}
	manifest.Ballots[0].SourceSHA256 = "not-a-digest"
	badDigestPath := filepath.Join(fixture.mappingDir, "bad-digest.json")
	badDigest, _ := json.Marshal(manifest)
	if err := os.WriteFile(badDigestPath, append(badDigest, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readVoteStateManifest(badDigestPath); err == nil {
		t.Fatal("malformed source digest was accepted")
	}
}

func TestVoteStateManifestCLIIsDBFreeForBuildAndValidation(t *testing.T) {
	fixture := makeVoteManifestFixture(t)
	binaryPath := filepath.Join(fixture.mappingDir, "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	env := environmentWithoutDatabaseOrListener()
	dryRun := exec.Command(binaryPath,
		"-build-vote-manifest-root", fixture.root,
		"-build-vote-manifest-issue-file", fixture.issuePath,
		"-build-vote-manifest-mapping", fixture.mappingPath,
		"-build-vote-manifest-dry-run",
	)
	dryRun.Env = env
	output, err := dryRun.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "vote state manifest build validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("dry-run output=%s err=%v", output, err)
	}
	write := exec.Command(binaryPath,
		"-build-vote-manifest-root", fixture.root,
		"-build-vote-manifest-issue-file", fixture.issuePath,
		"-build-vote-manifest-mapping", fixture.mappingPath,
		"-build-vote-manifest-output", fixture.outputPath,
	)
	write.Env = env
	output, err = write.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "vote state manifest built") {
		t.Fatalf("write output=%s err=%v", output, err)
	}
	pathOnly := exec.Command(binaryPath, "-import-vote-manifest", fixture.outputPath)
	pathOnly.Env = env
	output, err = pathOnly.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "vote state manifest validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("path-only output=%s err=%v", output, err)
	}
}

type voteManifestFixture struct {
	root, issuePath, mappingDir, mappingPath, outputPath string
}

func makeVoteManifestFixture(t *testing.T) voteManifestFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	playerDir := filepath.Join(root, "player")
	voteDir := filepath.Join(playerDir, "vote")
	for _, directory := range []string{playerDir, voteDir} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(voteDir, "Alice_v"), []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	issueDir := t.TempDir()
	if err := os.Chmod(issueDir, 0o700); err != nil {
		t.Fatal(err)
	}
	issuePath := filepath.Join(issueDir, "ISSUE")
	if err := os.WriteFile(issuePath, []byte("1\n질문\n후보\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mappingDir := t.TempDir()
	if err := os.Chmod(mappingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	expectedRevision := int64(4)
	mapping := voteStateManifestBuildInput{
		Version: voteStateManifestVersion, Kind: voteStateManifestKind, WorldID: "world-vote", CommandID: "vote-command",
		ExpectedRevision: &expectedRevision, Players: []voteStateManifestMapping{{Name: "Alice", ID: "player-alice"}},
	}
	mappingPath := filepath.Join(mappingDir, "mapping.json")
	raw, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mappingPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return voteManifestFixture{root: root, issuePath: issuePath, mappingDir: mappingDir, mappingPath: mappingPath, outputPath: filepath.Join(mappingDir, "vote-manifest.json")}
}
