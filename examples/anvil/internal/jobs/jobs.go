// Package jobs holds the domain model of Anvil and the work the worker performs.
// It has no I/O, so it is trivially testable.
package jobs

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxInputLength caps job input so a single request can't make the worker do unbounded work.
const MaxInputLength = 10_000

// Kind is the operation a job performs.
type Kind string

const (
	KindWordCount Kind = "wordcount"
	KindUppercase Kind = "uppercase"
	KindReverse   Kind = "reverse"
)

// Kinds lists every supported kind, in the order the UI shows them.
var Kinds = []Kind{KindWordCount, KindUppercase, KindReverse}

// Status is where a job is in its lifecycle: queued -> running -> done | failed.
type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Finished reports whether a job will never change again, which makes it safe to cache.
func (s Status) Finished() bool {
	return s == StatusDone || s == StatusFailed
}

// Job is one unit of work.
type Job struct {
	ID        int64     `json:"id"`
	Kind      Kind      `json:"kind"`
	Input     string    `json:"input"`
	Status    Status    `json:"status"`
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ErrInvalid is returned for requests that can never succeed, so callers can map it to HTTP 400.
var ErrInvalid = errors.New("invalid job")

// Validate checks a new job request.
func Validate(kind Kind, input string) error {
	switch {
	case !kind.valid():
		return fmt.Errorf("%w: unknown kind %q", ErrInvalid, kind)
	case strings.TrimSpace(input) == "":
		return fmt.Errorf("%w: input is empty", ErrInvalid)
	case utf8.RuneCountInString(input) > MaxInputLength:
		return fmt.Errorf("%w: input is longer than %d characters", ErrInvalid, MaxInputLength)
	case !utf8.ValidString(input):
		return fmt.Errorf("%w: input is not valid UTF-8", ErrInvalid)
	}
	return nil
}

func (k Kind) valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Process performs the work for a job and returns its result.
func Process(kind Kind, input string) (string, error) {
	switch kind {
	case KindWordCount:
		n := len(strings.Fields(input))
		if n == 1 {
			return "1 word", nil
		}
		return fmt.Sprintf("%d words", n), nil
	case KindUppercase:
		return strings.ToUpper(input), nil
	case KindReverse:
		runes := []rune(input)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return string(runes), nil
	default:
		return "", fmt.Errorf("%w: unknown kind %q", ErrInvalid, kind)
	}
}
