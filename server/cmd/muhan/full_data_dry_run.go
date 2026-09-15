package main

// This file is the read-only aggregate gate for the reviewed G4 migration
// inputs.  Each source family is still admitted by its existing boundary; the
// aggregate only combines their metadata and performs cross-family checks.  It
// deliberately has no storage or transport dependency.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	fullDataDryRunMode             = "full-data-dry-run"
	fullDataDryRunReportV1         = 1
	fullDataFamilyRooms            = "rooms"
	fullDataFamilyNPCItems         = "npc_item_graph"
	fullDataFamilyPlayers          = "players"
	fullDataFamilySocial           = "social"
	fullDataFamilyBank             = "bank"
	fullDataFamilyVote             = "vote"
	fullDataFamilySocialNames      = "social_family_manifest"
	fullDataFamilySocialMemos      = "social_memo_manifest"
	fullDataCompletionEvidenceGate = "g4_completion_evidence"
)

var (
	errFullDataDryRunOptInRequired = errors.New("full-data source flags require -full-data-dry-run")
	errFullDataDryRunInvalid       = errors.New("invalid full-data dry-run input")
)

// fullDataDryRunOptions contains only explicit source paths.  NPC and item
// graph admission is intentionally derived from the reviewed room catalog by
// the existing canonical import steps; there is no second, guessed source.
type fullDataDryRunOptions struct {
	RoomsPath                string
	PlayerManifestPath       string
	SocialFamilyManifestPath string
	SocialMemoManifestPath   string
	BankManifestPath         string
	VoteManifestPath         string
}

// FullDataDryRunReport is a deterministic, metadata-only report.  It is safe
// to emit to stdout: no source payload, credential, or object text is copied
// into the report.
type FullDataDryRunReport struct {
	Version               int                        `json:"version"`
	Mode                  string                     `json:"mode"`
	Status                string                     `json:"status"`
	Result                string                     `json:"result"`
	Complete              bool                       `json:"complete"`
	EvidenceGate          fullDataEvidenceGateReport `json:"evidence_gate"`
	ExitCode              int                        `json:"exit_code"`
	DatabaseOpened        bool                       `json:"database_opened"`
	ListenerStarted       bool                       `json:"listener_started"`
	OutputsWritten        bool                       `json:"outputs_written"`
	MissingSourceFamilies []string                   `json:"missing_source_families"`
	Sources               []fullDataSourceReport     `json:"sources"`
	Validation            fullDataValidationReport   `json:"validation"`
}

type fullDataSourceReport struct {
	Family          string         `json:"family"`
	Status          string         `json:"status"`
	Path            string         `json:"path,omitempty"`
	SourceSHA256    string         `json:"source_sha256,omitempty"`
	CanonicalSHA256 string         `json:"canonical_sha256,omitempty"`
	Counts          fullDataCounts `json:"counts"`
	Diagnostics     []string       `json:"diagnostics"`
}

// Counts uses one fixed struct rather than maps so JSON key ordering and the
// shape of the report never depend on Go map iteration.
type fullDataCounts struct {
	SourceFiles      int   `json:"source_files"`
	SourceBytes      int64 `json:"source_bytes"`
	CanonicalBytes   int64 `json:"canonical_bytes"`
	CanonicalRooms   int   `json:"canonical_rooms"`
	IgnoredArtifacts int   `json:"ignored_artifacts"`
	BodyExceptions   int   `json:"body_exceptions"`
	NPCs             int   `json:"npcs"`
	ItemNodes        int   `json:"item_nodes"`
	PlayerRecords    int   `json:"player_records"`
	PlayerItemIDs    int   `json:"player_item_ids"`
	SocialFamilies   int   `json:"social_families"`
	FamilyMembers    int   `json:"family_members"`
	MemoRecipients   int   `json:"memo_recipients"`
	Memos            int   `json:"memos"`
	BankRecords      int   `json:"bank_records"`
	BankItemIDs      int   `json:"bank_item_ids"`
	VoteBallots      int   `json:"vote_ballots"`
	VoteHistory      int   `json:"vote_history"`
}

type fullDataValidationReport struct {
	Status             string                `json:"status"`
	Checks             []fullDataCheckReport `json:"checks"`
	DuplicateItemIDs   []string              `json:"duplicate_item_ids"`
	DuplicateSources   []string              `json:"duplicate_sources"`
	MissingReferences  []string              `json:"missing_references"`
	IdentityMismatches []string              `json:"identity_mismatches"`
	LostItemNodes      []string              `json:"lost_item_nodes"`
	OwnershipErrors    []string              `json:"ownership_errors"`
}

type fullDataEvidenceGateReport struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Satisfied   bool     `json:"satisfied"`
	Diagnostics []string `json:"diagnostics"`
}

type fullDataCheckReport struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Diagnostics []string `json:"diagnostics"`
}

type fullDataDryRunError struct {
	Result string
}

func (e fullDataDryRunError) Error() string {
	if e.Result == "" {
		return errFullDataDryRunInvalid.Error()
	}
	return "full-data dry-run " + e.Result
}

// validateFullDataDryRunFlags keeps the aggregate strictly opt-in.  It is
// valid to pass only the mode flag: that produces a non-zero missing-input
// report naming every required family instead of a false PASS.
func validateFullDataDryRunFlags(enabled bool, paths ...string) (fullDataDryRunOptions, error) {
	if len(paths) != 6 {
		return fullDataDryRunOptions{}, errFullDataDryRunInvalid
	}
	if !enabled {
		for _, source := range paths {
			if source != "" {
				return fullDataDryRunOptions{}, errFullDataDryRunOptInRequired
			}
		}
		return fullDataDryRunOptions{}, nil
	}
	return fullDataDryRunOptions{
		RoomsPath:                paths[0],
		PlayerManifestPath:       paths[1],
		SocialFamilyManifestPath: paths[2],
		SocialMemoManifestPath:   paths[3],
		BankManifestPath:         paths[4],
		VoteManifestPath:         paths[5],
	}, nil
}

// RunFullDataDryRun loads all explicitly supplied source families and returns
// a report even when one or more families are missing or invalid.  Callers
// must marshal the report to stdout and use the returned error as the process
// exit signal.
func RunFullDataDryRun(options fullDataDryRunOptions) (FullDataDryRunReport, error) {
	report := newFullDataDryRunReport(options)
	aggregate := fullDataAggregate{
		itemOwners:     make(map[string][]string),
		playerIDs:      make(map[string]string),
		playerNames:    make(map[string]string),
		playerRefs:     make(map[string][]string),
		playerNameRefs: make(map[string][]fullDataNamedPlayerReference),
		worldIDs:       make(map[string]string),
	}

	inspectFullDataRooms(options, &report, &aggregate)
	inspectFullDataPlayers(options, &report, &aggregate)
	inspectFullDataSocial(options, &report, &aggregate)
	inspectFullDataBank(options, &report, &aggregate)
	inspectFullDataVote(options, &report, &aggregate)
	finishFullDataCrossChecks(&report, &aggregate)
	return report, finishFullDataDryRunResult(&report, fullDataCompletionEvidenceGateSatisfied())
}

// finishFullDataDryRunResult separates local source validation from the G4
// completion claim. The command has no input for raw-source provenance,
// interruption recovery, backup/restore, or production evidence, so its
// evidence gate is intentionally unsatisfied in the current CLI.
func finishFullDataDryRunResult(report *FullDataDryRunReport, evidenceGateSatisfied bool) error {
	report.EvidenceGate.Name = fullDataCompletionEvidenceGate
	if len(report.MissingSourceFamilies) > 0 {
		report.Status = "incomplete"
		report.Result = "missing-input"
		report.Complete = false
		report.ExitCode = 1
		return fullDataDryRunError{Result: report.Result}
	}
	if report.Validation.Status != "pass" || fullDataHasInvalidSource(report) {
		report.Status = "fail"
		report.Result = "validation-failed"
		report.Complete = false
		report.ExitCode = 1
		return fullDataDryRunError{Result: report.Result}
	}

	report.EvidenceGate.Satisfied = evidenceGateSatisfied
	if evidenceGateSatisfied {
		report.EvidenceGate.Status = "satisfied"
		report.EvidenceGate.Diagnostics = []string{"explicit G4 completion evidence gate satisfied"}
		addCheck(report, fullDataCompletionEvidenceGate, "pass", report.EvidenceGate.Diagnostics...)
		sort.SliceStable(report.Validation.Checks, func(i, j int) bool {
			return report.Validation.Checks[i].Name < report.Validation.Checks[j].Name
		})
		report.Status = "pass"
		report.Result = "validated"
		report.Complete = true
		report.ExitCode = 0
		return nil
	}

	report.EvidenceGate.Status = "not-satisfied"
	report.EvidenceGate.Diagnostics = []string{"full-data dry-run validates local inputs only; G4 completion evidence is not supplied"}
	addCheck(report, fullDataCompletionEvidenceGate, "incomplete", report.EvidenceGate.Diagnostics...)
	sort.SliceStable(report.Validation.Checks, func(i, j int) bool {
		return report.Validation.Checks[i].Name < report.Validation.Checks[j].Name
	})
	report.Validation.Status = "incomplete"
	report.Status = "incomplete"
	report.Result = "local-input-validation"
	report.Complete = false
	// Local input validation succeeded. Preserve the dry-run's successful process
	// exit while making the non-completion state explicit in the report.
	report.ExitCode = 0
	return nil
}

// fullDataCompletionEvidenceGateSatisfied is deliberately false until a
// separately reviewed G4 evidence bundle is wired into this command. Keeping
// the gate named and centralized prevents local manifest validation from being
// mistaken for migration completion.
func fullDataCompletionEvidenceGateSatisfied() bool {
	return false
}

// MarshalFullDataDryRunReport emits one stable JSON document and a final
// newline. Structs and all report slices are assembled in fixed order.
func MarshalFullDataDryRunReport(report FullDataDryRunReport) ([]byte, error) {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

type fullDataAggregate struct {
	rooms          *world.LegacyRoomCatalog
	state          *world.State
	players        *playerSnapshotImportBatch
	social         *fullDataSocialAggregate
	bank           *bankSnapshotImportBatch
	vote           *storage.VoteStateImport
	itemOwners     map[string][]string
	playerIDs      map[string]string
	playerNames    map[string]string
	playerRefs     map[string][]string
	playerNameRefs map[string][]fullDataNamedPlayerReference
	worldIDs       map[string]string
}

type fullDataNamedPlayerReference struct {
	Source string
	Name   string
}

type fullDataSocialAggregate struct {
	family *socialImportBatch
	memos  *socialImportBatch
}

func newFullDataDryRunReport(options fullDataDryRunOptions) FullDataDryRunReport {
	paths := []struct {
		family string
		path   string
	}{
		{fullDataFamilyRooms, options.RoomsPath},
		{fullDataFamilyNPCItems, options.RoomsPath},
		{fullDataFamilyPlayers, options.PlayerManifestPath},
		{fullDataFamilySocial, ""},
		{fullDataFamilyBank, options.BankManifestPath},
		{fullDataFamilyVote, options.VoteManifestPath},
	}
	sources := make([]fullDataSourceReport, 0, len(paths))
	for _, item := range paths {
		sources = append(sources, fullDataSourceReport{
			Family: item.family, Status: "missing", Path: cleanFullDataPath(item.path),
			Counts: fullDataCounts{}, Diagnostics: []string{},
		})
	}
	// Social is complete only when both reviewed aggregate kinds are supplied.
	if options.SocialFamilyManifestPath != "" || options.SocialMemoManifestPath != "" {
		sources[3].Path = cleanFullDataPath(options.SocialFamilyManifestPath)
		if options.SocialMemoManifestPath != "" {
			sources[3].Diagnostics = append(sources[3].Diagnostics, "memo manifest: "+cleanFullDataPath(options.SocialMemoManifestPath))
		}
	}
	return FullDataDryRunReport{
		Version: fullDataDryRunReportV1, Mode: fullDataDryRunMode,
		Status: "incomplete", Result: "missing-input", Complete: false, ExitCode: 1,
		EvidenceGate: fullDataEvidenceGateReport{
			Name: fullDataCompletionEvidenceGate, Status: "not-satisfied", Satisfied: false,
			Diagnostics: []string{"full-data dry-run validates local inputs only; G4 completion evidence is not supplied"},
		},
		DatabaseOpened: false, ListenerStarted: false, OutputsWritten: false,
		MissingSourceFamilies: []string{}, Sources: sources,
		Validation: fullDataValidationReport{
			Status: "incomplete", Checks: []fullDataCheckReport{},
			DuplicateItemIDs: []string{}, DuplicateSources: []string{},
			MissingReferences: []string{}, IdentityMismatches: []string{}, LostItemNodes: []string{}, OwnershipErrors: []string{},
		},
	}
}

func cleanFullDataPath(value string) string {
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func inspectFullDataRooms(options fullDataDryRunOptions, report *FullDataDryRunReport, aggregate *fullDataAggregate) {
	roomSource := &report.Sources[0]
	npcSource := &report.Sources[1]
	if options.RoomsPath == "" {
		addMissingFamily(report, fullDataFamilyRooms, "rooms source is required (-full-data-rooms)")
		addMissingFamily(report, fullDataFamilyNPCItems, "NPC/item graph source is derived from the reviewed rooms source")
		addCheck(report, "rooms_admission", "missing", "rooms source is required")
		addCheck(report, "npc_item_graph", "missing", "reviewed rooms source is required before canonical graph conversion")
		return
	}
	roomSource.Status = "invalid"
	npcSource.Status = "invalid"
	info, err := os.Stat(options.RoomsPath)
	if err != nil {
		roomSource.Diagnostics = append(roomSource.Diagnostics, "rooms source unavailable: "+err.Error())
		npcSource.Diagnostics = append(npcSource.Diagnostics, "NPC/item graph source unavailable: reviewed rooms source could not be loaded")
		addCheck(report, "rooms_admission", "fail", roomSource.Diagnostics...)
		addCheck(report, "npc_item_graph", "fail", npcSource.Diagnostics...)
		return
	}
	if !info.IsDir() {
		detail := "rooms source is not a directory"
		roomSource.Diagnostics = append(roomSource.Diagnostics, detail)
		npcSource.Diagnostics = append(npcSource.Diagnostics, detail)
		addCheck(report, "rooms_admission", "fail", detail)
		addCheck(report, "npc_item_graph", "fail", detail)
		return
	}
	catalog, err := world.LoadReviewedLegacyRoomCatalog(os.DirFS(options.RoomsPath), world.LegacyRoomCompatibilityPolicy)
	if err != nil {
		detail := "reviewed room catalog admission failed: " + err.Error()
		roomSource.Diagnostics = append(roomSource.Diagnostics, detail)
		npcSource.Diagnostics = append(npcSource.Diagnostics, "NPC/item graph admission blocked: "+err.Error())
		addCheck(report, "rooms_admission", "fail", detail)
		addCheck(report, "npc_item_graph", "fail", npcSource.Diagnostics...)
		return
	}
	manifest := catalog.AdmissionManifest()
	roomSource.Status = "valid"
	roomSource.SourceSHA256 = digestString(manifest.Digest())
	roomSource.Counts.SourceFiles = manifest.TotalFiles()
	roomSource.Counts.CanonicalRooms = catalog.Len()
	roomSource.Counts.IgnoredArtifacts = len(catalog.Ignored())
	roomSource.Counts.BodyExceptions = manifest.BodyExceptions()
	for _, entry := range manifest.Entries() {
		roomSource.Counts.SourceBytes += int64(entry.Size)
	}
	roomSource.Diagnostics = append(roomSource.Diagnostics, "reviewed admission manifest: "+manifest.Version())
	addCheck(report, "rooms_admission", "pass", roomSource.Diagnostics...)
	aggregate.rooms = &catalog

	state, err := catalog.NewState()
	if err == nil {
		npcNext := 0
		state, err = state.ImportNPCs(func() (string, error) {
			npcNext++
			return fmt.Sprintf("npc-%08d", npcNext), nil
		})
	}
	itemNext := 0
	if err == nil {
		state, err = state.ImportRoomItems(func() (string, error) {
			itemNext++
			return fmt.Sprintf("item-%08d", itemNext), nil
		})
	}
	if err == nil {
		state, err = state.ImportNPCItems(func() (string, error) {
			itemNext++
			return fmt.Sprintf("item-%08d", itemNext), nil
		})
	}
	if err != nil {
		detail := "canonical NPC/item graph admission failed: " + err.Error()
		npcSource.Diagnostics = append(npcSource.Diagnostics, detail)
		addCheck(report, "npc_item_graph", "fail", npcSource.Diagnostics...)
		return
	}
	if err := state.Validate(); err != nil {
		detail := "canonical NPC/item graph validation failed: " + err.Error()
		npcSource.Diagnostics = append(npcSource.Diagnostics, detail)
		addCheck(report, "npc_item_graph", "fail", npcSource.Diagnostics...)
		return
	}
	canonicalRaw, err := json.Marshal(state)
	if err != nil {
		detail := "canonical NPC/item graph digest failed: " + err.Error()
		npcSource.Diagnostics = append(npcSource.Diagnostics, detail)
		addCheck(report, "npc_item_graph", "fail", npcSource.Diagnostics...)
		return
	}
	canonicalDigest := sha256.Sum256(canonicalRaw)
	npcSource.Status = "valid"
	npcSource.SourceSHA256 = digestString(manifest.Digest())
	npcSource.CanonicalSHA256 = digestString(canonicalDigest)
	npcSource.Counts.SourceFiles = manifest.CanonicalPaths()
	npcSource.Counts.CanonicalRooms = len(state.Rooms)
	npcSource.Counts.NPCs = len(state.NPCs)
	npcSource.Counts.ItemNodes = countStateItemNodes(state)
	npcSource.Counts.SourceBytes = roomSource.Counts.SourceBytes
	npcSource.Counts.CanonicalBytes = int64(len(canonicalRaw))
	expectedItems := countCatalogItemNodes(catalog)
	if expectedItems != npcSource.Counts.ItemNodes {
		report.Validation.LostItemNodes = append(report.Validation.LostItemNodes, fmt.Sprintf("npc_item_graph: expected=%d observed=%d", expectedItems, npcSource.Counts.ItemNodes))
		npcSource.Diagnostics = append(npcSource.Diagnostics, fmt.Sprintf("item graph loss: expected=%d observed=%d", expectedItems, npcSource.Counts.ItemNodes))
	}
	for id, room := range state.Rooms {
		if room.Items != nil {
			for itemID := range room.Items.Items {
				aggregate.itemOwners[itemID] = append(aggregate.itemOwners[itemID], fmt.Sprintf("room:%d", id))
			}
		}
	}
	for id, npc := range state.NPCs {
		if npc.Items != nil {
			for itemID := range npc.Items.Items {
				aggregate.itemOwners[itemID] = append(aggregate.itemOwners[itemID], "npc:"+id)
			}
		}
	}
	aggregate.state = &state
	npcGraphStatus := npcItemGraphCheckStatus(expectedItems, npcSource.Counts.ItemNodes)
	addCheck(report, "npc_item_graph", npcGraphStatus, npcSource.Diagnostics...)
}

func npcItemGraphCheckStatus(expectedItems, observedItems int) string {
	if expectedItems != observedItems {
		return "fail"
	}
	return "pass"
}

func inspectFullDataPlayers(options fullDataDryRunOptions, report *FullDataDryRunReport, aggregate *fullDataAggregate) {
	source := &report.Sources[2]
	if options.PlayerManifestPath == "" {
		addMissingFamily(report, fullDataFamilyPlayers, "player manifest is required (-full-data-player-manifest)")
		addCheck(report, "player_manifest", "missing", "player manifest is required")
		return
	}
	source.Status = "invalid"
	manifestRaw, err := readPrivateBoundedFile(options.PlayerManifestPath, maxPlayerSnapshotManifestBytes, errPlayerSnapshotManifestNotPrivate, errPlayerSnapshotManifestNotRegular, errPlayerSnapshotManifestTooLarge)
	if err != nil {
		detail := "player manifest unavailable: " + err.Error()
		source.Diagnostics = append(source.Diagnostics, detail)
		addCheck(report, "player_manifest", "fail", detail)
		return
	}
	batch, err := readPlayerSnapshotImportManifest(options.PlayerManifestPath)
	if err != nil {
		detail := "player manifest rejected: " + err.Error()
		source.Diagnostics = append(source.Diagnostics, detail)
		addCheck(report, "player_manifest", "fail", detail)
		return
	}
	manifestDigest := sha256.Sum256(manifestRaw)
	canonicalDigests := make([]string, 0, len(batch.Requests))
	source.Status = "valid"
	source.SourceSHA256 = digestString(manifestDigest)
	source.Counts.SourceFiles = 1
	source.Counts.SourceBytes = int64(len(manifestRaw))
	source.Counts.PlayerRecords = len(batch.Requests)
	// The standalone manifest reader validates the CDTO wire contract, while
	// storage applies the final account-name and room-reference checks against
	// the live world.  The aggregate must close those checks before a dry-run
	// can report that all reviewed inputs are locally valid.
	for _, request := range batch.Requests {
		inspection, inspectErr := world.InspectPlayerSnapshotV1(request.Snapshot)
		if inspectErr != nil {
			detail := fmt.Sprintf("player snapshot validation failed for player_id=%s: %v", request.PlayerID, inspectErr)
			source.Status = "invalid"
			source.Diagnostics = append(source.Diagnostics, detail)
			addCheck(report, "player_manifest", "fail", detail)
			return
		}
		snapshotName := strings.TrimRight(string(inspection.Snapshot.Name[:]), "\x00")
		canonicalSnapshotName, nameErr := identity.CanonicalName(snapshotName)
		if nameErr != nil || canonicalSnapshotName != request.AccountName {
			detail := fmt.Sprintf("player snapshot account identity mismatch for player_id=%s", request.PlayerID)
			report.Validation.IdentityMismatches = append(report.Validation.IdentityMismatches, detail)
			source.Status = "invalid"
			source.Diagnostics = append(source.Diagnostics, detail)
			addCheck(report, "player_manifest", "fail", detail)
			return
		}
		if aggregate.state != nil {
			if _, roomExists := aggregate.state.Rooms[inspection.Snapshot.RoomNumber]; !roomExists {
				detail := fmt.Sprintf("player snapshot room reference missing for player_id=%s room=%d", request.PlayerID, inspection.Snapshot.RoomNumber)
				report.Validation.MissingReferences = append(report.Validation.MissingReferences, detail)
				source.Status = "invalid"
				source.Diagnostics = append(source.Diagnostics, detail)
				addCheck(report, "player_manifest", "fail", detail)
				return
			}
		}
	}
	if aggregate.playerIDs == nil {
		aggregate.playerIDs = make(map[string]string)
	}
	if aggregate.playerNames == nil {
		aggregate.playerNames = make(map[string]string)
	}
	for _, request := range batch.Requests {
		itemCount := len(request.ItemIDs)
		source.Counts.PlayerItemIDs += itemCount
		source.Counts.CanonicalBytes += int64(len(request.Snapshot))
		snapshotDigest := sha256.Sum256(request.Snapshot)
		canonicalDigests = append(canonicalDigests, digestString(snapshotDigest))
		aggregate.playerIDs[request.PlayerID] = "player"
		aggregate.playerNames[request.PlayerID] = request.AccountName
		aggregate.worldIDs[request.WorldID] = fullDataFamilyPlayers
		for _, itemID := range request.ItemIDs {
			aggregate.itemOwners[itemID] = append(aggregate.itemOwners[itemID], "player:"+request.PlayerID)
		}
	}
	source.CanonicalSHA256 = digestString(hashSortedStrings(canonicalDigests))
	aggregate.players = &batch
	addCheck(report, "player_manifest", "pass", fmt.Sprintf("validated %d player records", len(batch.Requests)))
}

func inspectFullDataSocial(options fullDataDryRunOptions, report *FullDataDryRunReport, aggregate *fullDataAggregate) {
	source := &report.Sources[3]
	familyMissing := options.SocialFamilyManifestPath == ""
	memosMissing := options.SocialMemoManifestPath == ""
	if familyMissing {
		addMissingFamily(report, fullDataFamilySocialNames, "social family manifest is required (-full-data-social-family-manifest)")
	}
	if memosMissing {
		addMissingFamily(report, fullDataFamilySocialMemos, "social memo manifest is required (-full-data-social-memo-manifest)")
	}
	if familyMissing || memosMissing {
		addMissingFamily(report, fullDataFamilySocial, "social requires both reviewed family and memo manifests")
		addCheck(report, "social_manifests", "missing", "social requires both reviewed family and memo manifests")
		return
	}
	source.Status = "invalid"
	familyRaw, familyErr := readPrivateBoundedFile(options.SocialFamilyManifestPath, maxSocialImportManifestBytes, errSocialImportManifestNotPrivate, errSocialImportManifestNotRegular, errSocialImportManifestTooLarge)
	memoRaw, memoErr := readPrivateBoundedFile(options.SocialMemoManifestPath, maxSocialImportManifestBytes, errSocialImportManifestNotPrivate, errSocialImportManifestNotRegular, errSocialImportManifestTooLarge)
	if familyErr != nil {
		source.Diagnostics = append(source.Diagnostics, "social family manifest unavailable: "+familyErr.Error())
	}
	if memoErr != nil {
		source.Diagnostics = append(source.Diagnostics, "social memo manifest unavailable: "+memoErr.Error())
	}
	if familyErr != nil || memoErr != nil {
		addCheck(report, "social_manifests", "fail", source.Diagnostics...)
		return
	}
	family, err := readSocialImportManifest(options.SocialFamilyManifestPath)
	if err != nil {
		source.Diagnostics = append(source.Diagnostics, "social family manifest rejected: "+err.Error())
	}
	memos, memoReadErr := readSocialImportManifest(options.SocialMemoManifestPath)
	if memoReadErr != nil {
		source.Diagnostics = append(source.Diagnostics, "social memo manifest rejected: "+memoReadErr.Error())
	}
	if err != nil || memoReadErr != nil {
		addCheck(report, "social_manifests", "fail", source.Diagnostics...)
		return
	}
	if family.Kind != socialImportKindFamily {
		source.Diagnostics = append(source.Diagnostics, "social family source has kind "+family.Kind)
	}
	if memos.Kind != socialImportKindMemos {
		source.Diagnostics = append(source.Diagnostics, "social memo source has kind "+memos.Kind)
	}
	if family.Kind != socialImportKindFamily || memos.Kind != socialImportKindMemos {
		addCheck(report, "social_manifests", "fail", source.Diagnostics...)
		return
	}
	source.Status = "valid"
	source.SourceSHA256 = digestString(hashSortedStrings([]string{digestBytes(familyRaw), digestBytes(memoRaw)}))
	source.CanonicalSHA256 = digestString(hashSortedStrings([]string{digestString(family.AggregateSHA256), digestString(memos.AggregateSHA256)}))
	source.Counts.SourceFiles = 2
	source.Counts.SourceBytes = int64(len(familyRaw) + len(memoRaw))
	source.Counts.SocialFamilies = family.FamilyCount
	source.Counts.FamilyMembers = family.MemberCount
	source.Counts.MemoRecipients = memos.RecipientCount
	source.Counts.Memos = memos.MemoCount
	aggregate.social = &fullDataSocialAggregate{family: &family, memos: &memos}
	aggregate.worldIDs[family.WorldID] = fullDataFamilySocial
	aggregate.worldIDs[memos.WorldID] = fullDataFamilySocial
	if memos.WorldID != family.WorldID {
		report.Validation.MissingReferences = append(report.Validation.MissingReferences, fmt.Sprintf("social world_id mismatch: family=%s memos=%s", family.WorldID, memos.WorldID))
	}
	if family.Family != nil {
		for _, row := range family.Family.Families {
			if row.BossID != "" {
				addCrossSourcePlayerIdentity(aggregate, report, row.BossID, row.BossName, "social-family-boss")
			}
			for _, member := range row.Members {
				addCrossSourcePlayerIdentity(aggregate, report, member.ID, member.Name, "social-family-member")
			}
		}
		// FamilyState is authoritative for member rows. The row spelling above
		// is retained for importer compatibility, but this loop also catches a
		// malformed aggregate that omitted optional row projections.
		for _, members := range family.Family.Family.Members {
			for _, member := range members {
				addCrossSourcePlayerReference(aggregate, report, member.ID, "social-family-member")
			}
		}
	}
	if memos.Memos != nil {
		for recipientID, recipientName := range memos.Memos.RecipientNames {
			addCrossSourcePlayerIdentity(aggregate, report, recipientID, recipientName, "social-memo-recipient")
		}
		for recipientID, records := range memos.Memos.Memos {
			if _, named := memos.Memos.RecipientNames[recipientID]; !named {
				addCrossSourcePlayerReference(aggregate, report, recipientID, "social-memo-recipient")
			}
			for _, memo := range records {
				addCrossSourcePlayerIdentity(aggregate, report, memo.SenderID, memo.SenderName, "social-memo-sender")
			}
		}
	}
	addCheck(report, "social_manifests", "pass", fmt.Sprintf("validated family members=%d memos=%d", family.MemberCount, memos.MemoCount))
}

func inspectFullDataBank(options fullDataDryRunOptions, report *FullDataDryRunReport, aggregate *fullDataAggregate) {
	source := &report.Sources[4]
	if options.BankManifestPath == "" {
		addMissingFamily(report, fullDataFamilyBank, "bank manifest is required (-full-data-bank-manifest)")
		addCheck(report, "bank_manifest", "missing", "bank manifest is required")
		return
	}
	source.Status = "invalid"
	manifestRaw, err := readPrivateBoundedFile(options.BankManifestPath, maxBankSnapshotManifestBytes, errBankSnapshotManifestNotPrivate, errBankSnapshotManifestNotRegular, errBankSnapshotManifestTooLarge)
	if err != nil {
		detail := "bank manifest unavailable: " + err.Error()
		source.Diagnostics = append(source.Diagnostics, detail)
		addCheck(report, "bank_manifest", "fail", detail)
		return
	}
	batch, err := readBankSnapshotImportManifest(options.BankManifestPath)
	if err != nil {
		detail := "bank manifest rejected: " + err.Error()
		source.Diagnostics = append(source.Diagnostics, detail)
		addCheck(report, "bank_manifest", "fail", detail)
		return
	}
	manifestDigest := sha256.Sum256(manifestRaw)
	canonicalDigests := make([]string, 0, len(batch.Requests))
	source.Status = "valid"
	source.SourceSHA256 = digestString(manifestDigest)
	source.Counts.SourceFiles = 1
	source.Counts.SourceBytes = int64(len(manifestRaw))
	source.Counts.BankRecords = len(batch.Requests)
	for _, request := range batch.Requests {
		source.Counts.BankItemIDs += len(request.ItemIDs)
		source.Counts.CanonicalBytes += int64(len(request.Snapshot))
		snapshotDigest := sha256.Sum256(request.Snapshot)
		canonicalDigests = append(canonicalDigests, digestString(snapshotDigest))
		aggregate.worldIDs[request.WorldID] = fullDataFamilyBank
		addCrossSourcePlayerIdentity(aggregate, report, request.PlayerID, request.AccountName, "bank-owner")
		for _, itemID := range request.ItemIDs {
			aggregate.itemOwners[itemID] = append(aggregate.itemOwners[itemID], "bank:"+request.PlayerID)
		}
	}
	source.CanonicalSHA256 = digestString(hashSortedStrings(canonicalDigests))
	aggregate.bank = &batch
	addCheck(report, "bank_manifest", "pass", fmt.Sprintf("validated records=%d", len(batch.Requests)))
}

func inspectFullDataVote(options fullDataDryRunOptions, report *FullDataDryRunReport, aggregate *fullDataAggregate) {
	source := &report.Sources[5]
	if options.VoteManifestPath == "" {
		addMissingFamily(report, fullDataFamilyVote, "vote manifest is required (-full-data-vote-manifest)")
		addCheck(report, "vote_manifest", "missing", "vote manifest is required")
		return
	}
	source.Status = "invalid"
	manifestRaw, err := readPrivateBoundedFile(options.VoteManifestPath, maxVoteStateManifestBytes, errVoteManifestNotPrivate, errVoteManifestNotRegular, errVoteManifestTooLarge)
	if err != nil {
		detail := "vote manifest unavailable: " + err.Error()
		source.Diagnostics = append(source.Diagnostics, detail)
		addCheck(report, "vote_manifest", "fail", detail)
		return
	}
	vote, err := readVoteStateManifest(options.VoteManifestPath)
	if err != nil {
		detail := "vote manifest rejected: " + err.Error()
		source.Diagnostics = append(source.Diagnostics, detail)
		addCheck(report, "vote_manifest", "fail", detail)
		return
	}
	var manifest voteStateManifest
	if err := decodeSingleJSON(manifestRaw, &manifest); err != nil {
		detail := "vote manifest identity evidence rejected: " + err.Error()
		source.Diagnostics = append(source.Diagnostics, detail)
		addCheck(report, "vote_manifest", "fail", detail)
		return
	}
	source.Status = "valid"
	manifestDigest := sha256.Sum256(manifestRaw)
	source.SourceSHA256 = digestString(manifestDigest)
	source.Counts.SourceFiles = 1
	source.Counts.SourceBytes = int64(len(manifestRaw))
	source.Counts.VoteBallots = len(vote.Votes.Ballots)
	source.Counts.VoteHistory = len(vote.Votes.History)
	voteRaw, marshalErr := json.Marshal(vote.Votes)
	if marshalErr != nil {
		source.Status = "invalid"
		source.Diagnostics = append(source.Diagnostics, "vote aggregate digest failed: "+marshalErr.Error())
	} else {
		digest := sha256.Sum256(voteRaw)
		source.CanonicalSHA256 = digestString(digest)
	}
	aggregate.worldIDs[vote.WorldID] = fullDataFamilyVote
	for _, ballot := range manifest.Ballots {
		addCrossSourcePlayerIdentity(aggregate, report, ballot.PlayerID, ballot.LegacyName, "vote-ballot")
	}
	for _, entry := range vote.Votes.History {
		addCrossSourcePlayerReference(aggregate, report, entry.ActorID, "vote-history")
	}
	aggregate.vote = &vote
	if source.Status == "valid" {
		addCheck(report, "vote_manifest", "pass", fmt.Sprintf("validated ballots=%d history=%d", len(vote.Votes.Ballots), len(vote.Votes.History)))
	} else {
		addCheck(report, "vote_manifest", "fail", source.Diagnostics...)
	}
}

func finishFullDataCrossChecks(report *FullDataDryRunReport, aggregate *fullDataAggregate) {
	// Social's split manifests are one source family; report it only after both
	// readers succeed. This keeps missing family names explicit above while
	// still exposing the two exact missing inputs.
	if aggregate.social != nil && report.Sources[3].Status == "valid" {
		// no-op: the source reader already recorded its check.
	}
	worldIDs := make([]string, 0, len(aggregate.worldIDs))
	for worldID := range aggregate.worldIDs {
		worldIDs = append(worldIDs, worldID)
	}
	sort.Strings(worldIDs)
	if len(worldIDs) > 1 {
		refs := make([]string, 0, len(worldIDs))
		for _, worldID := range worldIDs {
			refs = append(refs, worldID+"("+aggregate.worldIDs[worldID]+")")
		}
		report.Validation.MissingReferences = append(report.Validation.MissingReferences, "world_id mismatch: "+strings.Join(refs, ", "))
	}

	for itemID, owners := range aggregate.itemOwners {
		if len(owners) < 2 {
			continue
		}
		sort.Strings(owners)
		owners = uniqueStrings(owners)
		if len(owners) > 1 {
			report.Validation.DuplicateItemIDs = append(report.Validation.DuplicateItemIDs, itemID)
			report.Validation.OwnershipErrors = append(report.Validation.OwnershipErrors, fmt.Sprintf("item %s owned by %s", itemID, strings.Join(owners, ",")))
		}
	}
	for id, owners := range aggregate.itemOwners {
		if id == "" || len(owners) == 0 {
			report.Validation.OwnershipErrors = append(report.Validation.OwnershipErrors, "empty item owner")
		}
	}
	for id, refs := range aggregate.playerNameRefs {
		if len(refs) == 0 {
			continue
		}
		ordered := append([]fullDataNamedPlayerReference(nil), refs...)
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].Source != ordered[j].Source {
				return ordered[i].Source < ordered[j].Source
			}
			return ordered[i].Name < ordered[j].Name
		})
		if expected, ok := aggregate.playerNames[id]; ok {
			seen := make(map[string]struct{}, len(ordered))
			for _, ref := range ordered {
				key := ref.Source + "\x00" + ref.Name
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				if ref.Name != expected {
					report.Validation.IdentityMismatches = append(report.Validation.IdentityMismatches,
						fmt.Sprintf("player %s name mismatch: player=%s %s=%s", id, expected, ref.Source, ref.Name))
				}
			}
			continue
		}
		observed := make([]string, 0, len(ordered))
		for _, ref := range ordered {
			observed = append(observed, ref.Source+"="+ref.Name)
		}
		observed = uniqueStrings(observed)
		if len(observed) > 1 {
			report.Validation.IdentityMismatches = append(report.Validation.IdentityMismatches,
				fmt.Sprintf("player %s name mismatch across sources: %s", id, strings.Join(observed, ", ")))
		}
	}
	for id, refs := range aggregate.playerRefs {
		if _, ok := aggregate.playerIDs[id]; ok {
			continue
		}
		sort.Strings(refs)
		report.Validation.MissingReferences = append(report.Validation.MissingReferences, fmt.Sprintf("player %s referenced by %s but absent from player manifest", id, strings.Join(uniqueStrings(refs), ",")))
	}
	for _, source := range report.Sources {
		for _, diagnostic := range source.Diagnostics {
			if strings.Contains(strings.ToLower(diagnostic), "duplicate") {
				report.Validation.DuplicateSources = append(report.Validation.DuplicateSources, source.Family+": "+diagnostic)
			}
		}
	}
	sort.Strings(report.Validation.DuplicateItemIDs)
	sort.Strings(report.Validation.DuplicateSources)
	sort.Strings(report.Validation.MissingReferences)
	sort.Strings(report.Validation.IdentityMismatches)
	sort.Strings(report.Validation.LostItemNodes)
	sort.Strings(report.Validation.OwnershipErrors)
	if len(report.Validation.DuplicateItemIDs) > 0 || len(report.Validation.DuplicateSources) > 0 || len(report.Validation.MissingReferences) > 0 || len(report.Validation.IdentityMismatches) > 0 || len(report.Validation.LostItemNodes) > 0 || len(report.Validation.OwnershipErrors) > 0 {
		report.Validation.Status = "fail"
	} else {
		report.Validation.Status = "pass"
	}
	crossSourceDiagnostics := append([]string{}, report.Validation.MissingReferences...)
	crossSourceDiagnostics = append(crossSourceDiagnostics, report.Validation.IdentityMismatches...)
	crossSourceDiagnostics = append(crossSourceDiagnostics, report.Validation.OwnershipErrors...)
	addCheck(report, "cross_source_identity", report.Validation.Status, crossSourceDiagnostics...)
	addCheck(report, "item_ownership", report.Validation.Status,
		append(append([]string{}, report.Validation.DuplicateItemIDs...), report.Validation.LostItemNodes...)...)
	// Stable source diagnostics are useful to a machine caller and prevent a
	// future map-backed implementation from making output order nondeterministic.
	for index := range report.Sources {
		sort.Strings(report.Sources[index].Diagnostics)
	}
	sort.SliceStable(report.Validation.Checks, func(i, j int) bool {
		return report.Validation.Checks[i].Name < report.Validation.Checks[j].Name
	})
	checkMissing := false
	checkFailed := false
	for _, check := range report.Validation.Checks {
		switch check.Status {
		case "missing", "incomplete":
			checkMissing = true
		case "fail":
			checkFailed = true
		}
	}
	switch {
	case checkFailed:
		report.Validation.Status = "fail"
	case checkMissing:
		report.Validation.Status = "incomplete"
	default:
		report.Validation.Status = "pass"
	}
}

func fullDataHasInvalidSource(report *FullDataDryRunReport) bool {
	for _, source := range report.Sources {
		if source.Status != "valid" {
			return true
		}
	}
	return false
}

func addMissingFamily(report *FullDataDryRunReport, family, detail string) {
	for _, existing := range report.MissingSourceFamilies {
		if existing == family {
			return
		}
	}
	report.MissingSourceFamilies = append(report.MissingSourceFamilies, family)
	target := family
	if family == fullDataFamilySocialNames || family == fullDataFamilySocialMemos {
		target = fullDataFamilySocial
	}
	for index := range report.Sources {
		if report.Sources[index].Family == target {
			report.Sources[index].Diagnostics = append(report.Sources[index].Diagnostics, detail)
			if report.Sources[index].Status == "missing" {
				report.Sources[index].Status = "missing"
			}
		}
	}
}

func addCheck(report *FullDataDryRunReport, name, status string, diagnostics ...string) {
	owned := append([]string{}, diagnostics...)
	sort.Strings(owned)
	report.Validation.Checks = append(report.Validation.Checks, fullDataCheckReport{Name: name, Status: status, Diagnostics: owned})
}

func addCrossSourcePlayerReference(aggregate *fullDataAggregate, report *FullDataDryRunReport, id, source string) {
	if id == "" {
		report.Validation.MissingReferences = append(report.Validation.MissingReferences, source+": empty player ID")
		return
	}
	if aggregate.playerRefs == nil {
		aggregate.playerRefs = make(map[string][]string)
	}
	aggregate.playerRefs[id] = append(aggregate.playerRefs[id], source)
}

func addCrossSourcePlayerIdentity(aggregate *fullDataAggregate, report *FullDataDryRunReport, id, name, source string) {
	if id == "" {
		addCrossSourcePlayerReference(aggregate, report, id, source)
		return
	}
	if aggregate.playerNameRefs == nil {
		aggregate.playerNameRefs = make(map[string][]fullDataNamedPlayerReference)
	}
	if name == "" {
		addCrossSourcePlayerReference(aggregate, report, id, source)
		report.Validation.IdentityMismatches = append(report.Validation.IdentityMismatches,
			fmt.Sprintf("player %s name mismatch: %s has empty name", id, source))
		return
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil || canonical != name {
		addCrossSourcePlayerReference(aggregate, report, id, source)
		report.Validation.IdentityMismatches = append(report.Validation.IdentityMismatches,
			fmt.Sprintf("player %s name mismatch: %s=%s is not canonical", id, source, name))
		return
	}
	aggregate.playerRefs[id] = append(aggregate.playerRefs[id], source)
	aggregate.playerNameRefs[id] = append(aggregate.playerNameRefs[id], fullDataNamedPlayerReference{Source: source, Name: name})
}

func countStateItemNodes(state world.State) int {
	count := 0
	for _, room := range state.Rooms {
		if room.Items != nil {
			count += len(room.Items.Items)
		}
	}
	for _, npc := range state.NPCs {
		if npc.Items != nil {
			count += len(npc.Items.Items)
		}
	}
	return count
}

func countCatalogItemNodes(catalog world.LegacyRoomCatalog) int {
	count := 0
	for _, entry := range catalog.AdmissionManifest().Entries() {
		if entry.Class != world.LegacyRoomCanonical || !entry.Admitted {
			continue
		}
		base := path.Base(entry.Path)
		if len(base) != 6 || base[0] != 'r' {
			continue
		}
		id, err := strconv.Atoi(base[1:])
		if err != nil {
			continue
		}
		room, ok := catalog.Room(int16(id))
		if !ok {
			continue
		}
		for _, object := range room.Objects {
			count += countLegacyObjectNodes(object)
		}
		for _, monster := range room.Monsters {
			for _, object := range monster.Inventory {
				count += countLegacyObjectNodes(object)
			}
		}
	}
	return count
}

func countLegacyObjectNodes(object world.LegacyObject) int {
	count := 1
	for _, child := range object.Contents {
		count += countLegacyObjectNodes(child)
	}
	return count
}

func digestString(value [sha256.Size]byte) string {
	return hex.EncodeToString(value[:])
}

func digestBytes(raw []byte) string {
	return digestString(sha256.Sum256(raw))
}

func hashSortedStrings(values []string) [sha256.Size]byte {
	owned := append([]string(nil), values...)
	sort.Strings(owned)
	payload, _ := json.Marshal(owned)
	return sha256.Sum256(payload)
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
