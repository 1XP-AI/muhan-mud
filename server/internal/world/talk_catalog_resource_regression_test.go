package world

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"
)

// This is a provenance fixture for the one checked-in talk asset that is not
// strict CP949. It must remain a catalog admission failure until an exact
// source byte sequence is recovered; a plausible one-byte repair is not
// sufficient evidence to rewrite the asset.
func TestApartmentSentryTalkAssetRemainsFailClosed(t *testing.T) {
	const (
		resourcePath  = "objmon/talk/아파트_수위_아저씨-127"
		expectedSize  = 552
		expectedSHA   = "e64e47a24cf48b54c3c7aab271994c2e46d30196ed45273f6fa492b2f7912674"
		badOffset     = 216
		badLine       = 6
		badLineOffset = 39
	)

	raw, err := os.ReadFile("../../../resources_utf8/" + resourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != expectedSize {
		t.Fatalf("%s size=%d want %d", resourcePath, len(raw), expectedSize)
	}
	digest := sha256.Sum256(raw)
	if got := fmt.Sprintf("%x", digest); got != expectedSHA {
		t.Fatalf("%s sha256=%s want %s", resourcePath, got, expectedSHA)
	}
	if utf8.Valid(raw) {
		t.Fatalf("%s unexpectedly became valid UTF-8", resourcePath)
	}

	lines := bytes.Split(raw, []byte{'\n'})
	if len(lines) < badLine {
		t.Fatalf("%s has %d lines, want at least %d", resourcePath, len(lines), badLine)
	}
	lineStart := 0
	for _, line := range lines[:badLine-1] {
		lineStart += len(line) + 1
	}
	line := lines[badLine-1]
	if lineStart+badLineOffset != badOffset {
		t.Fatalf("bad byte location=%d want %d", lineStart+badLineOffset, badOffset)
	}
	if len(line) <= badLineOffset {
		t.Fatalf("line %d length=%d, missing bad offset %d", badLine, len(line), badLineOffset)
	}
	if line[badLineOffset] != 0xba {
		t.Fatalf("bad byte at offset %d=%02x want ba", badOffset, line[badLineOffset])
	}
	if badLineOffset == 0 || line[badLineOffset-1] != 0xba || line[badLineOffset+1] != ' ' {
		t.Fatalf("unexpected CP949 context around offset %d: % x", badOffset, line[max(0, badLineOffset-2):min(len(line), badLineOffset+3)])
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(line)
	if err != nil {
		t.Fatalf("%s line %d decoder error=%v", resourcePath, badLine, err)
	}
	if !strings.Contains(string(decoded), "\ufffd") {
		t.Fatalf("%s line %d decoder output=%q; want replacement rune", resourcePath, badLine, decoded)
	}
	if _, err := decodeTalkText(line); err == nil {
		t.Fatalf("%s line %d unexpectedly passed talk text admission", resourcePath, badLine)
	}

	// Isolate this file so the assertion is about its CP949 corruption. Other
	// legacy talk files contain ANSI control sequences and are a separate
	// admission decision.
	_, err = LoadTalkCatalog(fstest.MapFS{
		"아파트_수위_아저씨-127": &fstest.MapFile{Data: raw},
	})
	if err == nil {
		t.Fatalf("catalog admitted known-corrupt %s", resourcePath)
	}
	message := err.Error()
	if !strings.Contains(message, "아파트_수위_아저씨-127") || !strings.Contains(message, "invalid UTF-8/EUC-KR text") {
		t.Fatalf("catalog error=%q; want source path and strict decode failure", message)
	}
}
