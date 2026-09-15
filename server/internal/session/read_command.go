package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedReadLine is returned before a receipt is created when the
// line is outside this deliberately small read-only slice.  In particular,
// help/info are not silently approximated: their legacy implementations read
// external documents or use a continuation prompt, neither of which belongs
// to this pure State/receipt boundary yet.
var ErrUnsupportedReadLine = errors.New("line is not an implemented read-only command")

// ReadLineOptions contains every clock value used by ExecuteReadLine.  The
// caller must bind both values to the command before execution; reading the
// process clock here would make a receipt depend on when a retry happened.
// WallClock is formatted in its supplied location, just as ctime(3) formats
// the server's local clock.  No location or current time is inferred here.
type ReadLineOptions struct {
	GameHour  int
	WallClock time.Time
}

type readLineRequest struct {
	Kind       string
	Actor      string
	Generation uint64
	Line       string
	GameHour   int
	WallClock  time.Time
}

// ExecuteReadLine implements only the exact bare Korean 시간 command from
// command8.c's prt_time.  The resulting receipt is read-only with respect to
// world state: the reducer returns the original snapshot bytes unchanged.
// Clock inputs are part of the durable request identity, so a retry with the
// same command ID never re-renders against a different clock or re-runs a
// reducer after a receipt exists.
func (o *Ownership) ExecuteReadLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options ReadLineOptions) (storage.WorldReceipt, error) {
	if strings.TrimSpace(line) != "시간" {
		return storage.WorldReceipt{}, ErrUnsupportedReadLine
	}
	if options.GameHour < 0 || options.GameHour > 23 {
		return storage.WorldReceipt{}, fmt.Errorf("game hour outside 0..23")
	}
	if options.WallClock.IsZero() {
		return storage.WorldReceipt{}, fmt.Errorf("wall clock is required for 시간")
	}

	request, err := json.Marshal(readLineRequest{
		Kind:       "read-time",
		Actor:      lease.ActorID,
		Generation: lease.Generation,
		Line:       "시간",
		GameHour:   options.GameHour,
		WallClock:  options.WallClock,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}

	var receipt storage.WorldReceipt
	err = o.RunGame(lease, func() error {
		var err error
		receipt, err = engine.Execute(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
			state, err := world.DecodeState(raw)
			if err != nil {
				return nil, nil, err
			}
			player, ok := state.Players[lease.ActorID]
			if !ok || !player.Online {
				return nil, nil, fmt.Errorf("online read actor required")
			}
			response := fmt.Sprintf("현재 시간: %s %d시.\n실제 시간: %s (PST).\n", gamePeriod(options.GameHour), gameClockHour(options.GameHour), options.WallClock.Format("Mon Jan _2 15:04:05 2006"))
			encoded, err := json.Marshal(response)
			if err != nil {
				return nil, nil, err
			}
			return raw, encoded, nil
		})
		return err
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return receipt, nil
}

func gamePeriod(hour int) string {
	if hour > 11 {
		return "오후"
	}
	return "오전"
}

func gameClockHour(hour int) int {
	display := hour % 12
	if display == 0 {
		return 12
	}
	return display
}
