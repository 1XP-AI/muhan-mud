package main

// This file is the DB-free promotion boundary from descriptor-checked legacy
// family/memo files to the reviewed social import manifest.  Discovery and
// identity mapping stay explicit: a source root never supplies an account or
// character ID by itself, and the resulting manifest contains no raw paths or
// credentials.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const maxSocialManifestBuildMappingBytes int64 = 1 << 20

var (
	errSocialManifestBuildRootRequired        = errors.New("social manifest build source root is required")
	errSocialManifestBuildRootsExclusive      = errors.New("social manifest build accepts exactly one family or memo source root")
	errSocialManifestBuildMappingRequired     = errors.New("social manifest build identity mapping path is required")
	errSocialManifestBuildOutputRequired      = errors.New("social manifest build output path is required")
	errSocialManifestBuildInvalid             = errors.New("invalid social manifest build input")
	errSocialManifestBuildOutputInvalid       = errors.New("social manifest output must be a private 0600 file beside the mapping")
	errSocialManifestBuildOutputConflict      = errors.New("social manifest output already contains different bytes")
	errSocialManifestBuildSourceOutputOverlap = errors.New("social manifest output must not overlap the legacy source root")
)

type socialManifestBuildOptions struct {
	FamilyRoot  string
	MemoRoot    string
	MappingPath string
	OutputPath  string
	DryRun      bool
}

// socialManifestBuildInput is intentionally separate from socialImportManifest.
// It is operator input, not an import payload: family identity and every memo
// recipient/sender ID must be reviewed explicitly before this boundary.
type socialManifestBuildInput struct {
	Version          int                              `json:"version"`
	Kind             string                           `json:"kind"`
	WorldID          string                           `json:"world_id"`
	CommandID        string                           `json:"command_id"`
	ExpectedRevision *int64                           `json:"expected_revision"`
	FamilyIdentity   *world.LegacyFamilyIdentityMapV1 `json:"family_identity,omitempty"`
	Recipients       []socialMemoRecipientMapping     `json:"recipients"`
}

type socialMemoRecipientMapping struct {
	Name              string            `json:"name"`
	ID                string            `json:"id"`
	SenderIDs         map[string]string `json:"sender_ids"`
	SenderIDByName    map[string]string `json:"sender_id_by_name,omitempty"`
	TimestampLocation string            `json:"timestamp_location,omitempty"`
}

type socialManifestBuildResult struct {
	Batch socialImportBatch
	Raw   []byte
}

func validateSocialManifestBuildFlags(familyRoot, memoRoot, mappingPath, outputPath string, dryRun bool) (socialManifestBuildOptions, error) {
	if familyRoot == "" && memoRoot == "" {
		if mappingPath != "" || outputPath != "" || dryRun {
			return socialManifestBuildOptions{}, errSocialManifestBuildRootRequired
		}
		return socialManifestBuildOptions{}, nil
	}
	if familyRoot != "" && memoRoot != "" {
		return socialManifestBuildOptions{}, errSocialManifestBuildRootsExclusive
	}
	if mappingPath == "" {
		return socialManifestBuildOptions{}, errSocialManifestBuildMappingRequired
	}
	if !dryRun && outputPath == "" {
		return socialManifestBuildOptions{}, errSocialManifestBuildOutputRequired
	}
	return socialManifestBuildOptions{
		FamilyRoot: familyRoot, MemoRoot: memoRoot, MappingPath: mappingPath,
		OutputPath: outputPath, DryRun: dryRun,
	}, nil
}

// buildSocialImportManifest reads one explicit source domain and produces the
// same normalized batch consumed by the import path.  It never opens a DB or
// starts a listener; callers may use it for a dry-run or write the returned
// bytes to a private, immutable operator artifact.
func buildSocialImportManifest(options socialManifestBuildOptions) (socialManifestBuildResult, error) {
	if (options.FamilyRoot == "") == (options.MemoRoot == "") || options.MappingPath == "" {
		return socialManifestBuildResult{}, errSocialManifestBuildInvalid
	}
	mappingAbs, err := filepath.Abs(options.MappingPath)
	if err != nil {
		return socialManifestBuildResult{}, fmt.Errorf("%w: mapping path: %v", errSocialManifestBuildInvalid, err)
	}
	if _, err := privateDirectoryInfo(filepath.Dir(mappingAbs), errSocialManifestBuildInvalid); err != nil {
		return socialManifestBuildResult{}, fmt.Errorf("%w: mapping directory: %v", errSocialManifestBuildInvalid, err)
	}
	mappingRaw, err := readPrivateBoundedFile(mappingAbs, maxSocialManifestBuildMappingBytes, errSocialManifestBuildInvalid, errSocialManifestBuildInvalid, errSocialManifestBuildInvalid)
	if err != nil {
		return socialManifestBuildResult{}, fmt.Errorf("%w: mapping file: %v", errSocialManifestBuildInvalid, err)
	}
	var input socialManifestBuildInput
	if err := decodeSingleJSON(mappingRaw, &input); err != nil {
		return socialManifestBuildResult{}, fmt.Errorf("%w: mapping JSON: %v", errSocialManifestBuildInvalid, err)
	}
	if err := validateSocialManifestBuildInput(input, options); err != nil {
		return socialManifestBuildResult{}, err
	}

	var manifest socialImportManifest
	sourceRoot := options.FamilyRoot
	if sourceRoot == "" {
		sourceRoot = options.MemoRoot
	}
	if pathsOverlap(sourceRoot, mappingAbs) {
		return socialManifestBuildResult{}, fmt.Errorf("%w: mapping path", errSocialManifestBuildSourceOutputOverlap)
	}
	if options.OutputPath != "" {
		outputAbs, absErr := filepath.Abs(options.OutputPath)
		if absErr != nil {
			return socialManifestBuildResult{}, fmt.Errorf("%w: output path: %v", errSocialManifestBuildInvalid, absErr)
		}
		if pathsOverlap(sourceRoot, outputAbs) || outputAbs == mappingAbs {
			return socialManifestBuildResult{}, fmt.Errorf("%w: output path", errSocialManifestBuildSourceOutputOverlap)
		}
	}

	if input.Kind == socialImportKindFamily {
		located, locateErr := world.LocateLegacyFamilyRawFilesV1(options.FamilyRoot)
		if locateErr != nil {
			return socialManifestBuildResult{}, fmt.Errorf("%w: family source: %v", errSocialManifestBuildInvalid, locateErr)
		}
		inspection, inspectErr := world.InspectLegacyFamilyRawFilesV1(located, *input.FamilyIdentity)
		if inspectErr != nil {
			return socialManifestBuildResult{}, fmt.Errorf("%w: family inspection: %v", errSocialManifestBuildInvalid, inspectErr)
		}
		revision := *input.ExpectedRevision
		manifest = socialImportManifest{
			Version: socialImportManifestVersion, Kind: socialImportKindFamily,
			WorldID: input.WorldID, CommandID: input.CommandID, ExpectedRevision: &revision,
			FamilyState: &inspection.Family, Catalog: &inspection.Catalog, BossIDs: cloneFamilyBossIDs(inspection.BossIDs),
			Memos: nil, RecipientNames: nil,
		}
	} else {
		memos := make(map[string][]world.CharacterMemo, len(input.Recipients))
		recipientNames := make(map[string]string, len(input.Recipients))
		for index, recipient := range input.Recipients {
			located, locateErr := world.LocateLegacyMemoFileV1(options.MemoRoot, recipient.Name)
			if locateErr != nil {
				return socialManifestBuildResult{}, fmt.Errorf("%w: memo recipient %d source: %v", errSocialManifestBuildInvalid, index+1, locateErr)
			}
			location, locationErr := socialMemoTimestampLocation(recipient.TimestampLocation)
			if locationErr != nil {
				return socialManifestBuildResult{}, fmt.Errorf("%w: memo recipient %d timezone: %v", errSocialManifestBuildInvalid, index+1, locationErr)
			}
			parsed, parseErr := located.Parse(world.LegacyMemoParserOptionsV1{
				ABI: world.LegacyMemoFileV1ABI, RecipientName: recipient.Name, RecipientID: recipient.ID,
				SenderIDs: recipient.SenderIDs, SenderIDByName: recipient.SenderIDByName,
				TimestampLocation: location,
			})
			if parseErr != nil {
				return socialManifestBuildResult{}, fmt.Errorf("%w: memo recipient %d parse: %v", errSocialManifestBuildInvalid, index+1, parseErr)
			}
			memos[recipient.ID] = append([]world.CharacterMemo(nil), parsed.Memos...)
			recipientNames[recipient.ID] = recipient.Name
		}
		revision := *input.ExpectedRevision
		manifest = socialImportManifest{
			Version: socialImportManifestVersion, Kind: socialImportKindMemos,
			WorldID: input.WorldID, CommandID: input.CommandID, ExpectedRevision: &revision,
			Memos: memos, RecipientNames: recipientNames,
		}
	}
	batch, err := normalizeSocialImportManifest(manifest)
	if err != nil {
		return socialManifestBuildResult{}, fmt.Errorf("%w: normalized manifest: %v", errSocialManifestBuildInvalid, err)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return socialManifestBuildResult{}, fmt.Errorf("%w: manifest JSON: %v", errSocialManifestBuildInvalid, err)
	}
	raw = append(raw, '\n')
	if int64(len(raw)) > maxSocialImportManifestBytes {
		return socialManifestBuildResult{}, fmt.Errorf("%w: manifest exceeds size limit", errSocialManifestBuildInvalid)
	}
	return socialManifestBuildResult{Batch: batch, Raw: raw}, nil
}

func normalizeSocialImportManifest(manifest socialImportManifest) (socialImportBatch, error) {
	if manifest.Kind == socialImportKindFamily {
		return normalizeSocialFamilyManifest(manifest)
	}
	if manifest.Kind == socialImportKindMemos {
		return normalizeSocialMemosManifest(manifest)
	}
	return socialImportBatch{}, fmt.Errorf("%w: unsupported kind", errSocialManifestBuildInvalid)
}

func validateSocialManifestBuildInput(input socialManifestBuildInput, options socialManifestBuildOptions) error {
	if input.Version != socialImportManifestVersion ||
		(input.Kind != socialImportKindFamily && input.Kind != socialImportKindMemos) ||
		!validOpaqueImportID(input.WorldID) || len(input.WorldID) > 128 ||
		!validOpaqueImportID(input.CommandID) || len(input.CommandID) > 128 ||
		input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
		return fmt.Errorf("%w: version, kind, identity, or expected_revision", errSocialManifestBuildInvalid)
	}
	if input.Kind == socialImportKindFamily {
		if options.FamilyRoot == "" || input.FamilyIdentity == nil || input.Recipients != nil {
			return fmt.Errorf("%w: family mapping fields", errSocialManifestBuildInvalid)
		}
		return nil
	}
	if options.MemoRoot == "" || input.FamilyIdentity != nil || input.Recipients == nil || len(input.Recipients) == 0 || len(input.Recipients) > world.MaxMemoRecipients {
		return fmt.Errorf("%w: memo mapping fields", errSocialManifestBuildInvalid)
	}
	seenIDs := make(map[string]struct{}, len(input.Recipients))
	seenNames := make(map[string]struct{}, len(input.Recipients))
	for index, recipient := range input.Recipients {
		if err := world.ValidateLegacyMemoFileLocatorNameV1(recipient.Name); err != nil {
			return fmt.Errorf("%w: recipient %d name: %v", errSocialManifestBuildInvalid, index+1, err)
		}
		if !validOpaqueImportID(recipient.ID) || len(recipient.ID) > 128 {
			return fmt.Errorf("%w: recipient %d ID", errSocialManifestBuildInvalid, index+1)
		}
		if _, exists := seenIDs[recipient.ID]; exists {
			return fmt.Errorf("%w: duplicate recipient ID", errSocialManifestBuildInvalid)
		}
		if _, exists := seenNames[recipient.Name]; exists {
			return fmt.Errorf("%w: duplicate recipient name", errSocialManifestBuildInvalid)
		}
		if recipient.SenderIDs == nil && recipient.SenderIDByName == nil {
			return fmt.Errorf("%w: recipient %d sender mapping", errSocialManifestBuildInvalid, index+1)
		}
		seenIDs[recipient.ID] = struct{}{}
		seenNames[recipient.Name] = struct{}{}
	}
	return nil
}

func socialMemoTimestampLocation(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	return location, nil
}

func cloneFamilyBossIDs(values map[int16]string) map[int16]string {
	if values == nil {
		return nil
	}
	clone := make(map[int16]string, len(values))
	for id, value := range values {
		clone[id] = value
	}
	return clone
}

func writeSocialManifestBuild(outputPath string, manifestRaw []byte, mappingPath, sourceRoot string) error {
	if outputPath == "" {
		return errSocialManifestBuildOutputRequired
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("%w: output path: %v", errSocialManifestBuildOutputInvalid, err)
	}
	mappingAbs, err := filepath.Abs(mappingPath)
	if err != nil {
		return fmt.Errorf("%w: mapping path: %v", errSocialManifestBuildOutputInvalid, err)
	}
	if sourceRoot != "" && pathsOverlap(sourceRoot, outputAbs) {
		return errSocialManifestBuildSourceOutputOverlap
	}
	if filepath.Dir(outputAbs) != filepath.Dir(mappingAbs) {
		return errSocialManifestBuildOutputInvalid
	}
	if _, err := privateDirectoryInfo(filepath.Dir(outputAbs), errSocialManifestBuildOutputInvalid); err != nil {
		return err
	}
	if info, err := os.Lstat(outputAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errSocialManifestBuildOutputInvalid
		}
		stored, readErr := os.ReadFile(outputAbs)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(stored, manifestRaw) {
			return errSocialManifestBuildOutputConflict
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writePrivateImmutableFile(outputAbs, manifestRaw); err != nil {
		return fmt.Errorf("%w: %v", errSocialManifestBuildOutputInvalid, err)
	}
	return nil
}
