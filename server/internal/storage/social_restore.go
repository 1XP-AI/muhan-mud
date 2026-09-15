package storage

// This file is the read-only restart/recovery boundary for the social import
// tables.  The world snapshot remains the gameplay authority: evidence is
// reconstructed and checked against its immutable import receipt, but it is
// never used to fill, overwrite, or identify a player in the snapshot.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var (
	ErrSocialRestore                     = errors.New("social evidence restore rejected")
	ErrSocialRestoreConflict             = errors.New("social evidence restore conflict")
	ErrFamilyLedgerEvidenceUnavailable   = errors.New("family ledger evidence unavailable")
	ErrCharacterMemosEvidenceUnavailable = errors.New("character memos evidence unavailable")
)

// FamilyLedgerEvidence is the normalized, database-owned evidence for one
// reviewed family import.  Names and classes are integrity projections only;
// none of the fields is an account/name claim or a credential/path input.
// SnapshotRevision is the world revision observed by the read operation, not
// a revision that may be written by recovery.
type FamilyLedgerEvidence struct {
	WorldID          string
	CommandID        string
	SnapshotRevision int64
	ImportedRevision int64
	FamilyCount      int
	MemberCount      int
	AggregateSHA256  [sha256.Size]byte
	Family           world.FamilyState
	Catalog          world.FamilyCatalog
	BossIDs          map[int16]string
}

// CharacterMemosEvidence is the normalized, database-owned evidence for one
// reviewed memo import.  Recipient and sender IDs are already canonical
// server-owned identities; no display-name lookup is performed by recovery.
type CharacterMemosEvidence struct {
	WorldID          string
	CommandID        string
	SnapshotRevision int64
	ImportedRevision int64
	RecipientCount   int
	MemoCount        int
	AggregateSHA256  [sha256.Size]byte
	Memos            map[string][]world.CharacterMemo
}

// SocialRestoreOptions makes recovery an explicit expected-revision read.
// RequireFamily and RequireMemos select which normalized evidence domains
// must be present.  A false flag does not authorize a missing requested
// domain; it merely allows a caller to recover a world that has not admitted
// that domain yet.
type SocialRestoreOptions struct {
	WorldID          string
	ExpectedRevision int64
	RequireFamily    bool
	RequireMemos     bool
}

// SocialRestoreResult is a read-only recovery result. State is decoded from
// mud_go.worlds and is always authoritative. Evidence is secondary metadata
// which was independently verified before being returned.
type SocialRestoreResult struct {
	WorldID  string
	Revision int64
	State    world.State
	Family   *FamilyLedgerEvidence
	Memos    *CharacterMemosEvidence
}

type socialRestoreSnapshot struct {
	revision int64
	epoch    int64
	state    world.State
}

func validateSocialRestoreInput(worldID string, revision int64) error {
	if !validImportToken(worldID, 128) || revision < 0 {
		return fmt.Errorf("%w: invalid world/revision", ErrSocialRestore)
	}
	return nil
}

func (p *Postgres) beginSocialRead(ctx context.Context) (*sql.Tx, error) {
	if p == nil || p.db == nil || ctx == nil {
		return nil, ErrSocialRestore
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// PostgreSQL does not permit row-locking SELECT ... FOR SHARE inside a
	// read-only transaction. The transaction below remains application-level
	// read-only; the default SQL mode is required solely for the consistency
	// lock that fences an in-flight world writer.
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func (p *Postgres) readSocialSnapshotTx(ctx context.Context, tx *sql.Tx, worldID string) (socialRestoreSnapshot, error) {
	var snapshot socialRestoreSnapshot
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT revision,state,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR SHARE`, worldID).Scan(&snapshot.revision, &raw, &snapshot.epoch); err != nil {
		return socialRestoreSnapshot{}, err
	}
	if err := p.checkWriter(worldID, snapshot.epoch); err != nil {
		return socialRestoreSnapshot{}, err
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return socialRestoreSnapshot{}, fmt.Errorf("%w: invalid world snapshot: %v", ErrSocialRestoreConflict, err)
	}
	snapshot.state = state
	return snapshot, nil
}

func readSocialCommandReceipt(ctx context.Context, tx *sql.Tx, worldID, commandID string, requestDigest [sha256.Size]byte, importedRevision int64, target any) error {
	var storedHash, response []byte
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT request_hash,revision,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, commandID).Scan(&storedHash, &revision, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: command receipt missing", ErrSocialRestoreConflict)
	}
	if err != nil {
		return err
	}
	if len(storedHash) != sha256.Size || !bytes.Equal(storedHash, requestDigest[:]) || revision != importedRevision {
		return fmt.Errorf("%w: command receipt mismatch", ErrSocialRestoreConflict)
	}
	if err := decodeSocialJSON(response, target); err != nil {
		return fmt.Errorf("%w: invalid command receipt: %v", ErrSocialRestoreConflict, err)
	}
	return nil
}

func decodeSocialJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func readFamilyImportHeader(ctx context.Context, tx *sql.Tx, worldID string) (FamilyLedgerEvidence, error) {
	rows, err := tx.QueryContext(ctx, `SELECT command_id,aggregate_sha256,family_count,member_count,imported_revision FROM mud_go.family_imports WHERE world_id=$1 ORDER BY command_id`, worldID)
	if err != nil {
		return FamilyLedgerEvidence{}, err
	}
	defer rows.Close()
	var evidence FamilyLedgerEvidence
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: multiple family import evidence rows", ErrSocialRestoreConflict)
		}
		var digest []byte
		if err := rows.Scan(&evidence.CommandID, &digest, &evidence.FamilyCount, &evidence.MemberCount, &evidence.ImportedRevision); err != nil {
			return FamilyLedgerEvidence{}, err
		}
		if len(digest) != sha256.Size {
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: invalid family aggregate digest", ErrSocialRestoreConflict)
		}
		copy(evidence.AggregateSHA256[:], digest)
	}
	if err := rows.Err(); err != nil {
		return FamilyLedgerEvidence{}, err
	}
	if count == 0 {
		var catalogRows, memberRows int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.family_catalog WHERE world_id=$1`, worldID).Scan(&catalogRows); err != nil {
			return FamilyLedgerEvidence{}, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.family_members WHERE world_id=$1`, worldID).Scan(&memberRows); err != nil {
			return FamilyLedgerEvidence{}, err
		}
		if catalogRows != 0 || memberRows != 0 {
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: orphan family evidence rows", ErrSocialRestoreConflict)
		}
		return FamilyLedgerEvidence{}, ErrFamilyLedgerEvidenceUnavailable
	}
	if !validImportToken(evidence.CommandID, 128) || evidence.ImportedRevision <= 0 || evidence.FamilyCount < 0 || evidence.FamilyCount > int(world.FamilyMaxID) || evidence.MemberCount < 0 || evidence.MemberCount > 100000 {
		return FamilyLedgerEvidence{}, fmt.Errorf("%w: invalid family import header", ErrSocialRestoreConflict)
	}
	return evidence, nil
}

func readFamilyLedgerEvidenceTx(ctx context.Context, tx *sql.Tx, worldID string, snapshot socialRestoreSnapshot) (FamilyLedgerEvidence, error) {
	evidence, err := readFamilyImportHeader(ctx, tx, worldID)
	if err != nil {
		return FamilyLedgerEvidence{}, err
	}
	family := world.FamilyState{Members: make(map[int16][]world.FamilyMember)}
	catalog := world.FamilyCatalog{Families: make(map[int16]world.FamilyDefinition)}
	allBossIDs := make(map[int16]string)
	nonEmptyBossIDs := make(map[int16]string)
	rows, err := tx.QueryContext(ctx, `SELECT family_id,family_name,boss_id,boss_name,fee,command_id FROM mud_go.family_catalog WHERE world_id=$1 ORDER BY family_id`, worldID)
	if err != nil {
		return FamilyLedgerEvidence{}, err
	}
	for rows.Next() {
		var id int16
		var name, bossID, bossName, commandID string
		var fee int64
		if err := rows.Scan(&id, &name, &bossID, &bossName, &fee, &commandID); err != nil {
			rows.Close()
			return FamilyLedgerEvidence{}, err
		}
		if _, exists := catalog.Families[id]; exists || commandID != evidence.CommandID {
			rows.Close()
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: invalid family catalog row", ErrSocialRestoreConflict)
		}
		if id < 1 || id > world.FamilyMaxID || !validImportText(name, 256) || !canonicalPlayerName(bossName) || (bossID != "" && !validImportToken(bossID, 128)) || fee < 0 || fee > 214748 {
			rows.Close()
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: invalid family catalog evidence", ErrSocialRestoreConflict)
		}
		catalog.Families[id] = world.FamilyDefinition{ID: id, Name: name, Boss: bossName, Fee: fee}
		family.Members[id] = []world.FamilyMember{}
		allBossIDs[id] = bossID
		if bossID != "" {
			nonEmptyBossIDs[id] = bossID
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return FamilyLedgerEvidence{}, err
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, `SELECT family_id,member_position,character_id,character_name,character_class,command_id FROM mud_go.family_members WHERE world_id=$1 ORDER BY family_id,member_position`, worldID)
	if err != nil {
		return FamilyLedgerEvidence{}, err
	}
	for rows.Next() {
		var familyID int16
		var position, characterClass int
		var member world.FamilyMember
		var commandID string
		if err := rows.Scan(&familyID, &position, &member.ID, &member.Name, &characterClass, &commandID); err != nil {
			rows.Close()
			return FamilyLedgerEvidence{}, err
		}
		members, exists := family.Members[familyID]
		if !exists || position != len(members) || characterClass < 0 || characterClass > 255 || commandID != evidence.CommandID || !validImportToken(member.ID, 128) || !canonicalPlayerName(member.Name) {
			rows.Close()
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: invalid family member evidence", ErrSocialRestoreConflict)
		}
		member.Class = byte(characterClass)
		family.Members[familyID] = append(members, member)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return FamilyLedgerEvidence{}, err
	}
	rows.Close()

	if len(family.Members) != evidence.FamilyCount || familyMemberCount(family) != evidence.MemberCount {
		return FamilyLedgerEvidence{}, fmt.Errorf("%w: family evidence counts mismatch", ErrSocialRestoreConflict)
	}
	if err := family.Validate(); err != nil {
		return FamilyLedgerEvidence{}, fmt.Errorf("%w: family evidence validation: %v", ErrSocialRestoreConflict, err)
	}
	if err := catalog.Validate(); err != nil {
		return FamilyLedgerEvidence{}, fmt.Errorf("%w: family catalog validation: %v", ErrSocialRestoreConflict, err)
	}

	// Older imports accepted both a direct Family/Catalog projection and the
	// row-oriented Families spelling. The latter retains empty boss_id map
	// entries in its digest, while the former has an empty map. Try only those
	// deterministic representations; never infer an identity from a name.
	bossCandidates := []map[int16]string{
		{},
		nonEmptyBossIDs,
		allBossIDs,
	}
	matchedBossIDs := map[int16]string(nil)
	for _, candidate := range bossCandidates {
		normalized, normalizeErr := normalizeFamilyLedgerImport(FamilyLedgerImport{
			WorldID: worldID, CommandID: evidence.CommandID, ExpectedRevision: evidence.ImportedRevision,
			FamilyState: family, Catalog: catalog,
		})
		if normalizeErr != nil {
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: family evidence normalization: %v", ErrSocialRestoreConflict, normalizeErr)
		}
		// normalizeFamilyLedgerImport has no public boss-ID input on the
		// direct projection. Rebuild the digest with the candidate map for the
		// exact source spelling that was originally imported.
		payload, marshalErr := json.Marshal(struct {
			Family  world.FamilyState   `json:"family"`
			Catalog world.FamilyCatalog `json:"catalog"`
			BossIDs map[int16]string    `json:"boss_ids"`
		}{family, catalog, candidate})
		if marshalErr != nil {
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: family evidence encoding", ErrSocialRestoreConflict)
		}
		normalized.aggregate = sha256.Sum256(payload)
		if bytes.Equal(normalized.aggregate[:], evidence.AggregateSHA256[:]) {
			matchedBossIDs = candidate
			break
		}
	}
	if matchedBossIDs == nil {
		return FamilyLedgerEvidence{}, fmt.Errorf("%w: family aggregate mismatch", ErrSocialRestoreConflict)
	}
	request := familyLedgerRequest{Kind: "import-family-ledger-v1", Aggregate: evidence.AggregateSHA256}
	requestRaw, _ := json.Marshal(request)
	requestDigest := sha256.Sum256(requestRaw)
	receipt := familyLedgerReceipt{}
	if err := readSocialCommandReceipt(ctx, tx, worldID, evidence.CommandID, requestDigest, evidence.ImportedRevision, &receipt); err != nil {
		return FamilyLedgerEvidence{}, err
	}
	if receipt.Revision != evidence.ImportedRevision || receipt.FamilyCount != evidence.FamilyCount || receipt.MemberCount != evidence.MemberCount || receipt.AggregateSHA256 != evidence.AggregateSHA256 {
		return FamilyLedgerEvidence{}, fmt.Errorf("%w: family receipt metadata mismatch", ErrSocialRestoreConflict)
	}
	evidence.Family = family
	evidence.Catalog = catalog
	evidence.BossIDs = cloneFamilyBossIDs(matchedBossIDs)
	if snapshot.revision < evidence.ImportedRevision || snapshot.state.Family == nil {
		return FamilyLedgerEvidence{}, fmt.Errorf("%w: family snapshot is older or unresolved", ErrSocialRestoreConflict)
	}
	if snapshot.revision == evidence.ImportedRevision {
		if !equalJSON(snapshot.state.Family, evidence.Family) {
			return FamilyLedgerEvidence{}, fmt.Errorf("%w: family snapshot/evidence mismatch", ErrSocialRestoreConflict)
		}
		for familyID, bossID := range evidence.BossIDs {
			if bossID == "" {
				continue
			}
			player, ok := snapshot.state.Players[bossID]
			definition := evidence.Catalog.Families[familyID]
			if !ok || player.Body.Name != definition.Boss {
				return FamilyLedgerEvidence{}, fmt.Errorf("%w: family boss identity mismatch", ErrSocialRestoreConflict)
			}
			found := false
			for _, member := range evidence.Family.Members[familyID] {
				if member.ID == bossID {
					found = true
					break
				}
			}
			if !found {
				return FamilyLedgerEvidence{}, fmt.Errorf("%w: family boss member mismatch", ErrSocialRestoreConflict)
			}
		}
	}
	evidence.SnapshotRevision = snapshot.revision
	return evidence, nil
}

func cloneFamilyBossIDs(input map[int16]string) map[int16]string {
	if input == nil {
		return nil
	}
	output := make(map[int16]string, len(input))
	for id, value := range input {
		output[id] = value
	}
	return output
}

func readMemoImportHeader(ctx context.Context, tx *sql.Tx, worldID string) (CharacterMemosEvidence, error) {
	rows, err := tx.QueryContext(ctx, `SELECT command_id,aggregate_sha256,recipient_count,memo_count,imported_revision FROM mud_go.character_memo_imports WHERE world_id=$1 ORDER BY command_id`, worldID)
	if err != nil {
		return CharacterMemosEvidence{}, err
	}
	defer rows.Close()
	var evidence CharacterMemosEvidence
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return CharacterMemosEvidence{}, fmt.Errorf("%w: multiple memo import evidence rows", ErrSocialRestoreConflict)
		}
		var digest []byte
		if err := rows.Scan(&evidence.CommandID, &digest, &evidence.RecipientCount, &evidence.MemoCount, &evidence.ImportedRevision); err != nil {
			return CharacterMemosEvidence{}, err
		}
		if len(digest) != sha256.Size {
			return CharacterMemosEvidence{}, fmt.Errorf("%w: invalid memo aggregate digest", ErrSocialRestoreConflict)
		}
		copy(evidence.AggregateSHA256[:], digest)
	}
	if err := rows.Err(); err != nil {
		return CharacterMemosEvidence{}, err
	}
	if count == 0 {
		var memoRows int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.character_memos WHERE world_id=$1`, worldID).Scan(&memoRows); err != nil {
			return CharacterMemosEvidence{}, err
		}
		if memoRows != 0 {
			return CharacterMemosEvidence{}, fmt.Errorf("%w: orphan memo evidence rows", ErrSocialRestoreConflict)
		}
		return CharacterMemosEvidence{}, ErrCharacterMemosEvidenceUnavailable
	}
	if !validImportToken(evidence.CommandID, 128) || evidence.ImportedRevision <= 0 || evidence.RecipientCount < 0 || evidence.RecipientCount > world.MaxMemoRecipients || evidence.MemoCount < 0 || evidence.MemoCount > world.MaxMemoRecords {
		return CharacterMemosEvidence{}, fmt.Errorf("%w: invalid memo import header", ErrSocialRestoreConflict)
	}
	return evidence, nil
}

func cloneMemoEvidence(input map[string][]world.CharacterMemo) map[string][]world.CharacterMemo {
	if input == nil {
		return nil
	}
	output := make(map[string][]world.CharacterMemo, len(input))
	for id, records := range input {
		output[id] = append([]world.CharacterMemo(nil), records...)
	}
	return output
}

func readCharacterMemosEvidenceTx(ctx context.Context, tx *sql.Tx, worldID string, snapshot socialRestoreSnapshot) (CharacterMemosEvidence, error) {
	evidence, err := readMemoImportHeader(ctx, tx, worldID)
	if err != nil {
		return CharacterMemosEvidence{}, err
	}
	memos := make(map[string][]world.CharacterMemo)
	seenIDs := make(map[string]struct{})
	rows, err := tx.QueryContext(ctx, `SELECT recipient_id,memo_position,memo_id,sender_id,sender_name,body,created_at,command_id FROM mud_go.character_memos WHERE world_id=$1 ORDER BY recipient_id,memo_position`, worldID)
	if err != nil {
		return CharacterMemosEvidence{}, err
	}
	for rows.Next() {
		var recipientID, commandID string
		var position int
		var memo world.CharacterMemo
		if err := rows.Scan(&recipientID, &position, &memo.ID, &memo.SenderID, &memo.SenderName, &memo.Body, &memo.CreatedAt, &commandID); err != nil {
			rows.Close()
			return CharacterMemosEvidence{}, err
		}
		records := memos[recipientID]
		if position != len(records) || commandID != evidence.CommandID || !validImportToken(recipientID, 128) || !validImportToken(memo.ID, 128) || !validImportToken(memo.SenderID, 128) || !canonicalPlayerName(memo.SenderName) || memo.CreatedAt.IsZero() || memo.CreatedAt.Before(unixEpoch) {
			rows.Close()
			return CharacterMemosEvidence{}, fmt.Errorf("%w: invalid memo evidence row", ErrSocialRestoreConflict)
		}
		if _, exists := seenIDs[memo.ID]; exists {
			rows.Close()
			return CharacterMemosEvidence{}, fmt.Errorf("%w: duplicate memo identity", ErrSocialRestoreConflict)
		}
		seenIDs[memo.ID] = struct{}{}
		memos[recipientID] = append(records, memo)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return CharacterMemosEvidence{}, err
	}
	rows.Close()
	if len(memos) != evidence.RecipientCount || memoCount(memos) != evidence.MemoCount {
		return CharacterMemosEvidence{}, fmt.Errorf("%w: memo evidence counts mismatch", ErrSocialRestoreConflict)
	}

	// PostgreSQL timestamptz stores microseconds. Normal imports normally use
	// that precision; if an operator supplied finer precision, the authoritative
	// snapshot at the import revision is the only safe source from which to
	// re-check the original aggregate. We never silently round and accept an
	// arbitrary row.
	matchedMemos, matched := matchMemoAggregate(worldID, evidence, memos, snapshot)
	if !matched {
		return CharacterMemosEvidence{}, fmt.Errorf("%w: memo aggregate mismatch", ErrSocialRestoreConflict)
	}
	request := characterMemosRequest{Kind: "import-character-memos-v1", Aggregate: evidence.AggregateSHA256}
	requestRaw, _ := json.Marshal(request)
	requestDigest := sha256.Sum256(requestRaw)
	receipt := characterMemosReceipt{}
	if err := readSocialCommandReceipt(ctx, tx, worldID, evidence.CommandID, requestDigest, evidence.ImportedRevision, &receipt); err != nil {
		return CharacterMemosEvidence{}, err
	}
	if receipt.Revision != evidence.ImportedRevision || receipt.RecipientCount != evidence.RecipientCount || receipt.MemoCount != evidence.MemoCount || receipt.AggregateSHA256 != evidence.AggregateSHA256 {
		return CharacterMemosEvidence{}, fmt.Errorf("%w: memo receipt metadata mismatch", ErrSocialRestoreConflict)
	}
	if snapshot.revision < evidence.ImportedRevision || snapshot.state.Memos == nil {
		return CharacterMemosEvidence{}, fmt.Errorf("%w: memo snapshot is older or unresolved", ErrSocialRestoreConflict)
	}
	if snapshot.revision == evidence.ImportedRevision {
		if !equalJSON(snapshot.state.Memos, matchedMemos) && !memoMapsEqualAtStoragePrecision(snapshot.state.Memos, matchedMemos) {
			return CharacterMemosEvidence{}, fmt.Errorf("%w: memo snapshot/evidence mismatch", ErrSocialRestoreConflict)
		}
	} else if !memoEvidencePrefix(snapshot.state.Memos, matchedMemos) {
		return CharacterMemosEvidence{}, fmt.Errorf("%w: memo evidence is not a snapshot prefix", ErrSocialRestoreConflict)
	}
	evidence.Memos = matchedMemos
	evidence.SnapshotRevision = snapshot.revision
	return evidence, nil
}

func memoCount(memos map[string][]world.CharacterMemo) int {
	count := 0
	for _, records := range memos {
		count += len(records)
	}
	return count
}

func memoAggregateCandidates(worldID string, evidence CharacterMemosEvidence, memos map[string][]world.CharacterMemo, state world.State) [][sha256.Size]byte {
	variants := make([][sha256.Size]byte, 0, 2)
	withoutNames, err := normalizeMemosImport(CharacterMemosImport{WorldID: worldID, CommandID: evidence.CommandID, ExpectedRevision: evidence.ImportedRevision, Memos: memos})
	if err == nil {
		variants = append(variants, withoutNames.aggregate)
	}
	if state.Players != nil {
		names := make(map[string]string, len(memos))
		valid := true
		for recipientID := range memos {
			player, ok := state.Players[recipientID]
			if !ok {
				valid = false
				break
			}
			names[recipientID] = player.Body.Name
		}
		if valid {
			withNames, normalizeErr := normalizeMemosImport(CharacterMemosImport{WorldID: worldID, CommandID: evidence.CommandID, ExpectedRevision: evidence.ImportedRevision, Memos: memos, RecipientNames: names})
			if normalizeErr == nil {
				variants = append(variants, withNames.aggregate)
			}
		}
	}
	return variants
}

func matchMemoAggregate(worldID string, evidence CharacterMemosEvidence, memos map[string][]world.CharacterMemo, snapshot socialRestoreSnapshot) (map[string][]world.CharacterMemo, bool) {
	for _, digest := range memoAggregateCandidates(worldID, evidence, memos, snapshot.state) {
		if digest == evidence.AggregateSHA256 {
			return memos, true
		}
	}
	if snapshot.revision != evidence.ImportedRevision || snapshot.state.Memos == nil {
		return nil, false
	}
	if !memoMapsEqualAtStoragePrecision(snapshot.state.Memos, memos) {
		return nil, false
	}
	for _, digest := range memoAggregateCandidates(worldID, evidence, snapshot.state.Memos, snapshot.state) {
		if digest == evidence.AggregateSHA256 {
			return cloneMemoEvidence(snapshot.state.Memos), true
		}
	}
	return nil, false
}

func sameMemoAtStoragePrecision(a, b world.CharacterMemo) bool {
	if a.ID != b.ID || a.SenderID != b.SenderID || a.SenderName != b.SenderName || a.Body != b.Body {
		return false
	}
	return a.CreatedAt.UTC().Truncate(time.Microsecond).Equal(b.CreatedAt.UTC().Truncate(time.Microsecond))
}

func memoMapsEqualAtStoragePrecision(a, b map[string][]world.CharacterMemo) bool {
	if len(a) != len(b) {
		return false
	}
	for recipientID, records := range a {
		other, ok := b[recipientID]
		if !ok || len(records) != len(other) {
			return false
		}
		for index := range records {
			if !sameMemoAtStoragePrecision(records[index], other[index]) {
				return false
			}
		}
	}
	return true
}

func memoEvidencePrefix(snapshot, evidence map[string][]world.CharacterMemo) bool {
	for recipientID, records := range evidence {
		current, ok := snapshot[recipientID]
		if !ok || len(current) < len(records) {
			return false
		}
		for index := range records {
			if !sameMemoAtStoragePrecision(current[index], records[index]) {
				return false
			}
		}
	}
	return true
}

// ReadFamilyLedgerEvidence reads and validates the normalized family rows
// against the current world snapshot. It performs no writes and never uses a
// family/name lookup to select a character.
func (p *Postgres) ReadFamilyLedgerEvidence(ctx context.Context, worldID string) (FamilyLedgerEvidence, error) {
	if err := validateSocialRestoreInput(worldID, 0); err != nil {
		return FamilyLedgerEvidence{}, err
	}
	tx, err := p.beginSocialRead(ctx)
	if err != nil {
		return FamilyLedgerEvidence{}, err
	}
	defer tx.Rollback()
	snapshot, err := p.readSocialSnapshotTx(ctx, tx, worldID)
	if err != nil {
		return FamilyLedgerEvidence{}, err
	}
	evidence, err := readFamilyLedgerEvidenceTx(ctx, tx, worldID, snapshot)
	if err != nil {
		return FamilyLedgerEvidence{}, err
	}
	if err := tx.Commit(); err != nil {
		return FamilyLedgerEvidence{}, err
	}
	return evidence, nil
}

// ReadCharacterMemosEvidence reads and validates the normalized memo rows
// against the current world snapshot. It performs no writes.
func (p *Postgres) ReadCharacterMemosEvidence(ctx context.Context, worldID string) (CharacterMemosEvidence, error) {
	if err := validateSocialRestoreInput(worldID, 0); err != nil {
		return CharacterMemosEvidence{}, err
	}
	tx, err := p.beginSocialRead(ctx)
	if err != nil {
		return CharacterMemosEvidence{}, err
	}
	defer tx.Rollback()
	snapshot, err := p.readSocialSnapshotTx(ctx, tx, worldID)
	if err != nil {
		return CharacterMemosEvidence{}, err
	}
	evidence, err := readCharacterMemosEvidenceTx(ctx, tx, worldID, snapshot)
	if err != nil {
		return CharacterMemosEvidence{}, err
	}
	if err := tx.Commit(); err != nil {
		return CharacterMemosEvidence{}, err
	}
	return evidence, nil
}

// RestoreSocialState performs the explicit startup/recovery read. It locks
// the world row for a consistent snapshot, checks the caller's expected
// revision and writer generation, validates each requested evidence domain,
// then returns the decoded world snapshot without modifying PostgreSQL.
func (p *Postgres) RestoreSocialState(ctx context.Context, options SocialRestoreOptions) (SocialRestoreResult, error) {
	if err := validateSocialRestoreInput(options.WorldID, options.ExpectedRevision); err != nil {
		return SocialRestoreResult{}, err
	}
	tx, err := p.beginSocialRead(ctx)
	if err != nil {
		return SocialRestoreResult{}, err
	}
	defer tx.Rollback()
	snapshot, err := p.readSocialSnapshotTx(ctx, tx, options.WorldID)
	if err != nil {
		return SocialRestoreResult{}, err
	}
	if snapshot.revision != options.ExpectedRevision {
		return SocialRestoreResult{}, ErrWorldConflict
	}
	result := SocialRestoreResult{WorldID: options.WorldID, Revision: snapshot.revision, State: snapshot.state}
	family, familyErr := readFamilyLedgerEvidenceTx(ctx, tx, options.WorldID, snapshot)
	if familyErr == nil {
		result.Family = &family
	} else if !errors.Is(familyErr, ErrFamilyLedgerEvidenceUnavailable) || options.RequireFamily || snapshot.state.Family != nil {
		return SocialRestoreResult{}, familyErr
	}
	memos, memosErr := readCharacterMemosEvidenceTx(ctx, tx, options.WorldID, snapshot)
	if memosErr == nil {
		result.Memos = &memos
	} else if !errors.Is(memosErr, ErrCharacterMemosEvidenceUnavailable) || options.RequireMemos || snapshot.state.Memos != nil {
		return SocialRestoreResult{}, memosErr
	}
	if err := tx.Commit(); err != nil {
		return SocialRestoreResult{}, err
	}
	return result, nil
}

// ReadSocialState is a descriptive alias for callers that use read/recovery
// terminology. It has the same explicit expected-revision and writer fence as
// RestoreSocialState and never mutates the world.
func (p *Postgres) ReadSocialState(ctx context.Context, options SocialRestoreOptions) (SocialRestoreResult, error) {
	return p.RestoreSocialState(ctx, options)
}
