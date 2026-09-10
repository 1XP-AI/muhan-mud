package main

// This file is the DB-free promotion boundary from audited legacy vote files
// to a reviewed canonical VoteState manifest. Raw paths and bytes are used
// only while building evidence; the emitted manifest contains explicit player
// IDs, choice bytes, and digests, and the apply path is a separate command.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	voteStateManifestVersion    = 1
	voteStateManifestKind       = "vote-state-v1"
	maxVoteStateManifestBytes   = 8 << 20
	maxVoteStateManifestRecords = 100000
	maxVoteStateManifestChoices = world.VoteMaxSelections
)

var (
	errVoteManifestPathRequired       = errors.New("vote state manifest path is required")
	errVoteManifestNotPrivate         = errors.New("vote state manifest must be a private 0600 regular file")
	errVoteManifestNotRegular         = errors.New("vote state manifest must be a regular file")
	errVoteManifestTooLarge           = errors.New("vote state manifest exceeds the size limit")
	errVoteManifestInvalid            = errors.New("invalid vote state manifest")
	errVoteManifestApplyRequired      = errors.New("vote state manifest apply requires -import-vote-manifest")
	errVoteManifestDryRunRequired     = errors.New("vote state manifest dry-run requires -import-vote-manifest")
	errVoteManifestApplyDryRun        = errors.New("vote state manifest apply and dry-run modes are mutually exclusive")
	errVoteManifestBuildRootRequired  = errors.New("vote manifest build source root is required")
	errVoteManifestBuildIssueRequired = errors.New("vote manifest build ISSUE file is required")
	errVoteManifestBuildMappingNeeded = errors.New("vote manifest build mapping path is required")
	errVoteManifestBuildOutputNeeded  = errors.New("vote manifest build output path is required")
	errVoteManifestBuildInvalid       = errors.New("invalid vote manifest build input")
	errVoteManifestBuildOutputInvalid = errors.New("vote manifest output must be a private 0600 file beside the mapping")
	errVoteManifestBuildConflict      = errors.New("vote manifest output already contains different bytes")
	errVoteManifestBuildOverlap       = errors.New("vote manifest output must not overlap a source")
)

type voteStateManifestImportOptions struct {
	ManifestPath string
	DryRun       bool
	Apply        bool
}

type voteStateManifestBuildOptions struct {
	Root        string
	IssueFile   string
	MappingPath string
	OutputPath  string
	DryRun      bool
}

type voteStateManifest struct {
	Version          int                       `json:"version"`
	Kind             string                    `json:"kind"`
	WorldID          string                    `json:"world_id"`
	CommandID        string                    `json:"command_id"`
	ExpectedRevision *int64                    `json:"expected_revision"`
	CatalogDigest    string                    `json:"catalog_digest"`
	Ballots          []voteStateManifestBallot `json:"ballots"`
}

type voteStateManifestBallot struct {
	LegacyName   string `json:"legacy_name"`
	PlayerID     string `json:"player_id"`
	Choices      string `json:"choices"`
	SourceSHA256 string `json:"source_sha256"`
}

type voteStateManifestBuildInput struct {
	Version          int                        `json:"version"`
	Kind             string                     `json:"kind"`
	WorldID          string                     `json:"world_id"`
	CommandID        string                     `json:"command_id"`
	ExpectedRevision *int64                     `json:"expected_revision"`
	CatalogDigest    string                     `json:"catalog_digest,omitempty"`
	Players          []voteStateManifestMapping `json:"players"`
}

type voteStateManifestMapping struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type voteStateManifestBuildResult struct {
	Batch   storage.VoteStateImport
	Raw     []byte
	Ballots int
}

func validateVoteStateManifestImportFlags(path string, dryRun, apply bool) (voteStateManifestImportOptions, error) {
	if path == "" {
		if dryRun {
			return voteStateManifestImportOptions{}, errVoteManifestDryRunRequired
		}
		if apply {
			return voteStateManifestImportOptions{}, errVoteManifestApplyRequired
		}
		return voteStateManifestImportOptions{}, nil
	}
	if dryRun && apply {
		return voteStateManifestImportOptions{}, errVoteManifestApplyDryRun
	}
	return voteStateManifestImportOptions{ManifestPath: path, DryRun: !apply || dryRun, Apply: apply}, nil
}

func validateVoteStateManifestBuildFlags(root, issueFile, mappingPath, outputPath string, dryRun bool) (voteStateManifestBuildOptions, error) {
	if root == "" {
		if issueFile != "" || mappingPath != "" || outputPath != "" || dryRun {
			return voteStateManifestBuildOptions{}, errVoteManifestBuildRootRequired
		}
		return voteStateManifestBuildOptions{}, nil
	}
	if issueFile == "" {
		return voteStateManifestBuildOptions{}, errVoteManifestBuildIssueRequired
	}
	if mappingPath == "" {
		return voteStateManifestBuildOptions{}, errVoteManifestBuildMappingNeeded
	}
	if !dryRun && outputPath == "" {
		return voteStateManifestBuildOptions{}, errVoteManifestBuildOutputNeeded
	}
	return voteStateManifestBuildOptions{Root: root, IssueFile: issueFile, MappingPath: mappingPath, OutputPath: outputPath, DryRun: dryRun}, nil
}

func buildVoteStateManifest(options voteStateManifestBuildOptions) (voteStateManifestBuildResult, error) {
	if options.Root == "" || options.IssueFile == "" || options.MappingPath == "" {
		return voteStateManifestBuildResult{}, errVoteManifestBuildInvalid
	}
	mappingAbs, err := filepath.Abs(options.MappingPath)
	if err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: mapping path: %v", errVoteManifestBuildInvalid, err)
	}
	if _, err := privateDirectoryInfo(filepath.Dir(mappingAbs), errVoteManifestBuildInvalid); err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: mapping directory: %v", errVoteManifestBuildInvalid, err)
	}
	mappingRaw, err := readPrivateBoundedFile(mappingAbs, maxVoteStateManifestBytes, errVoteManifestBuildInvalid, errVoteManifestBuildInvalid, errVoteManifestBuildInvalid)
	if err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: mapping: %v", errVoteManifestBuildInvalid, err)
	}
	var input voteStateManifestBuildInput
	if err := decodeSingleJSON(mappingRaw, &input); err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: mapping JSON: %v", errVoteManifestBuildInvalid, err)
	}
	if err := validateVoteStateManifestBuildInput(input); err != nil {
		return voteStateManifestBuildResult{}, err
	}
	rootAbs, err := filepath.Abs(options.Root)
	if err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: root path: %v", errVoteManifestBuildInvalid, err)
	}
	if pathsOverlap(rootAbs, mappingAbs) {
		return voteStateManifestBuildResult{}, errVoteManifestBuildOverlap
	}
	if options.OutputPath != "" {
		outputAbs, absErr := filepath.Abs(options.OutputPath)
		if absErr != nil || pathsOverlap(rootAbs, outputAbs) || outputAbs == mappingAbs {
			return voteStateManifestBuildResult{}, errVoteManifestBuildOverlap
		}
	}
	catalogSource, err := world.LoadVoteCatalogFile(options.IssueFile)
	if err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: ISSUE: %v", errVoteManifestBuildInvalid, err)
	}
	catalogDigest, err := catalogSource.Catalog.Digest()
	if err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: ISSUE digest: %v", errVoteManifestBuildInvalid, err)
	}
	if input.CatalogDigest != "" && input.CatalogDigest != catalogDigest {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: catalog digest mismatch", errVoteManifestBuildInvalid)
	}
	sources, err := world.LocateLegacyVoteRawFilesV1(rootAbs)
	if err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: raw source: %v", errVoteManifestBuildInvalid, err)
	}
	byName := make(map[string]voteStateManifestMapping, len(input.Players))
	for _, mapping := range input.Players {
		byName[mapping.Name] = mapping
	}
	if len(byName) != len(sources) {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: mapping/source count mismatch", errVoteManifestBuildInvalid)
	}
	ballots := make(map[string]world.VoteBallot, len(sources))
	manifestBallots := make([]voteStateManifestBallot, 0, len(sources))
	for _, source := range sources {
		name := source.Metadata.PlayerName
		mapping, ok := byName[name]
		if !ok {
			return voteStateManifestBuildResult{}, fmt.Errorf("%w: missing mapping for %s", errVoteManifestBuildInvalid, name)
		}
		if len(source.Source) != catalogSource.Catalog.Issue.Number {
			return voteStateManifestBuildResult{}, fmt.Errorf("%w: %s choice count=%d expected=%d", errVoteManifestBuildInvalid, name, len(source.Source), catalogSource.Catalog.Issue.Number)
		}
		for index, choice := range source.Source {
			if choice < 'A' || choice > 'G' {
				return voteStateManifestBuildResult{}, fmt.Errorf("%w: %s invalid choice at %d", errVoteManifestBuildInvalid, name, index)
			}
		}
		if _, exists := ballots[mapping.ID]; exists {
			return voteStateManifestBuildResult{}, fmt.Errorf("%w: duplicate player ID", errVoteManifestBuildInvalid)
		}
		choices := string(source.Source)
		ballots[mapping.ID] = world.VoteBallot{CatalogDigest: catalogDigest, Choices: append([]byte(nil), source.Source...)}
		manifestBallots = append(manifestBallots, voteStateManifestBallot{LegacyName: name, PlayerID: mapping.ID, Choices: choices, SourceSHA256: fmt.Sprintf("%x", source.SHA256)})
	}
	sort.Slice(manifestBallots, func(i, j int) bool { return manifestBallots[i].LegacyName < manifestBallots[j].LegacyName })
	votes := world.VoteState{Ballots: ballots, History: []world.VoteHistoryEntry{}}
	if err := votes.Validate(); err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: canonical ballots: %v", errVoteManifestBuildInvalid, err)
	}
	revision := *input.ExpectedRevision
	manifest := voteStateManifest{Version: voteStateManifestVersion, Kind: voteStateManifestKind, WorldID: input.WorldID, CommandID: input.CommandID, ExpectedRevision: &revision, CatalogDigest: catalogDigest, Ballots: manifestBallots}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: manifest JSON: %v", errVoteManifestBuildInvalid, err)
	}
	raw = append(raw, '\n')
	if int64(len(raw)) > maxVoteStateManifestBytes {
		return voteStateManifestBuildResult{}, fmt.Errorf("%w: manifest size", errVoteManifestBuildInvalid)
	}
	return voteStateManifestBuildResult{Batch: storage.VoteStateImport{WorldID: input.WorldID, CommandID: input.CommandID, ExpectedRevision: revision, CatalogDigest: catalogDigest, Votes: votes}, Raw: raw, Ballots: len(manifestBallots)}, nil
}

func validateVoteStateManifestBuildInput(input voteStateManifestBuildInput) error {
	if input.Version != voteStateManifestVersion || input.Kind != voteStateManifestKind || !validOpaqueImportID(input.WorldID) || len(input.WorldID) > 128 || !validOpaqueImportID(input.CommandID) || len(input.CommandID) > 128 || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.Players == nil || len(input.Players) > maxVoteStateManifestRecords {
		return fmt.Errorf("%w: version, kind, identity, revision, or players", errVoteManifestBuildInvalid)
	}
	seenNames := make(map[string]struct{}, len(input.Players))
	seenIDs := make(map[string]struct{}, len(input.Players))
	for index, mapping := range input.Players {
		if err := world.ValidateLegacyVoteFileLocatorPlayerNameV1(mapping.Name); err != nil {
			return fmt.Errorf("%w: player %d name: %v", errVoteManifestBuildInvalid, index+1, err)
		}
		if !validOpaqueImportID(mapping.ID) || len(mapping.ID) > 128 {
			return fmt.Errorf("%w: player %d ID", errVoteManifestBuildInvalid, index+1)
		}
		if _, exists := seenNames[mapping.Name]; exists {
			return fmt.Errorf("%w: duplicate legacy name", errVoteManifestBuildInvalid)
		}
		if _, exists := seenIDs[mapping.ID]; exists {
			return fmt.Errorf("%w: duplicate player ID", errVoteManifestBuildInvalid)
		}
		seenNames[mapping.Name] = struct{}{}
		seenIDs[mapping.ID] = struct{}{}
	}
	return nil
}

func readVoteStateManifest(path string) (storage.VoteStateImport, error) {
	if path == "" {
		return storage.VoteStateImport{}, errVoteManifestPathRequired
	}
	manifestAbs, err := filepath.Abs(path)
	if err != nil {
		return storage.VoteStateImport{}, err
	}
	raw, err := readPrivateBoundedFile(manifestAbs, maxVoteStateManifestBytes, errVoteManifestNotPrivate, errVoteManifestNotRegular, errVoteManifestTooLarge)
	if err != nil {
		return storage.VoteStateImport{}, err
	}
	var manifest voteStateManifest
	if err := decodeSingleJSON(raw, &manifest); err != nil {
		return storage.VoteStateImport{}, fmt.Errorf("%w: JSON: %v", errVoteManifestInvalid, err)
	}
	if manifest.Version != voteStateManifestVersion || manifest.Kind != voteStateManifestKind || !validOpaqueImportID(manifest.WorldID) || len(manifest.WorldID) > 128 || !validOpaqueImportID(manifest.CommandID) || len(manifest.CommandID) > 128 || manifest.ExpectedRevision == nil || *manifest.ExpectedRevision < 0 || !validVoteManifestDigest(manifest.CatalogDigest) || manifest.Ballots == nil || len(manifest.Ballots) > maxVoteStateManifestRecords {
		return storage.VoteStateImport{}, fmt.Errorf("%w: envelope", errVoteManifestInvalid)
	}
	if _, err := privateDirectoryInfo(filepath.Dir(manifestAbs), errVoteManifestInvalid); err != nil {
		return storage.VoteStateImport{}, fmt.Errorf("%w: manifest directory: %v", errVoteManifestInvalid, err)
	}
	ballots := make(map[string]world.VoteBallot, len(manifest.Ballots))
	seenNames := make(map[string]struct{}, len(manifest.Ballots))
	for index, record := range manifest.Ballots {
		if err := world.ValidateLegacyVoteFileLocatorPlayerNameV1(record.LegacyName); err != nil {
			return storage.VoteStateImport{}, fmt.Errorf("%w: ballot %d name: %v", errVoteManifestInvalid, index+1, err)
		}
		if !validOpaqueImportID(record.PlayerID) || len(record.PlayerID) > 128 || record.Choices == "" || len(record.Choices) > maxVoteStateManifestChoices {
			return storage.VoteStateImport{}, fmt.Errorf("%w: ballot %d identity/choices", errVoteManifestInvalid, index+1)
		}
		if _, exists := seenNames[record.LegacyName]; exists {
			return storage.VoteStateImport{}, fmt.Errorf("%w: duplicate legacy name", errVoteManifestInvalid)
		}
		if _, exists := ballots[record.PlayerID]; exists {
			return storage.VoteStateImport{}, fmt.Errorf("%w: duplicate player ID", errVoteManifestInvalid)
		}
		if !validVoteChoicesString(record.Choices) || !validVoteManifestDigest(record.SourceSHA256) {
			return storage.VoteStateImport{}, fmt.Errorf("%w: ballot %d choice/digest", errVoteManifestInvalid, index+1)
		}
		seenNames[record.LegacyName] = struct{}{}
		ballots[record.PlayerID] = world.VoteBallot{CatalogDigest: manifest.CatalogDigest, Choices: []byte(record.Choices)}
	}
	votes := world.VoteState{Ballots: ballots, History: []world.VoteHistoryEntry{}}
	if err := votes.Validate(); err != nil {
		return storage.VoteStateImport{}, fmt.Errorf("%w: canonical ballots: %v", errVoteManifestInvalid, err)
	}
	return storage.VoteStateImport{WorldID: manifest.WorldID, CommandID: manifest.CommandID, ExpectedRevision: *manifest.ExpectedRevision, CatalogDigest: manifest.CatalogDigest, Votes: votes}, nil
}

func validVoteManifestDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := parseSHA256Hex(value)
	return err == nil
}

func validVoteChoicesString(value string) bool {
	for _, choice := range []byte(value) {
		if choice < 'A' || choice > 'G' {
			return false
		}
	}
	return true
}

func writeVoteStateManifest(outputPath string, manifestRaw []byte, mappingPath, sourceRoot string) error {
	if outputPath == "" {
		return errVoteManifestBuildOutputNeeded
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return errVoteManifestBuildOutputInvalid
	}
	mappingAbs, err := filepath.Abs(mappingPath)
	if err != nil {
		return errVoteManifestBuildOutputInvalid
	}
	if sourceRoot != "" && pathsOverlap(sourceRoot, outputAbs) {
		return errVoteManifestBuildOverlap
	}
	if filepath.Dir(outputAbs) != filepath.Dir(mappingAbs) {
		return errVoteManifestBuildOutputInvalid
	}
	if _, err := privateDirectoryInfo(filepath.Dir(outputAbs), errVoteManifestBuildOutputInvalid); err != nil {
		return err
	}
	if info, err := os.Lstat(outputAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errVoteManifestBuildOutputInvalid
		}
		stored, readErr := os.ReadFile(outputAbs)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(stored, manifestRaw) {
			return errVoteManifestBuildConflict
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writePrivateImmutableFile(outputAbs, manifestRaw); err != nil {
		return fmt.Errorf("%w: %v", errVoteManifestBuildOutputInvalid, err)
	}
	return nil
}

func runVoteStateImport(ctx context.Context, repo *storage.Postgres, batch storage.VoteStateImport) error {
	if ctx == nil || repo == nil {
		return errVoteManifestInvalid
	}
	result, err := repo.ImportVoteState(ctx, batch)
	if err != nil {
		return err
	}
	mode := "imported"
	if result.Replayed {
		mode = "replayed"
	}
	fmt.Printf("vote state %s: world=%s ballots=%d history=%d catalog_digest=%s aggregate_sha256=%x\n", mode, batch.WorldID, result.BallotCount, result.HistoryCount, result.CatalogDigest, result.AggregateSHA256)
	return nil
}
