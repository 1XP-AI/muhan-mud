package world

import (
	"errors"
	"testing"
	"time"
)

func TestProjectTimeUsesGameClockModuloAndServerPST(t *testing.T) {
	wallClock := time.Date(2026, time.January, 2, 23, 4, 5, 0, time.UTC)
	projection, err := ProjectTime(25, wallClock)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Now != 25 || projection.GameHour != 1 || projection.Period != "오전" || projection.DisplayHour != 1 {
		t.Fatalf("projection clock=%+v", projection)
	}
	if projection.WallClock != "Fri Jan  2 15:04:05 2026" {
		t.Fatalf("wall clock=%q", projection.WallClock)
	}
	want := "현재 시간: 오전 1시.\n실제 시간: Fri Jan  2 15:04:05 2026 (PST).\n"
	if projection.Response != want {
		t.Fatalf("response=%q want=%q", projection.Response, want)
	}
	if text, err := RenderTime(25, wallClock); err != nil || text != want {
		t.Fatalf("render=%q err=%v", text, err)
	}
}

func TestProjectTimeUsesLegacyTwelveHourBoundaries(t *testing.T) {
	wallClock := time.Date(2026, time.January, 2, 15, 4, 5, 0, time.FixedZone("KST", 9*60*60))
	for _, tc := range []struct {
		now         int64
		period      string
		displayHour int
	}{
		{now: 0, period: "오전", displayHour: 12},
		{now: 11, period: "오전", displayHour: 11},
		{now: 12, period: "오후", displayHour: 12},
		{now: 23, period: "오후", displayHour: 11},
		{now: 24, period: "오전", displayHour: 12},
	} {
		projection, err := ProjectTime(tc.now, wallClock)
		if err != nil {
			t.Fatalf("now=%d err=%v", tc.now, err)
		}
		if projection.GameHour != int(tc.now%24) || projection.Period != tc.period || projection.DisplayHour != tc.displayHour {
			t.Fatalf("now=%d projection=%+v", tc.now, projection)
		}
	}
}

func TestProjectTimeRejectsInvalidClockInputs(t *testing.T) {
	wallClock := time.Unix(0, 0)
	if _, err := ProjectTime(-1, wallClock); !errors.Is(err, ErrInvalidGameClock) {
		t.Fatalf("negative game clock err=%v", err)
	}
	if _, err := ProjectTime(1, time.Time{}); !errors.Is(err, ErrInvalidServerWallClock) {
		t.Fatalf("zero wall clock err=%v", err)
	}
}
