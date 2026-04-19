package server

import (
	"context"
	"time"
)

type ToolProgressUpdate struct {
	Phase          string
	Label          string
	Message        string
	ActivityKind   string
	ActivityTarget string
	ActivityQuery  string
	CreatedAt      string
	Timestamp      time.Time
	Payload        map[string]any
	Metadata       map[string]any
}

type toolProgressReporter func(ToolProgressUpdate) error

type toolProgressContextKey struct{}

func withToolProgressReporter(ctx context.Context, reporter toolProgressReporter) context.Context {
	if ctx == nil || reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, toolProgressContextKey{}, reporter)
}

func ReportToolProgress(ctx context.Context, update ToolProgressUpdate) error {
	if ctx == nil {
		return nil
	}
	reporter, _ := ctx.Value(toolProgressContextKey{}).(toolProgressReporter)
	if reporter == nil {
		return nil
	}
	return reporter(update)
}
