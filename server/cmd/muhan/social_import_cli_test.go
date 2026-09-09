package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestValidateSocialImportFlagsDefaultsToDBFreeValidation(t *testing.T) {
	if options, err := validateSocialImportFlags("", false, false); err != nil || options != (socialImportOptions{}) {
		t.Fatalf("normal mode options=%+v err=%v", options, err)
	}
	if _, err := validateSocialImportFlags("", true, false); !errors.Is(err, errSocialImportDryRunRequired) {
		t.Fatalf("dry-run without manifest err=%v", err)
	}
	if _, err := validateSocialImportFlags("", false, true); !errors.Is(err, errSocialImportApplyRequired) {
		t.Fatalf("apply without manifest err=%v", err)
	}
	if _, err := validateSocialImportFlags("social.json", true, true); !errors.Is(err, errSocialImportApplyDryRunExclusive) {
		t.Fatalf("apply+dry-run err=%v", err)
	}
	options, err := validateSocialImportFlags("social.json", false, false)
	if err != nil || options.ManifestPath != "social.json" || !options.DryRun || options.Apply {
		t.Fatalf("path-only options=%+v err=%v", options, err)
	}
	options, err = validateSocialImportFlags("social.json", false, true)
	if err != nil || options.ManifestPath != "social.json" || options.DryRun || !options.Apply {
		t.Fatalf("apply options=%+v err=%v", options, err)
	}
}

func TestReadSocialFamilyManifestRequiresExplicitIDsAndNormalizesAggregate(t *testing.T) {
	dir := t.TempDir()
	revision := int64(4)
	manifest := socialImportManifest{
		Version:          socialImportManifestVersion,
		Kind:             socialImportKindFamily,
		WorldID:          "world-1",
		CommandID:        "family-import-1",
		ExpectedRevision: &revision,
		FamilyState: &world.FamilyState{Members: map[int16][]world.FamilyMember{
			1: {{ID: "character-1", Name: "Alice", Class: 4}},
		}},
		Catalog: &world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
			1: {ID: 1, Name: "청룡", Boss: "Alice", Fee: 3},
		}},
		BossIDs: map[int16]string{1: "character-1"},
	}
	path := writeSocialManifestFixture(t, dir, manifest)
	batch, err := readSocialImportManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Kind != socialImportKindFamily || batch.WorldID != "world-1" || batch.ExpectedRevision != revision || batch.Family == nil {
		t.Fatalf("batch=%+v", batch)
	}
	if batch.FamilyCount != 1 || batch.MemberCount != 1 || batch.Family.Families[0].BossID != "character-1" {
		t.Fatalf("family batch=%+v", batch)
	}
	if batch.AggregateSHA256 == ([32]byte{}) {
		t.Fatal("aggregate digest is empty")
	}
	if batch.Family.SourcePath != "" || batch.Family.RawPath != "" || len(batch.Family.CredentialHash) != 0 {
		t.Fatal("sensitive source fields reached storage request")
	}

	manifest.BossIDs = nil
	path = writeSocialManifestFixture(t, dir, manifest)
	if _, err := readSocialImportManifest(path); err == nil {
		t.Fatal("family manifest without explicit boss IDs was accepted")
	}
}

func TestReadSocialMemoManifestValidatesPointerFreeIDsAndTimestamps(t *testing.T) {
	dir := t.TempDir()
	revision := int64(0)
	manifest := socialImportManifest{
		Version:          socialImportManifestVersion,
		Kind:             socialImportKindMemos,
		WorldID:          "world-memos",
		CommandID:        "memo-import-1",
		ExpectedRevision: &revision,
		Memos: map[string][]world.CharacterMemo{
			"recipient-1": {{
				ID:         "memo-1",
				SenderID:   "sender-1",
				SenderName: "Alice",
				Body:       "hello",
				CreatedAt:  time.Unix(10, 123).In(time.FixedZone("KST", 9*60*60)),
			}},
		},
		RecipientNames: map[string]string{"recipient-1": "Bob"},
	}
	path := writeSocialManifestFixture(t, dir, manifest)
	batch, err := readSocialImportManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Kind != socialImportKindMemos || batch.Memos == nil || batch.RecipientCount != 1 || batch.MemoCount != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	got := batch.Memos.Memos["recipient-1"][0].CreatedAt
	if got.Location() != time.UTC || got.Nanosecond() != 123 {
		t.Fatalf("timestamp=%v, want UTC with nanoseconds preserved", got)
	}
	if batch.Memos.SourcePath != "" || batch.Memos.RawPath != "" || len(batch.Memos.CredentialHash) != 0 {
		t.Fatal("sensitive source fields reached storage request")
	}

	manifest.Memos["recipient-1"][0].SenderName = "not canonical"
	path = writeSocialManifestFixture(t, dir, manifest)
	if _, err := readSocialImportManifest(path); err == nil {
		t.Fatal("non-canonical sender name was accepted")
	}
}

func TestReadSocialManifestRejectsSensitiveFieldsPathsAndTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	revision := int64(0)
	manifest := socialImportManifest{
		Version:          socialImportManifestVersion,
		Kind:             socialImportKindMemos,
		WorldID:          "world-memos",
		CommandID:        "memo-import-1",
		ExpectedRevision: &revision,
		Memos:            map[string][]world.CharacterMemo{},
	}
	validRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"password", "raw_path", "source_path", "credential_hash", "account_name"} {
		t.Run(field, func(t *testing.T) {
			var object map[string]any
			if err := json.Unmarshal(validRaw, &object); err != nil {
				t.Fatal(err)
			}
			object[field] = "must-not-be-accepted"
			path := filepath.Join(dir, field+".json")
			raw, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readSocialImportManifest(path); err == nil {
				t.Fatalf("sensitive field %q was accepted", field)
			}
		})
	}

	path := filepath.Join(dir, "trailing.json")
	if err := os.WriteFile(path, append(validRaw, []byte(` {"unexpected":true}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSocialImportManifest(path); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing JSON err=%v", err)
	}

	publicPath := filepath.Join(dir, "public.json")
	if err := os.WriteFile(publicPath, validRaw, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readSocialImportManifest(publicPath); !errors.Is(err, errSocialImportManifestNotPrivate) {
		t.Fatalf("public manifest err=%v", err)
	}
}

func TestRunSocialImportFailsClosedWithoutExplicitStorage(t *testing.T) {
	if err := runSocialImport(nil, nil, socialImportBatch{Kind: socialImportKindMemos}); err == nil {
		t.Fatal("nil store/context accepted")
	}
	if err := runSocialImport(nil, &storage.Postgres{}, socialImportBatch{Kind: socialImportKindMemos}); err == nil {
		t.Fatal("nil context/store accepted")
	}
}

func TestSocialManifestDryRunDoesNotOpenDatabaseOrListener(t *testing.T) {
	dir := t.TempDir()
	revision := int64(0)
	manifestPath := writeSocialManifestFixture(t, dir, socialImportManifest{
		Version:          socialImportManifestVersion,
		Kind:             socialImportKindMemos,
		WorldID:          "dry-run-world",
		CommandID:        "dry-run-command",
		ExpectedRevision: &revision,
		Memos:            map[string][]world.CharacterMemo{},
	})
	binaryPath := filepath.Join(dir, "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, args := range [][]string{
		{"-import-social-manifest", manifestPath},
		{"-import-social-manifest", manifestPath, "-import-social-manifest-dry-run"},
	} {
		command := exec.Command(binaryPath, args...)
		for _, value := range os.Environ() {
			if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") || strings.HasPrefix(value, "LISTEN_ADDR=") {
				continue
			}
			command.Env = append(command.Env, value)
		}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("social dry-run args=%v failed: %v\n%s", args, err, output)
		}
		text := string(output)
		if !strings.Contains(text, "social import manifest validated") || strings.Contains(text, "DATABASE_URL is required") || strings.Contains(text, "listening") {
			t.Fatalf("unexpected dry-run args=%v output=%q", args, text)
		}
	}
}

func TestSocialManifestApplyAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("MUHAN_SOCIAL_IMPORT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL URL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := storage.NewPostgres(db)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("social-cli-%d", time.Now().UnixNano())
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			// Offline recipients retain their canonical player record but do not
			// occupy a room in the world snapshot.
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{
			"actor":  {Body: world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
			"target": {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 5, RoomID: 1}, Online: false},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	revision := int64(0)
	manifestPath := writeSocialManifestFixture(t, t.TempDir(), socialImportManifest{
		Version:          socialImportManifestVersion,
		Kind:             socialImportKindMemos,
		WorldID:          worldID,
		CommandID:        "social-cli-memo-1",
		ExpectedRevision: &revision,
		Memos:            map[string][]world.CharacterMemo{"target": {{ID: "social-cli-memo", SenderID: "actor", SenderName: "Alice", Body: "hello", CreatedAt: time.Unix(10, 0)}}},
		RecipientNames:   map[string]string{"target": "Bob"},
	})
	binaryPath := filepath.Join(t.TempDir(), "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binaryPath, "-import-social-manifest", manifestPath, "-import-social-manifest-apply")
	command.Env = make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "LISTEN_ADDR=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	command.Env = append(command.Env, "DATABASE_URL="+dsn)
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "social import completed") {
		t.Fatalf("social apply output=%s err=%v", output, err)
	}
	loaded, err := repo.LoadWorld(ctx, worldID)
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("loaded world=%+v err=%v", loaded, err)
	}
	decoded, err := world.DecodeState(loaded.State)
	if err != nil || decoded.Memos["target"][0].Body != "hello" {
		t.Fatalf("applied memos=%+v err=%v", decoded.Memos, err)
	}
}

func writeSocialManifestFixture(t *testing.T, dir string, manifest socialImportManifest) string {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "social-manifest.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
