// Package terminal implements transport input, not game command interpretation.
package terminal

import (
	"errors"
	"unicode/utf8"
)

// ErrInput deliberately contains no input text: input may be a password.
var ErrInput = errors.New("invalid or oversized terminal line")

// Input belongs to one connection's serial receive loop. It is not concurrent.
// Feed never echoes input. Rendering and password modes belong to the session.
type Input struct {
	limit   int
	line    []byte
	pending []byte
	discard bool
	afterCR bool
}

func NewInput(maxBytes int) *Input {
	if maxBytes <= 0 {
		panic("terminal input limit must be positive")
	}
	return &Input{limit: maxBytes}
}

// Reset drops unfinished input on disconnect or a session boundary.
func (in *Input) Reset() {
	clear(in.line)
	clear(in.pending)
	in.line = in.line[:0]
	in.pending = in.pending[:0]
	in.discard = false
	in.afterCR = false
}

// Feed frames UTF-8 stream chunks. A rejected line is discarded through its
// terminator, so its suffix cannot accidentally execute as a separate command.
// Valid complete lines and a generic rejection may both be returned.
func (in *Input) Feed(chunk []byte) (lines []string, err error) {
	reject := func() {
		clear(in.line)
		clear(in.pending)
		in.line = in.line[:0]
		in.pending = in.pending[:0]
		in.discard = true
		err = ErrInput
	}
	for _, b := range chunk {
		if in.afterCR && b == '\n' {
			in.afterCR = false
			continue
		}
		in.afterCR = false
		if b == '\r' || b == '\n' {
			if len(in.pending) != 0 {
				reject()
			}
			if !in.discard {
				lines = append(lines, string(in.line))
			}
			in.Reset()
			in.afterCR = b == '\r'
			continue
		}
		if in.discard {
			continue
		}
		if b == '\b' || b == 127 {
			if len(in.pending) != 0 {
				reject()
				continue
			}
			if len(in.line) != 0 {
				_, size := utf8.DecodeLastRune(in.line)
				clear(in.line[len(in.line)-size:])
				in.line = in.line[:len(in.line)-size]
			}
			continue
		}
		if b < 32 || len(in.line)+len(in.pending) >= in.limit {
			reject()
			continue
		}
		in.pending = append(in.pending, b)
		if !utf8.FullRune(in.pending) {
			continue
		}
		r, size := utf8.DecodeRune(in.pending)
		if r == utf8.RuneError && size == 1 {
			reject()
			continue
		}
		in.line = append(in.line, in.pending...)
		clear(in.pending)
		in.pending = in.pending[:0]
	}
	return lines, err
}
