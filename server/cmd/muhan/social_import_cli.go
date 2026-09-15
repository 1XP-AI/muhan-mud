package main

// This file is the operator boundary for the reviewed social aggregates. It
// deliberately does not discover legacy files, derive IDs from names, or open
// a database. main.go is responsible for wiring the returned options into the
// process lifecycle; only the explicit Apply option may call runSocialImport.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	maxSocialImportManifestBytes int64 = 8 << 20
	socialImportManifestVersion        = 1
	socialImportKindFamily             = "family-ledger-v1"
	socialImportKindMemos              = "character-memos-v1"
)

var (
	errSocialImportManifestPathRequired = errors.New("social import manifest path is required")
	errSocialImportManifestNotPrivate   = errors.New("social import manifest must be a private 0600 regular file")
	errSocialImportManifestNotRegular   = errors.New("social import manifest must be a regular file")
	errSocialImportManifestTooLarge     = errors.New("social import manifest exceeds the size limit")
	errSocialImportManifestInvalid      = errors.New("invalid social import manifest")
	errSocialImportApplyDryRunExclusive = errors.New("social import apply and dry-run modes are mutually exclusive")
	errSocialImportApplyRequired        = errors.New("social import apply requires -import-social-manifest")
	errSocialImportDryRunRequired       = errors.New("social import dry-run requires -import-social-manifest")
)

// socialImportOptions is intentionally more restrictive than the older
// manifest import modes. A path alone is validation-only. Apply must be an
// explicit operator acknowledgement and is the only value that may reach the
// PostgreSQL import methods.
type socialImportOptions struct {
	ManifestPath string
	DryRun       bool
	Apply        bool
}

func validateSocialImportFlags(path string, dryRun, apply bool) (socialImportOptions, error) {
	if path == "" {
		if dryRun {
			return socialImportOptions{}, errSocialImportDryRunRequired
		}
		if apply {
			return socialImportOptions{}, errSocialImportApplyRequired
		}
		return socialImportOptions{}, nil
	}
	if dryRun && apply {
		return socialImportOptions{}, errSocialImportApplyDryRunExclusive
	}
	// Supplying only the manifest path is the safe default: validate all
	// bytes and return before DATABASE_URL, a listener, or a writer is opened.
	return socialImportOptions{ManifestPath: path, DryRun: !apply || dryRun, Apply: apply}, nil
}

// socialImportManifest is a single reviewed aggregate. The family spelling
// carries FamilyState plus FamilyCatalog and an explicit boss ID for every
// family. The memo spelling is keyed by immutable recipient IDs; optional
// recipient_names are integrity evidence and never an identity selector.
//
// There are intentionally no source/raw path, account, password, or
// credential fields. DisallowUnknownFields below makes adding one fail
// closed instead of silently accepting sensitive operator input.
type socialImportManifest struct {
	Version          int                  `json:"version"`
	Kind             string               `json:"kind"`
	WorldID          string               `json:"world_id"`
	CommandID        string               `json:"command_id"`
	ExpectedRevision *int64               `json:"expected_revision"`
	FamilyState      *world.FamilyState   `json:"family_state,omitempty"`
	Catalog          *world.FamilyCatalog `json:"catalog,omitempty"`
	BossIDs          map[int16]string     `json:"boss_ids,omitempty"`
	// Memos intentionally has no omitempty: an explicit empty map is the
	// reviewed, fully migrated memo aggregate, while an omitted/null field is
	// unresolved and must fail closed.
	Memos          map[string][]world.CharacterMemo `json:"memos"`
	RecipientNames map[string]string                `json:"recipient_names,omitempty"`
}

// socialImportBatch is the fully normalized, DB-independent result of
// reading one manifest. It contains only the reviewed aggregate itself; no
// path or source bytes are carried beyond the parser.
type socialImportBatch struct {
	Kind             string
	WorldID          string
	CommandID        string
	ExpectedRevision int64
	AggregateSHA256  [sha256.Size]byte
	Family           *storage.FamilyLedgerImport
	Memos            *storage.CharacterMemosImport
	FamilyCount      int
	MemberCount      int
	RecipientCount   int
	MemoCount        int
}

func readSocialImportManifest(path string) (socialImportBatch, error) {
	if path == "" {
		return socialImportBatch{}, errSocialImportManifestPathRequired
	}
	raw, err := readPrivateBoundedFile(path, maxSocialImportManifestBytes, errSocialImportManifestNotPrivate, errSocialImportManifestNotRegular, errSocialImportManifestTooLarge)
	if err != nil {
		return socialImportBatch{}, err
	}
	var manifest socialImportManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return socialImportBatch{}, fmt.Errorf("%w: %v", errSocialImportManifestInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return socialImportBatch{}, fmt.Errorf("%w: trailing JSON values", errSocialImportManifestInvalid)
		}
		return socialImportBatch{}, fmt.Errorf("%w: trailing JSON: %v", errSocialImportManifestInvalid, err)
	}
	if manifest.Version != socialImportManifestVersion ||
		(manifest.Kind != socialImportKindFamily && manifest.Kind != socialImportKindMemos) ||
		!validOpaqueImportID(manifest.WorldID) || len(manifest.WorldID) > 128 ||
		!validOpaqueImportID(manifest.CommandID) || len(manifest.CommandID) > 128 ||
		manifest.ExpectedRevision == nil || *manifest.ExpectedRevision < 0 {
		return socialImportBatch{}, fmt.Errorf("%w: version, kind, identity, or expected_revision", errSocialImportManifestInvalid)
	}

	switch manifest.Kind {
	case socialImportKindFamily:
		return normalizeSocialFamilyManifest(manifest)
	case socialImportKindMemos:
		return normalizeSocialMemosManifest(manifest)
	default:
		// The closed kind check above makes this unreachable, but retaining a
		// default keeps this function fail-closed if the constants change.
		return socialImportBatch{}, fmt.Errorf("%w: unsupported kind", errSocialImportManifestInvalid)
	}
}

func normalizeSocialFamilyManifest(manifest socialImportManifest) (socialImportBatch, error) {
	if manifest.FamilyState == nil || manifest.Catalog == nil || manifest.BossIDs == nil || manifest.Memos != nil || manifest.RecipientNames != nil {
		return socialImportBatch{}, fmt.Errorf("%w: family aggregate fields", errSocialImportManifestInvalid)
	}
	family := *manifest.FamilyState
	catalog := *manifest.Catalog
	if family.Members == nil || catalog.Families == nil {
		return socialImportBatch{}, fmt.Errorf("%w: family and catalog must be explicit non-nil maps", errSocialImportManifestInvalid)
	}
	if err := family.Validate(); err != nil {
		return socialImportBatch{}, fmt.Errorf("%w: family state: %v", errSocialImportManifestInvalid, err)
	}
	if err := catalog.Validate(); err != nil {
		return socialImportBatch{}, fmt.Errorf("%w: family catalog: %v", errSocialImportManifestInvalid, err)
	}
	if len(family.Members) != len(catalog.Families) || len(family.Members) != len(manifest.BossIDs) {
		return socialImportBatch{}, fmt.Errorf("%w: family/catalog/boss ID sets differ", errSocialImportManifestInvalid)
	}

	familyIDs := make([]int, 0, len(family.Members))
	for familyID := range family.Members {
		familyIDs = append(familyIDs, int(familyID))
	}
	sort.Ints(familyIDs)
	rows := make([]storage.FamilyLedgerFamily, 0, len(familyIDs))
	seenBossIDs := make(map[string]struct{}, len(familyIDs))
	for _, rawID := range familyIDs {
		familyID := int16(rawID)
		definition, ok := catalog.Families[familyID]
		if !ok || definition.ID != familyID {
			return socialImportBatch{}, fmt.Errorf("%w: missing catalog family %d", errSocialImportManifestInvalid, familyID)
		}
		bossID, ok := manifest.BossIDs[familyID]
		if !ok || !validOpaqueImportID(bossID) || len(bossID) > 128 {
			return socialImportBatch{}, fmt.Errorf("%w: explicit boss identity required for family %d", errSocialImportManifestInvalid, familyID)
		}
		if _, exists := seenBossIDs[bossID]; exists {
			return socialImportBatch{}, fmt.Errorf("%w: duplicate boss identity", errSocialImportManifestInvalid)
		}
		seenBossIDs[bossID] = struct{}{}
		canonicalBoss, err := identity.CanonicalName(definition.Boss)
		if err != nil || canonicalBoss != definition.Boss {
			return socialImportBatch{}, fmt.Errorf("%w: non-canonical catalog boss name", errSocialImportManifestInvalid)
		}
		bossFound := false
		for _, member := range family.Members[familyID] {
			if member.ID == bossID {
				bossFound = true
				break
			}
		}
		if !bossFound {
			return socialImportBatch{}, fmt.Errorf("%w: boss identity is absent from family %d", errSocialImportManifestInvalid, familyID)
		}
		rows = append(rows, storage.FamilyLedgerFamily{
			ID:       familyID,
			Name:     definition.Name,
			BossID:   bossID,
			BossName: definition.Boss,
			Fee:      definition.Fee,
			Members:  append([]world.FamilyMember(nil), family.Members[familyID]...),
		})
	}

	input := storage.FamilyLedgerImport{
		WorldID:          manifest.WorldID,
		CommandID:        manifest.CommandID,
		ExpectedRevision: *manifest.ExpectedRevision,
		Family:           family,
		FamilyState:      family,
		Catalog:          catalog,
		Families:         rows,
	}
	aggregate, err := socialFamilyAggregateSHA256(family, catalog, manifest.BossIDs)
	if err != nil {
		return socialImportBatch{}, fmt.Errorf("%w: family aggregate encoding", errSocialImportManifestInvalid)
	}
	return socialImportBatch{
		Kind:             manifest.Kind,
		WorldID:          manifest.WorldID,
		CommandID:        manifest.CommandID,
		ExpectedRevision: *manifest.ExpectedRevision,
		AggregateSHA256:  aggregate,
		Family:           &input,
		FamilyCount:      len(family.Members),
		MemberCount:      socialFamilyMemberCount(family),
	}, nil
}

func normalizeSocialMemosManifest(manifest socialImportManifest) (socialImportBatch, error) {
	if manifest.FamilyState != nil || manifest.Catalog != nil || manifest.BossIDs != nil || manifest.Memos == nil {
		return socialImportBatch{}, fmt.Errorf("%w: memo aggregate fields", errSocialImportManifestInvalid)
	}
	memos := make(map[string][]world.CharacterMemo, len(manifest.Memos))
	seenMemoIDs := make(map[string]string)
	memoCount := 0
	for recipientID, records := range manifest.Memos {
		if !validOpaqueImportID(recipientID) || len(recipientID) > 128 {
			return socialImportBatch{}, fmt.Errorf("%w: invalid recipient ID", errSocialImportManifestInvalid)
		}
		if len(records) > world.MaxMemoRecords || memoCount > world.MaxMemoRecords-len(records) {
			return socialImportBatch{}, fmt.Errorf("%w: memo record limit", errSocialImportManifestInvalid)
		}
		copyRecords := append([]world.CharacterMemo(nil), records...)
		for index := range copyRecords {
			memo := &copyRecords[index]
			if !validOpaqueImportID(memo.ID) || len(memo.ID) > world.MaxMemoIDBytes {
				return socialImportBatch{}, fmt.Errorf("%w: invalid memo ID", errSocialImportManifestInvalid)
			}
			if _, exists := seenMemoIDs[memo.ID]; exists {
				return socialImportBatch{}, fmt.Errorf("%w: duplicate memo ID", errSocialImportManifestInvalid)
			}
			seenMemoIDs[memo.ID] = recipientID
			if !validOpaqueImportID(memo.SenderID) || len(memo.SenderID) > 128 {
				return socialImportBatch{}, fmt.Errorf("%w: invalid memo sender ID", errSocialImportManifestInvalid)
			}
			canonicalSender, err := identity.CanonicalName(memo.SenderName)
			if err != nil || canonicalSender != memo.SenderName {
				return socialImportBatch{}, fmt.Errorf("%w: invalid memo sender name", errSocialImportManifestInvalid)
			}
			if err := world.ValidateMemoBody(memo.Body); err != nil {
				return socialImportBatch{}, fmt.Errorf("%w: invalid memo body", errSocialImportManifestInvalid)
			}
			if memo.CreatedAt.IsZero() || memo.CreatedAt.Before(time.Unix(0, 0)) {
				return socialImportBatch{}, fmt.Errorf("%w: invalid memo timestamp", errSocialImportManifestInvalid)
			}
			memo.CreatedAt = memo.CreatedAt.Round(0).UTC()
		}
		memos[recipientID] = copyRecords
		memoCount += len(copyRecords)
	}
	if len(memos) > world.MaxMemoRecipients {
		return socialImportBatch{}, fmt.Errorf("%w: recipient limit", errSocialImportManifestInvalid)
	}
	for recipientID, recipientName := range manifest.RecipientNames {
		if !validOpaqueImportID(recipientID) || len(recipientID) > 128 {
			return socialImportBatch{}, fmt.Errorf("%w: invalid recipient-name ID", errSocialImportManifestInvalid)
		}
		canonicalName, err := identity.CanonicalName(recipientName)
		if err != nil || canonicalName != recipientName {
			return socialImportBatch{}, fmt.Errorf("%w: invalid recipient name", errSocialImportManifestInvalid)
		}
		if _, ok := memos[recipientID]; !ok {
			return socialImportBatch{}, fmt.Errorf("%w: foreign recipient name", errSocialImportManifestInvalid)
		}
	}

	input := storage.CharacterMemosImport{
		WorldID:          manifest.WorldID,
		CommandID:        manifest.CommandID,
		ExpectedRevision: *manifest.ExpectedRevision,
		Memos:            memos,
		RecipientNames:   copyStringMap(manifest.RecipientNames),
	}
	aggregate, err := socialMemosAggregateSHA256(memos, input.RecipientNames)
	if err != nil {
		return socialImportBatch{}, fmt.Errorf("%w: memo aggregate encoding", errSocialImportManifestInvalid)
	}
	return socialImportBatch{
		Kind:             manifest.Kind,
		WorldID:          manifest.WorldID,
		CommandID:        manifest.CommandID,
		ExpectedRevision: *manifest.ExpectedRevision,
		AggregateSHA256:  aggregate,
		Memos:            &input,
		RecipientCount:   len(memos),
		MemoCount:        memoCount,
	}, nil
}

func copyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func socialFamilyMemberCount(family world.FamilyState) int {
	count := 0
	for _, members := range family.Members {
		count += len(members)
	}
	return count
}

func socialFamilyAggregateSHA256(family world.FamilyState, catalog world.FamilyCatalog, bossIDs map[int16]string) ([sha256.Size]byte, error) {
	payload, err := json.Marshal(struct {
		Family  world.FamilyState   `json:"family"`
		Catalog world.FamilyCatalog `json:"catalog"`
		BossIDs map[int16]string    `json:"boss_ids"`
	}{family, catalog, bossIDs})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

func socialMemosAggregateSHA256(memos map[string][]world.CharacterMemo, recipientNames map[string]string) ([sha256.Size]byte, error) {
	payload, err := json.Marshal(struct {
		Memos          map[string][]world.CharacterMemo `json:"memos"`
		RecipientNames map[string]string                `json:"recipient_names,omitempty"`
	}{memos, recipientNames})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(payload), nil
}

// runSocialImport is the sole CLI-to-storage call site. Callers must invoke it
// only after validating options and explicitly selecting Apply. It performs
// exactly one reviewed aggregate import and never starts a listener.
func runSocialImport(ctx context.Context, repo *storage.Postgres, batch socialImportBatch) error {
	if repo == nil {
		return errors.New("nil postgres store")
	}
	if ctx == nil {
		return errors.New("nil import context")
	}
	switch batch.Kind {
	case socialImportKindFamily:
		if batch.Family == nil {
			return errSocialImportManifestInvalid
		}
		result, err := repo.ImportFamilyLedger(ctx, *batch.Family)
		if err != nil {
			return fmt.Errorf("family ledger import command %q: %w", batch.CommandID, err)
		}
		logSocialFamilyImportResult(result)
		return nil
	case socialImportKindMemos:
		if batch.Memos == nil {
			return errSocialImportManifestInvalid
		}
		result, err := repo.ImportCharacterMemos(ctx, *batch.Memos)
		if err != nil {
			return fmt.Errorf("character memos import command %q: %w", batch.CommandID, err)
		}
		logSocialMemosImportResult(result)
		return nil
	default:
		return errSocialImportManifestInvalid
	}
}

func logSocialFamilyImportResult(result storage.FamilyLedgerImportResult) {
	mode := "imported"
	if result.Replayed {
		mode = "replayed"
	}
	// Aggregate hashes and counts are safe operator evidence. No memo body,
	// password, raw path, or credential material is printed.
	fmt.Printf("social family ledger %s: revision=%d families=%d members=%d aggregate_sha256=%x\n", mode, result.Revision, result.FamilyCount, result.MemberCount, result.AggregateSHA256)
}

func logSocialMemosImportResult(result storage.CharacterMemosImportResult) {
	mode := "imported"
	if result.Replayed {
		mode = "replayed"
	}
	fmt.Printf("social character memos %s: revision=%d recipients=%d memos=%d aggregate_sha256=%x\n", mode, result.Revision, result.RecipientCount, result.MemoCount, result.AggregateSHA256)
}
