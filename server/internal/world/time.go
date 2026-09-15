package world

import (
	"errors"
	"fmt"
	"time"
)

const (
	// TimeAlias and TimeLegacyCommandID are the exact global.c registration for
	// command8.c:prt_time.  The numeric ID is 49; it is not inferred from the
	// current Go dispatcher enum.
	TimeAlias           = "시간"
	TimeLegacyCommandID = 49

	timeHoursPerDay = int64(24)
)

var (
	// C's global Time starts at zero and advances by one hour. Negative values
	// are not a meaningful server clock, so this slice refuses to normalize
	// them into a seemingly valid modulo result.
	ErrInvalidGameClock = errors.New("invalid game clock")
	// A zero wall clock means the server did not bind a realtime value to this
	// read-only command. It must not be replaced by time.Now during a retry.
	ErrInvalidServerWallClock = errors.New("invalid server wall clock")
)

// serverPST is deliberately a fixed PST location. prt_time labels ctime's
// server-local output as PST; this projection keeps that source contract
// stable across hosts that would otherwise apply a different local timezone
// or daylight-saving rule.
var serverPST = time.FixedZone("PST", -8*60*60)

// TimeProjection is the source-backed semantic output of command8.c:prt_time.
// Now is the bound legacy game clock, GameHour is now%24, and WallClock is
// ctime-shaped server realtime normalized to fixed PST. Response preserves the
// two source lines and their LF terminators. No world state is changed.
type TimeProjection struct {
	Now         int64  `json:"now"`
	GameHour    int    `json:"game_hour"`
	Period      string `json:"period"`
	DisplayHour int    `json:"display_hour"`
	WallClock   string `json:"wall_clock"`
	Response    string `json:"response"`
}

// TimeResult is a descriptive compatibility name for callers that treat the
// projection as a command result.
type TimeResult = TimeProjection

func validateTimeInputs(now int64, wallClock time.Time) error {
	if now < 0 {
		return fmt.Errorf("%w: clock must be non-negative", ErrInvalidGameClock)
	}
	if wallClock.IsZero() {
		return ErrInvalidServerWallClock
	}
	return nil
}

// ProjectTime computes prt_time without consulting process-global clocks.
// The caller supplies both values so a durable receipt can bind the exact
// observation before commit; retries never call time.Now or re-read Time.
func ProjectTime(now int64, wallClock time.Time) (TimeProjection, error) {
	if err := validateTimeInputs(now, wallClock); err != nil {
		return TimeProjection{}, err
	}

	gameHour := int(now % timeHoursPerDay)
	period := "오전"
	if gameHour > 11 {
		period = "오후"
	}
	displayHour := gameHour % 12
	if displayHour == 0 {
		displayHour = 12
	}
	serverWallClock := wallClock.Round(0).In(serverPST).Format("Mon Jan _2 15:04:05 2006")
	response := fmt.Sprintf("현재 시간: %s %d시.\n실제 시간: %s (PST).\n", period, displayHour, serverWallClock)
	return TimeProjection{
		Now:         now,
		GameHour:    gameHour,
		Period:      period,
		DisplayHour: displayHour,
		WallClock:   serverWallClock,
		Response:    response,
	}, nil
}

// RenderTime is the compact response-only form used by command adapters.
func RenderTime(now int64, wallClock time.Time) (string, error) {
	projection, err := ProjectTime(now, wallClock)
	if err != nil {
		return "", err
	}
	return projection.Response, nil
}
