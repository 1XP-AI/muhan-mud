package session

import (
	"errors"
	"testing"
)

func TestGameCommandsRequireConfirmedAdmission(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	forbidden := func() error { t.Fatal("unadmitted command ran"); return nil }
	if err := owners.RunGame(lease, forbidden); err == nil {
		t.Fatal("pending command accepted")
	}
	fail := errors.New("admission uncertain")
	if err := owners.Admit(lease, func() error { return fail }); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if err := owners.RunGame(lease, forbidden); err == nil {
		t.Fatal("failed admission accepted")
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := owners.RunGame(lease, func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal("admitted command blocked")
	}
	if err := owners.Finish(lease, func() error { return fail }); err == nil {
		t.Fatal("expected close failure")
	}
	if err := owners.RunGame(lease, forbidden); err == nil {
		t.Fatal("closing command ran")
	}
	if err := owners.Finish(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	next, _ := owners.Acquire("a")
	if err := owners.RunGame(next, forbidden); err == nil {
		t.Fatal("new lease inherited admission")
	}
}
