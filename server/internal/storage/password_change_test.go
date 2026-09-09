package storage

import (
	"bytes"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

func TestPasswordChangeCredentialHashValidationRejectsPlaintextAndWrongCost(t *testing.T) {
	valid, err := identity.HashPassword([]byte("old-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCredentialHash(valid); err != nil {
		t.Fatalf("valid bcrypt hash rejected: %v", err)
	}
	for _, invalid := range [][]byte{nil, []byte("old-password"), []byte("$2a$04$invalid")} {
		if err := validateCredentialHash(invalid); err == nil {
			t.Fatalf("invalid credential hash accepted: %q", invalid)
		}
	}
}

func TestPasswordChangeExpectedAndReplacementHashesAreDistinctOpaqueValues(t *testing.T) {
	oldHash, err := identity.HashPassword([]byte("old-password"))
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := identity.HashPassword([]byte("new-password"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldHash, newHash) || bytes.Equal(newHash, []byte("new-password")) {
		t.Fatal("password change test hashes are not opaque and salted")
	}
	if err := validateCredentialHash(oldHash); err != nil {
		t.Fatal(err)
	}
	if err := validateCredentialHash(newHash); err != nil {
		t.Fatal(err)
	}
}
