package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"testing/fstest"
)

func legacyVoteImportInput(catalog VoteCatalog) LegacyVoteImportInput {
	return LegacyVoteImportInput{
		Files: map[string][]byte{
			"player/vote/Bob_v":   []byte("CBA"),
			"player/vote/Alice_v": []byte("ABC"),
		},
		Catalog: catalog,
		NameToID: map[string]string{
			"Alice": "actor",
			"Bob":   "other",
		},
	}
}

func legacyVoteImportState(t *testing.T) State {
	t.Helper()
	s := voteTestState(4, 3*86400, true)
	other := s.Players["actor"]
	other.Body.Name = "Bob"
	other.Body.RoomID = 1
	s.Players["other"] = other
	s.Rooms[1] = RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"actor", "other"},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestImportLegacyVotesUsesExactPathsExplicitIDsAndCatalogDigest(t *testing.T) {
	catalog := voteTestCatalog()
	digest, err := catalog.Digest()
	if err != nil {
		t.Fatal(err)
	}
	input := legacyVoteImportInput(catalog)
	input.ExpectedCatalogDigest = digest
	s := legacyVoteImportState(t)
	original := s.clone()

	next, err := s.ImportLegacyVotes(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("import mutated source state")
	}
	if next.Votes == nil || next.Votes.Ballots == nil || next.Votes.History == nil {
		t.Fatalf("missing canonical markers: %#v", next.Votes)
	}
	if len(next.Votes.History) != 0 {
		t.Fatalf("legacy files fabricated history: %#v", next.Votes.History)
	}
	if got := next.Votes.Ballots["actor"]; got.CatalogDigest != digest || !bytes.Equal(got.Choices, []byte("ABC")) {
		t.Fatalf("actor ballot=%+v", got)
	}
	if got := next.Votes.Ballots["other"]; got.CatalogDigest != digest || !bytes.Equal(got.Choices, []byte("CBA")) {
		t.Fatalf("other ballot=%+v", got)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input.Files["player/vote/Alice_v"], []byte("ABC")) {
		t.Fatal("import changed source bytes")
	}
}

func TestImportLegacyVotesAcceptsExplicitEmptySourceAndPreservesMarkers(t *testing.T) {
	s := voteTestState(4, 3*86400, true)
	next, err := s.ImportLegacyVotes(LegacyVoteImportInput{
		Files:   map[string][]byte{},
		Catalog: voteTestCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Votes == nil || next.Votes.Ballots == nil || next.Votes.History == nil {
		t.Fatalf("empty import lost nonnil markers: %#v", next.Votes)
	}
	if len(next.Votes.Ballots) != 0 || len(next.Votes.History) != 0 {
		t.Fatalf("empty import not empty: %#v", next.Votes)
	}
	if _, err := s.ImportLegacyVotes(LegacyVoteImportInput{Catalog: voteTestCatalog()}); !errors.Is(err, ErrVoteImportSourceUnavailable) {
		t.Fatalf("nil source err=%v", err)
	}
}

func TestImportLegacyVotesRejectsMalformedInputWithoutPartialState(t *testing.T) {
	catalog := voteTestCatalog()
	base := legacyVoteImportState(t)
	cases := []struct {
		name  string
		input LegacyVoteImportInput
		want  error
	}{
		{
			name: "noncanonical path",
			input: LegacyVoteImportInput{
				Files:    map[string][]byte{"player/vote/../Alice_v": []byte("ABC")},
				Catalog:  catalog,
				NameToID: map[string]string{"Alice": "actor"},
			},
			want: ErrVoteImportPath,
		},
		{
			name: "truncated",
			input: LegacyVoteImportInput{
				Files:    map[string][]byte{"player/vote/Alice_v": []byte("AB")},
				Catalog:  catalog,
				NameToID: map[string]string{"Alice": "actor"},
			},
			want: ErrVoteImportTruncated,
		},
		{
			name: "too many choices",
			input: LegacyVoteImportInput{
				Files:    map[string][]byte{"player/vote/Alice_v": []byte("ABCD")},
				Catalog:  catalog,
				NameToID: map[string]string{"Alice": "actor"},
			},
			want: ErrVoteImportChoiceCount,
		},
		{
			name: "lowercase source byte",
			input: LegacyVoteImportInput{
				Files:    map[string][]byte{"player/vote/Alice_v": []byte("aBC")},
				Catalog:  catalog,
				NameToID: map[string]string{"Alice": "actor"},
			},
			want: ErrVoteImportChoice,
		},
		{
			name: "unknown actor",
			input: LegacyVoteImportInput{
				Files:    map[string][]byte{"player/vote/Ghost_v": []byte("ABC")},
				Catalog:  catalog,
				NameToID: map[string]string{"Ghost": "missing"},
			},
			want: ErrVoteImportUnknownActor,
		},
		{
			name: "duplicate canonical actor",
			input: LegacyVoteImportInput{
				Files: map[string][]byte{
					"player/vote/Alice_v": []byte("ABC"),
					"player/vote/Bob_v":   []byte("CBA"),
				},
				Catalog: catalog,
				NameToID: map[string]string{
					"Alice": "actor",
					"Bob":   "actor",
				},
			},
			want: ErrVoteImportDuplicate,
		},
		{
			name: "issue digest mismatch",
			input: LegacyVoteImportInput{
				Files:                 map[string][]byte{"player/vote/Alice_v": []byte("ABC")},
				Catalog:               catalog,
				ExpectedCatalogDigest: hex.EncodeToString(make([]byte, sha256.Size)),
				NameToID:              map[string]string{"Alice": "actor"},
			},
			want: ErrVoteImportIssueDigest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := base.clone()
			if _, err := before.ImportLegacyVotes(tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if !reflect.DeepEqual(before, base) {
				t.Fatal("failed import changed state")
			}
		})
	}
}

func TestImportLegacyVotesRejectsDuplicateRecordsAndAlreadyImportedState(t *testing.T) {
	catalog := voteTestCatalog()
	duplicate := LegacyVoteImportInput{
		Records: []LegacyVoteFile{
			{Path: "player/vote/Alice_v", Bytes: []byte("ABC")},
			{Path: "player/vote/Alice_v", Bytes: []byte("ABC")},
		},
		Catalog:  catalog,
		NameToID: map[string]string{"Alice": "actor"},
	}
	s := voteTestState(4, 3*86400, true)
	if _, err := s.ImportLegacyVotes(duplicate); !errors.Is(err, ErrVoteImportDuplicate) {
		t.Fatalf("duplicate record err=%v", err)
	}
	digest, err := catalog.Digest()
	if err != nil {
		t.Fatal(err)
	}
	s.Votes = &VoteState{Ballots: map[string]VoteBallot{}, History: []VoteHistoryEntry{}}
	if _, err := s.ImportLegacyVotes(LegacyVoteImportInput{
		Files:                 map[string][]byte{},
		Catalog:               catalog,
		ExpectedCatalogDigest: digest,
	}); !errors.Is(err, ErrVoteImportAlreadyResolved) {
		t.Fatalf("already imported err=%v", err)
	}
}

func TestImportLegacyVotesNeverInfersNameMappingAndPlansEvidenceInPathOrder(t *testing.T) {
	s := legacyVoteImportState(t)
	catalog := voteTestCatalog()
	var calls []string
	input := LegacyVoteImportInput{
		Files: map[string][]byte{
			"player/vote/Bob_v":   []byte("CBA"),
			"player/vote/Alice_v": []byte("ABC"),
		},
		Catalog: catalog,
		Resolver: func(name string) (string, error) {
			calls = append(calls, name)
			if name == "Alice" {
				return "actor", nil
			}
			return "other", nil
		},
	}
	plan, err := s.PlanLegacyVoteImport(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"Alice", "Bob"}) {
		t.Fatalf("resolver order=%v", calls)
	}
	if len(plan.Evidence) != 2 || plan.Evidence[0].Path != "player/vote/Alice_v" || plan.Evidence[1].Path != "player/vote/Bob_v" {
		t.Fatalf("evidence order=%+v", plan.Evidence)
	}
	if plan.Evidence[0].SHA256 != sha256.Sum256([]byte("ABC")) || !bytes.Equal(plan.Evidence[0].Bytes, []byte("ABC")) {
		t.Fatalf("evidence=%+v", plan.Evidence[0])
	}

	// A matching display name in State.Players is not a substitute for the
	// explicit resolver. The empty resolver case is still an unknown actor.
	withoutResolver := input
	withoutResolver.Resolver = nil
	if _, err := s.ImportLegacyVotes(withoutResolver); !errors.Is(err, ErrVoteImportUnknownActor) {
		t.Fatalf("implicit name lookup err=%v", err)
	}
}

func TestReadLegacyVoteFilesPreservesCanonicalPathsBytesAndOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"player/vote/Zed_v":   &fstest.MapFile{Data: []byte("CBA")},
		"player/vote/Alice_v": &fstest.MapFile{Data: []byte("ABC")},
	}
	files, err := ReadLegacyVoteFiles(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "player/vote/Alice_v" || files[1].Path != "player/vote/Zed_v" {
		t.Fatalf("files=%+v", files)
	}
	if !bytes.Equal(files[0].Bytes, []byte("ABC")) {
		t.Fatalf("bytes=%q", files[0].Bytes)
	}
	files[0].Bytes[0] = 'Z'
	filesAgain, err := ReadLegacyVoteFiles(fsys)
	if err != nil || !bytes.Equal(filesAgain[0].Bytes, []byte("ABC")) {
		t.Fatalf("filesystem bytes were aliased: files=%+v err=%v", filesAgain, err)
	}

	playerRoot := fstest.MapFS{
		"vote/Zed_v":   &fstest.MapFile{Data: []byte("CBA")},
		"vote/Alice_v": &fstest.MapFile{Data: []byte("ABC")},
	}
	rooted, err := ReadLegacyVoteFilesFromPlayerRoot(playerRoot)
	if err != nil || len(rooted) != 2 || rooted[0].Path != "player/vote/Alice_v" || rooted[1].Path != "player/vote/Zed_v" {
		t.Fatalf("player-root files=%+v err=%v", rooted, err)
	}
}
