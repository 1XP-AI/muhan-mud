package storage

// This file is deliberately an operator-only boundary.  It imports reviewed,
// pointer-free projections; it never opens legacy files, resolves a character
// by name, or accepts credentials as an identity claim.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var (
	ErrFamilyLedgerImport           = errors.New("family ledger import rejected")
	ErrFamilyLedgerImportConflict   = errors.New("family ledger import conflict")
	ErrCharacterMemosImport         = errors.New("character memos import rejected")
	ErrCharacterMemosImportConflict = errors.New("character memos import conflict")
)

// FamilyLedgerImport is a reviewed canonical aggregate. Family and Catalog
// are caller-owned projections: member IDs are immutable character IDs,
// names/classes are integrity evidence, and Catalog.Fee is the source fee
// unit. SourcePath, RawPath, and CredentialHash are intentionally forbidden;
// they exist only so accidental raw/credential plumbing fails closed.
type FamilyLedgerImport struct {
	WorldID          string
	CommandID        string
	ExpectedRevision int64
	Family           world.FamilyState
	FamilyState      world.FamilyState
	Catalog          world.FamilyCatalog
	Families         []FamilyLedgerFamily
	SourcePath       string
	RawPath          string
	CredentialHash   []byte
}

// FamilyLedgerFamily is an optional row-oriented spelling for operators that
// do not want to construct world.FamilyState/FamilyCatalog separately.
// BossID is optional display evidence only; no name is ever used to claim it.
type FamilyLedgerFamily struct {
	ID       int16
	Name     string
	BossID   string
	BossName string
	Fee      int64
	Members  []world.FamilyMember
}

type FamilyLedgerImportResult struct {
	Revision        int64
	FamilyCount     int
	MemberCount     int
	AggregateSHA256 [sha256.Size]byte
	Replayed        bool
}

type familyLedgerReceipt struct {
	Revision        int64             `json:"revision"`
	FamilyCount     int               `json:"family_count"`
	MemberCount     int               `json:"member_count"`
	AggregateSHA256 [sha256.Size]byte `json:"aggregate_sha256"`
}

// CharacterMemosImport is a complete State.Memos projection keyed by exact
// canonical character IDs. A nonnil empty map is an explicit empty import;
// nil is not accepted as a successful migration.
type CharacterMemosImport struct {
	WorldID          string
	CommandID        string
	ExpectedRevision int64
	Memos            map[string][]world.CharacterMemo
	Records          map[string][]world.CharacterMemo
	RecipientNames   map[string]string
	SourcePath       string
	RawPath          string
	CredentialHash   []byte
}

// CharacterMemoImport is a compatibility spelling for operator callers.
type CharacterMemoImport = CharacterMemosImport

type CharacterMemosImportResult struct {
	Revision        int64
	RecipientCount  int
	MemoCount       int
	AggregateSHA256 [sha256.Size]byte
	Replayed        bool
}

type characterMemosReceipt struct {
	Revision        int64             `json:"revision"`
	RecipientCount  int               `json:"recipient_count"`
	MemoCount       int               `json:"memo_count"`
	AggregateSHA256 [sha256.Size]byte `json:"aggregate_sha256"`
}

type familyLedgerRequest struct {
	Kind      string            `json:"kind"`
	Aggregate [sha256.Size]byte `json:"aggregate_sha256"`
}

type characterMemosRequest struct {
	Kind      string            `json:"kind"`
	Aggregate [sha256.Size]byte `json:"aggregate_sha256"`
}

type normalizedFamilyImport struct {
	input       FamilyLedgerImport
	family      world.FamilyState
	catalog     world.FamilyCatalog
	bossIDs     map[int16]string
	aggregate   [sha256.Size]byte
	familyCount int
	memberCount int
}

type normalizedMemosImport struct {
	input      CharacterMemosImport
	memos      map[string][]world.CharacterMemo
	aggregate  [sha256.Size]byte
	recipients int
	memoCount  int
}

func validImportToken(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '/' || r == '\\' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validImportText(value string, max int) bool {
	if !validImportToken(value, max) {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func canonicalPlayerName(name string) bool {
	canonical, err := identity.CanonicalName(name)
	return err == nil && canonical == name
}

func validateImportEnvelope(worldID, commandID string, revision int64) error {
	if !validImportToken(worldID, 128) || !validImportToken(commandID, 128) || revision < 0 {
		return ErrFamilyLedgerImport
	}
	return nil
}

func validateNoSensitiveImportFields(sourcePath, rawPath string, credential []byte) error {
	if sourcePath != "" || rawPath != "" || len(credential) != 0 {
		return errors.New("operator import cannot contain credential or raw path")
	}
	return nil
}

func normalizeFamilyLedgerImport(input FamilyLedgerImport) (normalizedFamilyImport, error) {
	if err := validateImportEnvelope(input.WorldID, input.CommandID, input.ExpectedRevision); err != nil {
		return normalizedFamilyImport{}, err
	}
	if err := validateNoSensitiveImportFields(input.SourcePath, input.RawPath, input.CredentialHash); err != nil {
		return normalizedFamilyImport{}, fmt.Errorf("%w: %v", ErrFamilyLedgerImport, err)
	}
	family := input.FamilyState
	if family.Members == nil {
		family = input.Family
	}
	catalog := input.Catalog
	bossIDs := make(map[int16]string)
	if input.Families != nil {
		derivedFamily := world.FamilyState{Members: make(map[int16][]world.FamilyMember, len(input.Families))}
		derivedCatalog := world.FamilyCatalog{Families: make(map[int16]world.FamilyDefinition, len(input.Families))}
		for _, row := range input.Families {
			if row.ID < 1 || row.ID > world.FamilyMaxID || !validImportText(row.Name, 256) || row.Fee < 0 || row.Fee > math.MaxInt32/20000 || row.BossName == "" || !canonicalPlayerName(row.BossName) || (row.BossID != "" && !validImportToken(row.BossID, 128)) {
				return normalizedFamilyImport{}, fmt.Errorf("%w: invalid family row", ErrFamilyLedgerImport)
			}
			if _, exists := derivedFamily.Members[row.ID]; exists {
				return normalizedFamilyImport{}, fmt.Errorf("%w: duplicate family ID", ErrFamilyLedgerImport)
			}
			derivedFamily.Members[row.ID] = append([]world.FamilyMember(nil), row.Members...)
			derivedCatalog.Families[row.ID] = world.FamilyDefinition{ID: row.ID, Name: row.Name, Boss: row.BossName, Fee: row.Fee}
			bossIDs[row.ID] = row.BossID
		}
		if family.Members != nil && !equalJSON(family, derivedFamily) {
			return normalizedFamilyImport{}, fmt.Errorf("%w: family projections disagree", ErrFamilyLedgerImport)
		}
		family = derivedFamily
		if catalog.Families == nil {
			catalog = derivedCatalog
		}
	}
	if family.Members == nil || catalog.Families == nil {
		return normalizedFamilyImport{}, fmt.Errorf("%w: explicit family and fee ledger required", ErrFamilyLedgerImport)
	}
	if err := family.Validate(); err != nil {
		return normalizedFamilyImport{}, fmt.Errorf("%w: %v", ErrFamilyLedgerImport, err)
	}
	if err := catalog.Validate(); err != nil {
		return normalizedFamilyImport{}, fmt.Errorf("%w: %v", ErrFamilyLedgerImport, err)
	}
	if len(family.Members) != len(catalog.Families) {
		return normalizedFamilyImport{}, fmt.Errorf("%w: family/catalog ID set mismatch", ErrFamilyLedgerImport)
	}
	for familyID, members := range family.Members {
		definition, ok := catalog.Families[familyID]
		if !ok || definition.ID != familyID || definition.Fee < 0 || definition.Fee > math.MaxInt32/20000 || !validImportText(definition.Name, 256) || !canonicalPlayerName(definition.Boss) {
			return normalizedFamilyImport{}, fmt.Errorf("%w: invalid fee/catalog evidence", ErrFamilyLedgerImport)
		}
		for _, member := range members {
			if !validImportToken(member.ID, 128) || !canonicalPlayerName(member.Name) {
				return normalizedFamilyImport{}, fmt.Errorf("%w: invalid member identity", ErrFamilyLedgerImport)
			}
		}
	}
	payload, err := json.Marshal(struct {
		Family  world.FamilyState   `json:"family"`
		Catalog world.FamilyCatalog `json:"catalog"`
		BossIDs map[int16]string    `json:"boss_ids"`
	}{family, catalog, bossIDs})
	if err != nil {
		return normalizedFamilyImport{}, fmt.Errorf("%w: aggregate encoding", ErrFamilyLedgerImport)
	}
	return normalizedFamilyImport{input: input, family: family, catalog: catalog, bossIDs: bossIDs, aggregate: sha256.Sum256(payload), familyCount: len(family.Members), memberCount: familyMemberCount(family)}, nil
}

func familyMemberCount(family world.FamilyState) int {
	count := 0
	for _, members := range family.Members {
		count += len(members)
	}
	return count
}

func normalizeMemosImport(input CharacterMemosImport) (normalizedMemosImport, error) {
	if !validImportToken(input.WorldID, 128) || !validImportToken(input.CommandID, 128) || input.ExpectedRevision < 0 {
		return normalizedMemosImport{}, ErrCharacterMemosImport
	}
	if err := validateNoSensitiveImportFields(input.SourcePath, input.RawPath, input.CredentialHash); err != nil {
		return normalizedMemosImport{}, fmt.Errorf("%w: %v", ErrCharacterMemosImport, err)
	}
	memos := input.Memos
	if memos == nil {
		memos = input.Records
	}
	if memos == nil {
		return normalizedMemosImport{}, fmt.Errorf("%w: nil memo aggregate is unresolved", ErrCharacterMemosImport)
	}
	copyMemos := make(map[string][]world.CharacterMemo, len(memos))
	for recipientID, records := range memos {
		if !validImportToken(recipientID, 128) {
			return normalizedMemosImport{}, fmt.Errorf("%w: invalid recipient ID", ErrCharacterMemosImport)
		}
		if name, ok := input.RecipientNames[recipientID]; ok && (!canonicalPlayerName(name) || !validImportText(name, 256)) {
			return normalizedMemosImport{}, fmt.Errorf("%w: invalid recipient name", ErrCharacterMemosImport)
		}
		copyMemos[recipientID] = append([]world.CharacterMemo(nil), records...)
		for index := range copyMemos[recipientID] {
			memo := &copyMemos[recipientID][index]
			if memo.CreatedAt.IsZero() || memo.CreatedAt.Before(unixEpoch) {
				return normalizedMemosImport{}, fmt.Errorf("%w: invalid timestamp", ErrCharacterMemosImport)
			}
			memo.CreatedAt = memo.CreatedAt.Round(0).UTC()
		}
	}
	if input.RecipientNames != nil {
		for recipientID := range input.RecipientNames {
			if _, ok := copyMemos[recipientID]; !ok {
				return normalizedMemosImport{}, fmt.Errorf("%w: foreign recipient name", ErrCharacterMemosImport)
			}
		}
	}
	payload, err := json.Marshal(struct {
		Memos          map[string][]world.CharacterMemo `json:"memos"`
		RecipientNames map[string]string                `json:"recipient_names,omitempty"`
	}{copyMemos, input.RecipientNames})
	if err != nil {
		return normalizedMemosImport{}, fmt.Errorf("%w: aggregate encoding", ErrCharacterMemosImport)
	}
	count := 0
	for _, records := range copyMemos {
		count += len(records)
	}
	return normalizedMemosImport{input: input, memos: copyMemos, aggregate: sha256.Sum256(payload), recipients: len(copyMemos), memoCount: count}, nil
}

var unixEpoch = epochTime()

func epochTime() (t time.Time) { return time.Unix(0, 0) }

func equalJSON(a, b any) bool {
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ra, rb)
}

func (p *Postgres) ImportFamilyLedger(ctx context.Context, input FamilyLedgerImport) (FamilyLedgerImportResult, error) {
	normalized, err := normalizeFamilyLedgerImport(input)
	if err != nil || p == nil || p.db == nil || ctx == nil {
		if err != nil {
			return FamilyLedgerImportResult{}, err
		}
		return FamilyLedgerImportResult{}, ErrFamilyLedgerImport
	}
	request := familyLedgerRequest{Kind: "import-family-ledger-v1", Aggregate: normalized.aggregate}
	requestRaw, _ := json.Marshal(request)
	requestDigest := sha256.Sum256(requestRaw)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return FamilyLedgerImportResult{}, err
	}
	defer tx.Rollback()
	var revision, epoch int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT revision,state,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, input.WorldID).Scan(&revision, &raw, &epoch); err != nil {
		return FamilyLedgerImportResult{}, err
	}
	if err = p.checkWriter(input.WorldID, epoch); err != nil {
		return FamilyLedgerImportResult{}, err
	}
	var storedHash, storedResponse []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, input.WorldID, input.CommandID).Scan(&storedHash, &storedResponse)
	if err == nil {
		if !bytes.Equal(storedHash, requestDigest[:]) {
			return FamilyLedgerImportResult{}, ErrCommandConflict
		}
		var receipt familyLedgerReceipt
		if json.Unmarshal(storedResponse, &receipt) != nil {
			return FamilyLedgerImportResult{}, ErrFamilyLedgerImport
		}
		return FamilyLedgerImportResult{Revision: receipt.Revision, FamilyCount: receipt.FamilyCount, MemberCount: receipt.MemberCount, AggregateSHA256: receipt.AggregateSHA256, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return FamilyLedgerImportResult{}, err
	}
	if revision != input.ExpectedRevision {
		return FamilyLedgerImportResult{}, ErrWorldConflict
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return FamilyLedgerImportResult{}, err
	}
	if state.Family != nil {
		return FamilyLedgerImportResult{}, ErrFamilyLedgerImportConflict
	}
	if err := validateUniquePlayerNames(state); err != nil {
		return FamilyLedgerImportResult{}, fmt.Errorf("%w: %v", ErrFamilyLedgerImportConflict, err)
	}
	for familyID, bossID := range normalized.bossIDs {
		if bossID == "" {
			continue
		}
		boss, ok := state.Players[bossID]
		if !ok || boss.Body.Name != normalized.catalog.Families[familyID].Boss {
			return FamilyLedgerImportResult{}, fmt.Errorf("%w: foreign boss identity", ErrFamilyLedgerImportConflict)
		}
		found := false
		for _, member := range normalized.family.Members[familyID] {
			if member.ID == bossID {
				found = true
				break
			}
		}
		if !found {
			return FamilyLedgerImportResult{}, fmt.Errorf("%w: boss is absent from family ledger", ErrFamilyLedgerImportConflict)
		}
	}
	next, err := state.WithFamilyState(normalized.family)
	if err != nil {
		return FamilyLedgerImportResult{}, fmt.Errorf("%w: %v", ErrFamilyLedgerImport, err)
	}
	stateJSON, err := json.Marshal(next)
	if err != nil {
		return FamilyLedgerImportResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE mud_go.worlds SET state=$2,revision=revision+1 WHERE id=$1 RETURNING revision`, input.WorldID, string(stateJSON)).Scan(&revision); err != nil {
		return FamilyLedgerImportResult{}, err
	}
	receipt := familyLedgerReceipt{Revision: revision, FamilyCount: normalized.familyCount, MemberCount: normalized.memberCount, AggregateSHA256: normalized.aggregate}
	response, _ := json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.family_imports(world_id,command_id,aggregate_sha256,family_count,member_count,imported_revision) VALUES($1,$2,$3,$4,$5,$6)`, input.WorldID, input.CommandID, normalized.aggregate[:], normalized.familyCount, normalized.memberCount, revision); err != nil {
		return FamilyLedgerImportResult{}, err
	}
	for familyID, members := range normalized.family.Members {
		definition := normalized.catalog.Families[familyID]
		if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.family_catalog(world_id,family_id,family_name,boss_id,boss_name,fee,command_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, input.WorldID, familyID, definition.Name, normalized.bossIDs[familyID], definition.Boss, definition.Fee, input.CommandID); err != nil {
			return FamilyLedgerImportResult{}, err
		}
		for position, member := range members {
			if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.family_members(world_id,family_id,member_position,character_id,character_name,character_class,command_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, input.WorldID, familyID, position, member.ID, member.Name, member.Class, input.CommandID); err != nil {
				return FamilyLedgerImportResult{}, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response) VALUES($1,$2,$3,$4,$5)`, input.WorldID, input.CommandID, requestDigest[:], revision, string(response)); err != nil {
		return FamilyLedgerImportResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return FamilyLedgerImportResult{}, err
	}
	return FamilyLedgerImportResult{Revision: revision, FamilyCount: normalized.familyCount, MemberCount: normalized.memberCount, AggregateSHA256: normalized.aggregate}, nil
}

func (p *Postgres) ImportCharacterMemos(ctx context.Context, input CharacterMemosImport) (CharacterMemosImportResult, error) {
	normalized, err := normalizeMemosImport(input)
	if err != nil || p == nil || p.db == nil || ctx == nil {
		if err != nil {
			return CharacterMemosImportResult{}, err
		}
		return CharacterMemosImportResult{}, ErrCharacterMemosImport
	}
	request := characterMemosRequest{Kind: "import-character-memos-v1", Aggregate: normalized.aggregate}
	requestRaw, _ := json.Marshal(request)
	requestDigest := sha256.Sum256(requestRaw)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return CharacterMemosImportResult{}, err
	}
	defer tx.Rollback()
	var revision, epoch int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT revision,state,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, input.WorldID).Scan(&revision, &raw, &epoch); err != nil {
		return CharacterMemosImportResult{}, err
	}
	if err = p.checkWriter(input.WorldID, epoch); err != nil {
		return CharacterMemosImportResult{}, err
	}
	var storedHash, storedResponse []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, input.WorldID, input.CommandID).Scan(&storedHash, &storedResponse)
	if err == nil {
		if !bytes.Equal(storedHash, requestDigest[:]) {
			return CharacterMemosImportResult{}, ErrCommandConflict
		}
		var receipt characterMemosReceipt
		if json.Unmarshal(storedResponse, &receipt) != nil {
			return CharacterMemosImportResult{}, ErrCharacterMemosImport
		}
		return CharacterMemosImportResult{Revision: receipt.Revision, RecipientCount: receipt.RecipientCount, MemoCount: receipt.MemoCount, AggregateSHA256: receipt.AggregateSHA256, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CharacterMemosImportResult{}, err
	}
	if revision != input.ExpectedRevision {
		return CharacterMemosImportResult{}, ErrWorldConflict
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return CharacterMemosImportResult{}, err
	}
	if state.Memos != nil {
		return CharacterMemosImportResult{}, ErrCharacterMemosImportConflict
	}
	if err := validateUniquePlayerNames(state); err != nil {
		return CharacterMemosImportResult{}, fmt.Errorf("%w: %v", ErrCharacterMemosImportConflict, err)
	}
	for recipientID, recipientName := range input.RecipientNames {
		recipient, ok := state.Players[recipientID]
		if !ok || recipient.Body.Name != recipientName {
			return CharacterMemosImportResult{}, fmt.Errorf("%w: foreign recipient identity", ErrCharacterMemosImportConflict)
		}
	}
	next := state
	next.Memos = normalized.memos
	if err := next.Validate(); err != nil {
		return CharacterMemosImportResult{}, fmt.Errorf("%w: %v", ErrCharacterMemosImport, err)
	}
	stateJSON, err := json.Marshal(next)
	if err != nil {
		return CharacterMemosImportResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE mud_go.worlds SET state=$2,revision=revision+1 WHERE id=$1 RETURNING revision`, input.WorldID, string(stateJSON)).Scan(&revision); err != nil {
		return CharacterMemosImportResult{}, err
	}
	receipt := characterMemosReceipt{Revision: revision, RecipientCount: normalized.recipients, MemoCount: normalized.memoCount, AggregateSHA256: normalized.aggregate}
	response, _ := json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.character_memo_imports(world_id,command_id,aggregate_sha256,recipient_count,memo_count,imported_revision) VALUES($1,$2,$3,$4,$5,$6)`, input.WorldID, input.CommandID, normalized.aggregate[:], normalized.recipients, normalized.memoCount, revision); err != nil {
		return CharacterMemosImportResult{}, err
	}
	for recipientID, records := range normalized.memos {
		for position, memo := range records {
			if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.character_memos(world_id,recipient_id,memo_position,memo_id,sender_id,sender_name,body,created_at,command_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, input.WorldID, recipientID, position, memo.ID, memo.SenderID, memo.SenderName, memo.Body, memo.CreatedAt, input.CommandID); err != nil {
				return CharacterMemosImportResult{}, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response) VALUES($1,$2,$3,$4,$5)`, input.WorldID, input.CommandID, requestDigest[:], revision, string(response)); err != nil {
		return CharacterMemosImportResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return CharacterMemosImportResult{}, err
	}
	return CharacterMemosImportResult{Revision: revision, RecipientCount: normalized.recipients, MemoCount: normalized.memoCount, AggregateSHA256: normalized.aggregate}, nil
}

func validateUniquePlayerNames(state world.State) error {
	seen := make(map[string]string, len(state.Players))
	for id, player := range state.Players {
		if !validImportToken(id, 128) || !canonicalPlayerName(player.Body.Name) {
			return errors.New("foreign or non-canonical player identity")
		}
		if previous, ok := seen[player.Body.Name]; ok && previous != id {
			return errors.New("ambiguous player name")
		}
		seen[player.Body.Name] = id
	}
	return nil
}
