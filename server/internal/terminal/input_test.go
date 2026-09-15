package terminal

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestInputChunks(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{"commands", []string{"북\r남\n"}, []string{"북", "남"}},
		{"split CRLF", []string{"보기\r", "\n북\r\n"}, []string{"보기", "북"}},
		{"split UTF8", []string{string([]byte{0xeb}), string([]byte{0xb6, 0x81}), "\r"}, []string{"북"}},
		{"erase rune", []string{"남쪽\x7f북\r"}, []string{"남북"}},
		{"backspace", []string{"가\b\b나\r"}, []string{"나"}},
		{"blank", []string{"\r\r"}, []string{"", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := NewInput(64)
			var got []string
			for _, chunk := range tt.chunks {
				lines, err := in.Feed([]byte(chunk))
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, lines...)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestRejectedLineCannotBecomeCommandSuffix(t *testing.T) {
	for _, chunk := range []string{"12345", "a\x00b", "a\xffb"} {
		in := NewInput(4)
		if _, err := in.Feed([]byte(chunk)); err == nil {
			t.Fatal("expected rejection")
		}
		got, err := in.Feed([]byte("북\r남\r"))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{"남"}) {
			t.Fatalf("rejected suffix executed: %q", got)
		}
	}
}

func FuzzChunkBoundaries(f *testing.F) {
	for _, seed := range []string{"한글\x7f북\r\n", "abc\rdef\n", "\xff\r북\r", "\x1b[A\r"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		whole := NewInput(64)
		want, wholeErr := whole.Feed(data)
		pieces := NewInput(64)
		var got []string
		rejected := false
		for _, b := range data {
			lines, err := pieces.Feed([]byte{b})
			rejected = rejected || err != nil
			got = append(got, lines...)
		}
		if !reflect.DeepEqual(got, want) || rejected != (wholeErr != nil) {
			t.Fatal("chunk boundary changes framing")
		}
		for _, line := range got {
			if len(line) > 64 || !utf8.ValidString(line) {
				t.Fatal("invalid output line")
			}
		}
	})
}

func TestIncompleteRuneAtSubmitRejected(t *testing.T) {
	in := NewInput(64)
	got, err := in.Feed([]byte{0xeb, '\r'})
	if err == nil || len(got) != 0 {
		t.Fatal("partial UTF8 accepted")
	}
	got, err = in.Feed([]byte("북\r"))
	if err != nil || !reflect.DeepEqual(got, []string{"북"}) {
		t.Fatal("next line lost")
	}
}

func TestResetDiscardsPartialCredential(t *testing.T) {
	in := NewInput(64)
	in.Feed([]byte("unfinished"))
	in.Reset()
	got, err := in.Feed([]byte("new\r"))
	if err != nil || !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatal("stale input retained")
	}
}
