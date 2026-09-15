package world

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func legacyMemoParserFixture(now time.Time, sender, body string) []byte {
	// command12.c strips ctime's newline before writing the header, so the
	// timestamp and header occupy one physical line in the native file.
	return []byte(now.Format(LegacyMemoTimestampFormat) +
		" 에 [" + sender + "] 님이 남기신 메모 : \n" +
		">>>>> " + body + "\n")
}

func legacyMemoParserOptions(recipientID string, senders map[string]string) LegacyMemoParserOptionsV1 {
	return LegacyMemoParserOptionsV1{
		ABI: LegacyMemoFileV1ABI, RecipientName: "Bob", RecipientID: recipientID,
		SenderIDs: senders, TimestampLocation: time.UTC,
	}
}

func TestLegacyMemoParserV1MatchesCommand12Fixture(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 34, 56, 0, time.UTC)
	body := "안녕, Bob!"
	raw := legacyMemoParserFixture(now, "Alice", body)
	parsed, err := ParseLegacyMemoFileV1(raw, legacyMemoParserOptions("character-bob", map[string]string{"Alice": "character-alice"}))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.RecipientName != "Bob" || parsed.RecipientID != "character-bob" || parsed.LineCount != 2 || parsed.SourceOctets != len(raw) {
		t.Fatalf("metadata=%+v", parsed)
	}
	if parsed.SourceSHA256 != sha256.Sum256(raw) {
		t.Fatal("source digest does not cover exact fixture")
	}
	if len(parsed.Memos) != 1 || !reflect.DeepEqual(parsed.Memos, parsed.Records) {
		t.Fatalf("records=%+v memos=%+v", parsed.Records, parsed.Memos)
	}
	record := parsed.Memos[0]
	if record.SenderID != "character-alice" || record.SenderName != "Alice" || record.Body != body || !record.CreatedAt.Equal(now) {
		t.Fatalf("record=%+v", record)
	}
	if record.ID == "" || len(record.ID) > MaxMemoIDBytes || !strings.HasPrefix(record.ID, "legacy-memo-v1-") {
		t.Fatalf("generated record ID=%q", record.ID)
	}
	// The source projection is pointer-free: no path or credential can enter
	// the parser result even though the locator exposes path metadata separately.
	typ := reflect.TypeOf(parsed)
	for field := 0; field < typ.NumField(); field++ {
		name := typ.Field(field).Name
		if name == "Path" || name == "RawPath" || name == "Credential" || name == "Password" {
			t.Fatalf("sensitive field leaked into parser result: %s", name)
		}
	}
}

func TestLegacyMemoParserV1SupportsMultipleRecordsAndExplicitTimezone(t *testing.T) {
	location := time.FixedZone("source", 9*60*60)
	first := time.Date(2024, time.January, 2, 3, 4, 5, 0, location)
	second := time.Date(2025, time.December, 31, 23, 59, 59, 0, location)
	raw := append(legacyMemoParserFixture(first, "Alice", "one"), legacyMemoParserFixture(second, "Carol", "two")...)
	options := legacyMemoParserOptions("bob-id", map[string]string{"Alice": "alice-id", "Carol": "carol-id"})
	options.TimestampLocation = location
	parsed, err := ParseLegacyMemoFileV1(raw, options)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.LineCount != 4 || len(parsed.Memos) != 2 || parsed.Memos[0].SenderID != "alice-id" || parsed.Memos[1].SenderID != "carol-id" {
		t.Fatalf("parsed=%+v", parsed)
	}
	if !parsed.Memos[0].CreatedAt.Equal(first) || !parsed.Memos[1].CreatedAt.Equal(second) {
		t.Fatalf("timestamps=%v,%v", parsed.Memos[0].CreatedAt, parsed.Memos[1].CreatedAt)
	}
	if parsed.Memos[0].ID == parsed.Memos[1].ID {
		t.Fatal("record IDs are not unique")
	}
}

func TestLegacyMemoParserV1RequiresExplicitABIAndIdentityMapping(t *testing.T) {
	raw := legacyMemoParserFixture(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "Alice", "body")
	valid := legacyMemoParserOptions("bob-id", map[string]string{"Alice": "alice-id"})
	cases := []struct {
		name   string
		mutate func(*LegacyMemoParserOptionsV1)
		want   error
	}{
		{name: "missing ABI", mutate: func(options *LegacyMemoParserOptionsV1) { options.ABI = "" }, want: ErrLegacyMemoUnsupportedABI},
		{name: "wrong ABI", mutate: func(options *LegacyMemoParserOptionsV1) { options.ABI += ":other" }, want: ErrLegacyMemoUnsupportedABI},
		{name: "missing recipient ID", mutate: func(options *LegacyMemoParserOptionsV1) { options.RecipientID = "" }, want: ErrLegacyMemoIdentityMapping},
		{name: "nil sender map", mutate: func(options *LegacyMemoParserOptionsV1) { options.SenderIDs = nil }, want: ErrLegacyMemoIdentityMapping},
		{name: "unmapped sender", mutate: func(options *LegacyMemoParserOptionsV1) { options.SenderIDs = map[string]string{"Carol": "carol-id"} }, want: ErrLegacyMemoIdentityMapping},
		{name: "conflicting maps", mutate: func(options *LegacyMemoParserOptionsV1) {
			options.SenderIDByName = map[string]string{"Alice": "other-id"}
		}, want: ErrLegacyMemoIdentityConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			options := valid
			tc.mutate(&options)
			if _, err := ParseLegacyMemoFileV1(raw, options); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v, want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
}

func TestLegacyMemoParserV1RejectsMalformedAndTrailingSources(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	base := legacyMemoParserFixture(now, "Alice", "body")
	options := legacyMemoParserOptions("bob-id", map[string]string{"Alice": "alice-id"})
	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{name: "missing final LF", raw: bytes.TrimSuffix(base, []byte("\n")), want: ErrLegacyMemoTrailingBytes},
		{name: "one incomplete line", raw: append(append([]byte(nil), base...), []byte("partial\n")...), want: ErrLegacyMemoLineCount},
		{name: "extra trailing bytes", raw: append(append([]byte(nil), base...), []byte("trailing")...), want: ErrLegacyMemoTrailingBytes},
		{name: "CRLF", raw: bytes.ReplaceAll(base, []byte("\n"), []byte("\r\n")), want: ErrLegacyMemoMalformed},
		{name: "bad header", raw: bytes.Replace(base, []byte(" 에 [Alice] 님이 남기신 메모 : "), []byte("bad"), 1), want: ErrLegacyMemoMalformed},
		{name: "bad body prefix", raw: bytes.Replace(base, []byte(">>>>> body"), []byte("body"), 1), want: ErrLegacyMemoMalformed},
		{name: "invalid UTF-8", raw: append(append([]byte(nil), base[:len(base)-1]...), 0xff, '\n'), want: ErrLegacyMemoMalformed},
		{name: "zero timestamp", raw: legacyMemoParserFixture(time.Date(1969, time.December, 31, 23, 59, 59, 0, time.UTC), "Alice", "body"), want: ErrLegacyMemoMalformed},
		{name: "future timestamp", raw: legacyMemoParserFixture(time.Date(2101, time.January, 1, 0, 0, 0, 0, time.UTC), "Alice", "body"), want: ErrLegacyMemoMalformed},
		{name: "body too long", raw: legacyMemoParserFixture(now, "Alice", strings.Repeat("a", MaxMemoBodyBytes+1)), want: ErrLegacyMemoMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseLegacyMemoFileV1(tc.raw, options); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v, want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
}

func TestLegacyMemoParserV1RejectsLineAndNameBounds(t *testing.T) {
	options := legacyMemoParserOptions("bob-id", map[string]string{})
	tooManyLines := bytes.Repeat([]byte("\n"), LegacyMemoFileV1MaxLines+1)
	if _, err := ParseLegacyMemoFileV1(tooManyLines, options); !errors.Is(err, ErrLegacyMemoRecordLimit) {
		t.Fatalf("line bound err=%v", err)
	}
	base := legacyMemoParserFixture(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "Alice", "body")
	for name, mutate := range map[string]func(*LegacyMemoParserOptionsV1){
		"recipient noncanonical": func(value *LegacyMemoParserOptionsV1) { value.RecipientName = "alice" },
		"recipient traversal":    func(value *LegacyMemoParserOptionsV1) { value.RecipientName = "../Bob" },
		"sender noncanonical":    func(value *LegacyMemoParserOptionsV1) { value.SenderIDs = map[string]string{"alice": "alice-id"} },
		"sender ID path":         func(value *LegacyMemoParserOptionsV1) { value.SenderIDs = map[string]string{"Alice": "../alice"} },
	} {
		t.Run(name, func(t *testing.T) {
			value := options
			mutate(&value)
			if _, err := ParseLegacyMemoFileV1(base, value); !errors.Is(err, ErrLegacyMemoIdentityMapping) {
				t.Fatalf("err=%v, want identity mapping rejection", err)
			}
		})
	}
}

func TestLegacyMemoParserV1EmptySourceIsExplicitEmptyAggregate(t *testing.T) {
	options := legacyMemoParserOptions("bob-id", map[string]string{})
	parsed, err := ParseLegacyMemoFileV1(nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Memos == nil || parsed.Records == nil || len(parsed.Memos) != 0 || parsed.SourceOctets != 0 || parsed.LineCount != 0 {
		t.Fatalf("empty aggregate=%+v", parsed)
	}
	if parsed.SourceSHA256 != sha256.Sum256(nil) {
		t.Fatal("empty source digest mismatch")
	}
	clone := parsed.Clone()
	if clone.Memos == nil || clone.Records == nil {
		t.Fatal("clone lost explicit empty slices")
	}
	clone.Memos = append(clone.Memos, CharacterMemo{ID: "clone-only"})
	if len(parsed.Memos) != 0 {
		t.Fatal("clone mutation changed original aggregate")
	}
}
