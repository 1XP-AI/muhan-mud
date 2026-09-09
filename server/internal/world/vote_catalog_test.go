package world

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"testing"

	"testing/fstest"
)

func TestParseVoteCatalogMatchesCommand11LineOrder(t *testing.T) {
	raw := []byte("2 leader\n다음 대표를 선택하세요.\n첫 번째 후보\n두 번째 후보\n")
	catalog, err := ParseVoteCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := VoteCatalog{Issue: VoteIssue{
		Number:  2,
		Prompt:  "다음 대표를 선택하세요.",
		Options: []string{"첫 번째 후보", "두 번째 후보"},
	}}
	if !reflect.DeepEqual(catalog, want) {
		t.Fatalf("catalog=%+v want=%+v", catalog, want)
	}
	if _, err := ParseVoteCatalog([]byte("2\r\n질문\r\nA\r\nB\r\n")); err != nil {
		t.Fatalf("CRLF catalog rejected: %v", err)
	}
	withoutFinalNewline, err := ParseVoteCatalog([]byte("1\n질문\nA"))
	if err != nil || withoutFinalNewline.Issue.Options[0] != "A" {
		t.Fatalf("final line without newline: %+v err=%v", withoutFinalNewline, err)
	}
}

func TestParseVoteCatalogSupportsLegacyCP949Text(t *testing.T) {
	// "질문" and "후보" in EUC-KR/CP949 bytes. The parser must decode the
	// source encoding before VoteCatalog.Validate applies UTF-8 boundaries.
	raw := append([]byte("1\n"), 193, 250, 185, 174, '\n', 200, 196, 186, 184)
	catalog, err := ParseVoteCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Issue.Prompt != "질문" || catalog.Issue.Options[0] != "후보" {
		t.Fatalf("decoded catalog=%+v", catalog)
	}
}

func TestParseVoteCatalogRejectsTruncationTrailingAndMalformedData(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{name: "empty", raw: nil, want: ErrVoteCatalogUnavailable},
		{name: "header only", raw: []byte("2\n"), want: ErrVoteIssueTruncated},
		{name: "missing option", raw: []byte("2\nquestion\nA\n"), want: ErrVoteIssueTruncated},
		{name: "trailing record", raw: []byte("1\nquestion\nA\nextra\n"), want: ErrVoteIssueTrailing},
		{name: "zero options", raw: []byte("0\nquestion\n"), want: ErrVoteIssueMalformed},
		{name: "too many header fields", raw: []byte("1 class extra\nquestion\nA\n"), want: ErrVoteIssueMalformed},
		{name: "empty prompt", raw: []byte("1\n\nA\n"), want: ErrVoteIssueMalformed},
		{name: "invalid UTF8", raw: []byte("1\n\xff\nA\n"), want: ErrVoteIssueMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseVoteCatalog(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
	tooLong := append([]byte("1\n"), bytes.Repeat([]byte{'x'}, VoteIssueTextBytes+1)...)
	tooLong = append(tooLong, '\n', 'A')
	if _, err := ParseVoteCatalog(tooLong); !errors.Is(err, ErrVoteIssueMalformed) {
		t.Fatalf("oversized prompt err=%v", err)
	}
}

func TestReadLegacyVoteCatalogReturnsPathAndByteEvidence(t *testing.T) {
	raw := []byte("1\n질문\n후보")
	fsys := fstest.MapFS{
		"post/ISSUE": &fstest.MapFile{Data: raw},
	}
	source, err := ReadLegacyVoteCatalog(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if source.Path != VoteIssuePath || !bytes.Equal(source.Bytes, raw) || source.SHA256 != sha256.Sum256(raw) {
		t.Fatalf("source=%+v", source)
	}
	copySource := source.Clone()
	copySource.Bytes[0] = 'X'
	copySource.Catalog.Issue.Options[0] = "changed"
	if source.Bytes[0] == 'X' || source.Catalog.Issue.Options[0] == "changed" {
		t.Fatal("source clone aliases original")
	}
	postRoot, err := ReadLegacyVoteCatalogFromPostRoot(fstest.MapFS{
		"ISSUE": &fstest.MapFile{Data: raw},
	})
	if err != nil || postRoot.Path != VoteIssuePath {
		t.Fatalf("post-root source=%+v err=%v", postRoot, err)
	}
	if _, err := ReadLegacyVoteCatalog(fstest.MapFS{}); !errors.Is(err, ErrVoteIssueSourceUnavailable) {
		t.Fatalf("missing source err=%v", err)
	}
	if _, err := ReadLegacyVoteCatalog(fstest.MapFS{
		"post/ISSUE": &fstest.MapFile{Mode: fs.ModeDir},
	}); !errors.Is(err, ErrVoteIssueSourceInvalid) {
		t.Fatalf("directory source err=%v", err)
	}
}

func TestLoadVoteCatalogFileRequiresRegularExplicitFile(t *testing.T) {
	dir := t.TempDir()
	filename := dir + "/ISSUE"
	raw := []byte("1\nquestion\nA")
	if err := os.WriteFile(filename, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := LoadVoteCatalogFile(filename)
	if err != nil || !bytes.Equal(source.Bytes, raw) {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	if _, err := LoadVoteCatalogFile(dir); !errors.Is(err, ErrVoteIssueSourceInvalid) {
		t.Fatalf("directory err=%v", err)
	}
}
