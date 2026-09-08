package identity

import (
	"bytes"
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	password := []byte("pw1234")
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, password) || bytes.Equal(first, second) {
		t.Fatal("password not salted")
	}
	if !CheckPassword(first, password) || CheckPassword(first, []byte("wrong")) {
		t.Fatal("verification mismatch")
	}
	if CheckPassword([]byte("invalid"), password) {
		t.Fatal("invalid stored hash accepted")
	}
}

func TestPasswordBoundaries(t *testing.T) {
	for _, input := range []string{"", "ab", strings.Repeat("x", 15), "ab\x00", "ab\n", string([]byte{0xff, 0xff, 0xff})} {
		if _, err := HashPassword([]byte(input)); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, input := range []string{"abc", strings.Repeat("x", 14), "암호"} {
		hash, err := HashPassword([]byte(input))
		if err != nil || !CheckPassword(hash, []byte(input)) {
			t.Fatal("valid password rejected")
		}
	}
}
