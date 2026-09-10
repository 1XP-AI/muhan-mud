package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func voteImportCatalog(t *testing.T) string {
	t.Helper()
	catalog := world.VoteCatalog{Issue: world.VoteIssue{Number: 1, Prompt: "선택", Options: []string{"후보"}}}
	digest, err := catalog.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func voteImportState() world.State {
	flags := [8]byte{}
	flags[world.VoteElectionRoomFlag/8] |= 1 << (world.VoteElectionRoomFlag % 8)
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: flags}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1},
			Online: true,
		}},
	}
}

func TestNormalizeVoteStateImportRejectsSensitiveDuplicateAndDigestMismatch(t *testing.T) {
	digest := voteImportCatalog(t)
	valid := VoteStateImport{WorldID: "w", CommandID: "c", CatalogDigest: digest, Votes: world.VoteState{Ballots: map[string]world.VoteBallot{}, History: []world.VoteHistoryEntry{}}}
	if _, err := normalizeVoteStateImport(valid); err != nil {
		t.Fatal(err)
	}
	bad := valid
	bad.SourcePath = "/player/vote/Alice_v"
	if _, err := normalizeVoteStateImport(bad); !errors.Is(err, ErrVoteStateImport) {
		t.Fatalf("source path err=%v", err)
	}
	bad = valid
	bad.BallotState = valid.Votes
	if _, err := normalizeVoteStateImport(bad); !errors.Is(err, ErrVoteStateImport) {
		t.Fatalf("duplicate err=%v", err)
	}
	bad = valid
	bad.Votes = world.VoteState{Ballots: map[string]world.VoteBallot{"actor": {CatalogDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Choices: []byte("A")}}, History: []world.VoteHistoryEntry{}}
	if _, err := normalizeVoteStateImport(bad); !errors.Is(err, ErrVoteStateImport) {
		t.Fatalf("digest err=%v", err)
	}
}

func TestPostgresImportsVoteStateAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_VOTE_IMPORT_TEST_DATABASE_URL")
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
	repo := NewPostgres(db)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("vote-import-%d", time.Now().UnixNano())
	state := voteImportState()
	raw, _ := json.Marshal(state)
	if err := repo.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	digest := voteImportCatalog(t)
	input := VoteStateImport{WorldID: worldID, CommandID: "vote-import-1", ExpectedRevision: 0, CatalogDigest: digest, Votes: world.VoteState{
		Ballots: map[string]world.VoteBallot{"actor": {CatalogDigest: digest, Choices: []byte("A")}},
		History: []world.VoteHistoryEntry{},
	}}
	first, err := repo.ImportVoteState(ctx, input)
	if err != nil || first.Replayed || first.Revision != 1 || first.BallotCount != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := repo.ImportVoteState(ctx, input)
	if err != nil || !replay.Replayed || replay.Revision != 1 || replay.AggregateSHA256 != first.AggregateSHA256 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	changed := input
	changed.CommandID = "vote-import-2"
	changed.ExpectedRevision = 1
	changed.Votes.Ballots["actor"] = world.VoteBallot{CatalogDigest: digest, Choices: []byte("B")}
	if _, err := repo.ImportVoteState(ctx, changed); !errors.Is(err, ErrVoteStateImportConflict) {
		t.Fatalf("second aggregate err=%v", err)
	}
	loaded, err := repo.LoadWorld(ctx, worldID)
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	decoded, err := world.DecodeState(loaded.State)
	if err != nil || decoded.Votes == nil || string(decoded.Votes.Ballots["actor"].Choices) != "A" {
		t.Fatalf("votes=%+v err=%v", decoded.Votes, err)
	}
}
