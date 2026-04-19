package agentloop

import "errors"

// PauseTurnError indicates that tool execution intentionally paused the current
// turn and the loop should return without treating it as a failure.
type PauseTurnError struct {
	Reason string
}

func (e *PauseTurnError) Error() string {
	if e == nil || e.Reason == "" {
		return "agent loop turn paused"
	}
	return e.Reason
}

// ErrPauseTurn is a sentinel error for callers that do not need a custom reason.
var ErrPauseTurn = &PauseTurnError{}

// IsPauseTurn reports whether err pauses the current turn intentionally.
func IsPauseTurn(err error) bool {
	var pauseErr *PauseTurnError
	return errors.As(err, &pauseErr)
}
