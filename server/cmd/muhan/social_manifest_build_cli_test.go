package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestValidateSocialManifestBuildFlags(t *testing.T) {
	if options, err := validateSocialManifestBuildFlags("", "", "", "", false); err != nil || options != (socialManifestBuildOptions{}) {
		t.Fatalf("default options=%+v err=%v", options, err)
	}
	cases := []struct {
		name       string
		familyRoot string
		memoRoot   string
		mapping    string
		output     string
		dryRun     bool
		want       error
	}{
		{name: "root required", mapping: "mapping.json", want: errSocialManifestBuildRootRequired},
		{name: "roots exclusive", familyRoot: "/family", memoRoot: "/memo", mapping: "mapping.json", want: errSocialManifestBuildRootsExclusive},
		{name: "mapping required", familyRoot: "/family", want: errSocialManifestBuildMappingRequired},
		{name: "output required", familyRoot: "/family", mapping: "mapping.json", want: errSocialManifestBuildOutputRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateSocialManifestBuildFlags(tc.familyRoot, tc.memoRoot, tc.mapping, tc.output, tc.dryRun)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
	if options, err := validateSocialManifestBuildFlags("/family", "", "mapping.json", "", true); err != nil || !options.DryRun {
		t.Fatalf("dry-run options=%+v err=%v", options, err)
	}
}

func TestBuildSocialFamilyManifestUsesExplicitIdentityMap(t *testing.T) {
	root := makeSocialBuildFamilyRoot(t)
	dir := t.TempDir()
	mappingPath := writeSocialBuildMapping(t, dir, socialManifestBuildInput{
		Version: 1, Kind: socialImportKindFamily, WorldID: "world-family", CommandID: "family-command",
		ExpectedRevision: ptrInt64(3),
		FamilyIdentity: &world.LegacyFamilyIdentityMapV1{Families: map[int16]world.LegacyFamilyIdentityFamilyV1{
			1: {BossID: "character-boss", Members: map[string]string{"Boss": "character-boss", "Alice": "character-alice"}},
		}},
	})
	result, err := buildSocialImportManifest(socialManifestBuildOptions{FamilyRoot: root, MappingPath: mappingPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.Kind != socialImportKindFamily || result.Batch.FamilyCount != 1 || result.Batch.MemberCount != 2 || result.Batch.Family == nil {
		t.Fatalf("result=%+v", result)
	}
	if result.Batch.Family.Families[0].BossID != "character-boss" || result.Batch.Family.Families[0].Members[0].ID != "character-boss" {
		t.Fatalf("family=%+v", result.Batch.Family.Families)
	}
	if result.Batch.AggregateSHA256 == [32]byte{} || len(result.Raw) == 0 {
		t.Fatal("manifest evidence is empty")
	}
	var manifest socialImportManifest
	if err := decodeSingleJSON(result.Raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Memos != nil || manifest.RecipientNames != nil || manifest.FamilyState == nil || manifest.Catalog == nil {
		t.Fatalf("unexpected family manifest fields=%+v", manifest)
	}
	outputPath := filepath.Join(dir, "family-manifest.json")
	if err := writeSocialManifestBuild(outputPath, result.Raw, mappingPath, root); err != nil {
		t.Fatal(err)
	}
	loaded, err := readSocialImportManifest(outputPath)
	if err != nil || loaded.FamilyCount != 1 || loaded.MemberCount != 2 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestBuildSocialMemoManifestParsesEveryMappedRecipient(t *testing.T) {
	root := makeSocialBuildMemoRoot(t)
	dir := t.TempDir()
	mappingPath := writeSocialBuildMapping(t, dir, socialManifestBuildInput{
		Version: 1, Kind: socialImportKindMemos, WorldID: "world-memo", CommandID: "memo-command",
		ExpectedRevision: ptrInt64(7), Recipients: []socialMemoRecipientMapping{
			{Name: "Bob", ID: "character-bob", SenderIDs: map[string]string{"Alice": "character-alice"}},
		},
	})
	result, err := buildSocialImportManifest(socialManifestBuildOptions{MemoRoot: root, MappingPath: mappingPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.Kind != socialImportKindMemos || result.Batch.RecipientCount != 1 || result.Batch.MemoCount != 1 || result.Batch.Memos == nil {
		t.Fatalf("result=%+v", result)
	}
	memo := result.Batch.Memos.Memos["character-bob"][0]
	if memo.SenderID != "character-alice" || memo.SenderName != "Alice" || memo.Body != "hello" || memo.CreatedAt.Location() != time.UTC {
		t.Fatalf("memo=%+v", memo)
	}
	if result.Batch.Memos.SourcePath != "" || result.Batch.Memos.RawPath != "" || len(result.Batch.Memos.CredentialHash) != 0 {
		t.Fatal("source or credential evidence leaked into storage request")
	}
	var manifest socialImportManifest
	if err := decodeSingleJSON(result.Raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Memos == nil || manifest.RecipientNames["character-bob"] != "Bob" || manifest.FamilyState != nil {
		t.Fatalf("unexpected memo manifest=%+v", manifest)
	}
}

func TestBuildSocialManifestRejectsUnknownMappingAndUnsafeOutput(t *testing.T) {
	root := makeSocialBuildFamilyRoot(t)
	dir := t.TempDir()
	valid := socialManifestBuildInput{
		Version: 1, Kind: socialImportKindFamily, WorldID: "world-family", CommandID: "family-command",
		ExpectedRevision: ptrInt64(0), FamilyIdentity: &world.LegacyFamilyIdentityMapV1{Families: map[int16]world.LegacyFamilyIdentityFamilyV1{
			1: {BossID: "boss", Members: map[string]string{"Boss": "boss", "Alice": "alice"}},
		}},
	}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "unknown.json")
	if err := os.WriteFile(path, append(raw[:len(raw)-1], []byte(`,"password":"bad"}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildSocialImportManifest(socialManifestBuildOptions{FamilyRoot: root, MappingPath: path}); err == nil {
		t.Fatal("unknown sensitive mapping field was accepted")
	}
	validPath := writeSocialBuildMapping(t, dir, valid)
	result, err := buildSocialImportManifest(socialManifestBuildOptions{FamilyRoot: root, MappingPath: validPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSocialManifestBuild(filepath.Join(root, "family-manifest.json"), result.Raw, validPath, root); !errors.Is(err, errSocialManifestBuildSourceOutputOverlap) {
		t.Fatalf("source-overlap err=%v", err)
	}
	out := filepath.Join(dir, "family-manifest.json")
	if err := writeSocialManifestBuild(out, result.Raw, validPath, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(writeSocialManifestBuild(out, result.Raw, validPath, root), errSocialManifestBuildOutputConflict) {
		t.Fatal("changed output was accepted")
	}
}

func TestSocialManifestBuildCLIIsDBFreeAndWritesImmutableOutput(t *testing.T) {
	root := makeSocialBuildFamilyRoot(t)
	dir := t.TempDir()
	mappingPath := writeSocialBuildMapping(t, dir, socialManifestBuildInput{
		Version: 1, Kind: socialImportKindFamily, WorldID: "world-cli", CommandID: "family-cli",
		ExpectedRevision: ptrInt64(0), FamilyIdentity: &world.LegacyFamilyIdentityMapV1{Families: map[int16]world.LegacyFamilyIdentityFamilyV1{
			1: {BossID: "boss", Members: map[string]string{"Boss": "boss", "Alice": "alice"}},
		}},
	})
	binaryPath := filepath.Join(dir, "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	baseEnv := make([]string, 0, len(os.Environ()))
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") || strings.HasPrefix(value, "LISTEN_ADDR=") {
			continue
		}
		baseEnv = append(baseEnv, value)
	}
	dryRun := exec.Command(binaryPath, "-build-social-family-root", root, "-build-social-manifest-mapping", mappingPath, "-build-social-manifest-dry-run")
	dryRun.Env = baseEnv
	if output, err := dryRun.CombinedOutput(); err != nil || !strings.Contains(string(output), "social manifest build validated") || strings.Contains(string(output), "DATABASE_URL is required") {
		t.Fatalf("dry-run output=%s err=%v", output, err)
	}
	outputPath := filepath.Join(dir, "social-manifest.json")
	writeArgs := []string{"-build-social-family-root", root, "-build-social-manifest-mapping", mappingPath, "-build-social-manifest-output", outputPath}
	write := exec.Command(binaryPath, writeArgs...)
	write.Env = baseEnv
	if output, err := write.CombinedOutput(); err != nil || !strings.Contains(string(output), "social import manifest built") {
		t.Fatalf("write output=%s err=%v", output, err)
	}
	if info, err := os.Stat(outputPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("output stat=%v info=%v", err, info)
	}
	replay := exec.Command(binaryPath, writeArgs...)
	replay.Env = baseEnv
	if output, err := replay.CombinedOutput(); err != nil || !strings.Contains(string(output), "social import manifest built") {
		t.Fatalf("same-byte replay output=%s err=%v", output, err)
	}
}

func makeSocialBuildFamilyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	familyDir := filepath.Join(root, "family")
	if err := os.Mkdir(familyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(familyDir, "family_list"), []byte("1 Red Boss 3\n16 end end 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(familyDir, "family_member_1"), []byte("5 Boss\n4 Alice\n0 Red\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func makeSocialBuildMemoRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	player := filepath.Join(root, "player")
	fal := filepath.Join(player, "fal")
	for _, path := range []string{player, fal} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	raw := []byte("Thu Jan  1 00:00:00 2026 에 [Alice] 님이 남기신 메모 : \n>>>>> hello\n")
	if err := os.WriteFile(filepath.Join(fal, "Bob"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeSocialBuildMapping(t *testing.T, dir string, input socialManifestBuildInput) string {
	t.Helper()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mapping.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ptrInt64(value int64) *int64 { return &value }
