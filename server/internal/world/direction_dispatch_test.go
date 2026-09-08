package world

import "testing"

// ParseDirectionalToken is the command-dispatch boundary for movement.  The
// terminal supplies a complete line, while the canonical movement API accepts
// only the first command token in MovementInput.Prefix.  Dispatch must return
// the canonical Korean exit name and report false for non-movement commands.
func TestParseDirectionalTokenNormalizesKoreanAndNumericAliases(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "south numeric", line: "2", want: "남"},
		{name: "south jamo", line: "ㄴ", want: "남"},
		{name: "south name", line: "남", want: "남"},
		{name: "west numeric", line: "4", want: "서"},
		{name: "west jamo", line: "ㅅ", want: "서"},
		{name: "west name", line: "서", want: "서"},
		{name: "east numeric", line: "6", want: "동"},
		{name: "east jamo", line: "ㄷ", want: "동"},
		{name: "east name", line: "동", want: "동"},
		{name: "north numeric", line: "8", want: "북"},
		{name: "north jamo", line: "ㅂ", want: "북"},
		{name: "north name", line: "북", want: "북"},
		{name: "down numeric", line: "3", want: "밑"},
		{name: "down jamo", line: "ㅁ", want: "밑"},
		{name: "down name", line: "밑", want: "밑"},
		{name: "up numeric", line: "9", want: "위"},
		{name: "up jamo", line: "ㅇ", want: "위"},
		{name: "up name", line: "위", want: "위"},
		{name: "leave alias", line: "나가", want: "밖"},
		// C's command parser dispatches move from the first token and the
		// move handler ignores later command arguments.
		{name: "leading and trailing spaces", line: "  8   북문", want: "북"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseDirectionalToken(tt.line)
			if !ok || got != tt.want {
				t.Fatalf("ParseDirectionalToken(%q) = %q, %v; want %q, true", tt.line, got, ok, tt.want)
			}
		})
	}
}

func TestParseDirectionalTokenRejectsNonMovementCommands(t *testing.T) {
	for _, line := range []string{"", " ", "봐", "보다 검", "조사 검", "소지품", "검 조사"} {
		if got, ok := ParseDirectionalToken(line); ok {
			t.Fatalf("ParseDirectionalToken(%q) = %q, true; want rejected", line, got)
		}
	}
}

// The parser result is a movement token, not a destination index or a client
// supplied room.  The existing selector remains responsible for resolving it
// against the authoritative room exits.
func TestParsedDirectionFeedsCanonicalExitSelection(t *testing.T) {
	exits := []LegacyExit{
		{Name: "남"},
		{Name: "서"},
		{Name: "동"},
		{Name: "북"},
		{Name: "밑"},
		{Name: "위"},
		{Name: "밖"},
	}
	for _, tt := range []struct {
		line string
		want int
	}{
		{line: "2", want: 0},
		{line: "ㅅ", want: 1},
		{line: "동", want: 2},
		{line: "8", want: 3},
		{line: "ㅁ", want: 4},
		{line: "위", want: 5},
		{line: "나가", want: 6},
	} {
		token, ok := ParseDirectionalToken(tt.line)
		if !ok {
			t.Fatalf("ParseDirectionalToken(%q) rejected movement input", tt.line)
		}
		if got := SelectDirectionalExit(exits, token); got != tt.want {
			t.Fatalf("line %q normalized to %q and selected %d; want %d", tt.line, token, got, tt.want)
		}
	}
}

// This is the canonical state transition boundary that the eventual command
// dispatcher must call after parsing.  It intentionally uses a numeric alias
// and verifies that the alias reaches the existing directional transfer API.
func TestParsedDirectionReachesCanonicalDirectionalTransfer(t *testing.T) {
	s, in := canonicalTransferFixture()
	in.Movement.Prefix, _ = ParseDirectionalToken("8")

	next, proposal, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Movement.Moved || proposal.Entry == nil || next.Players[in.ActorID].Body.RoomID != 2 {
		t.Fatalf("parsed direction did not reach canonical transfer: moved=%v entry=%v room=%d", proposal.Movement.Moved, proposal.Entry != nil, next.Players[in.ActorID].Body.RoomID)
	}
}
