// Package tracecontext implements the small W3C trace context surface Sigma
// needs to carry trace identity across HTTP, task records, and child processes.
package tracecontext

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type Context struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	TraceFlags   string
}

func New() (Context, error) {
	var traceID [16]byte
	var spanID [8]byte
	if _, err := rand.Read(traceID[:]); err != nil {
		return Context{}, fmt.Errorf("generate trace ID: %w", err)
	}
	if _, err := rand.Read(spanID[:]); err != nil {
		return Context{}, fmt.Errorf("generate span ID: %w", err)
	}
	if allZero(traceID[:]) || allZero(spanID[:]) {
		return New()
	}
	return Context{
		TraceID:    hex.EncodeToString(traceID[:]),
		SpanID:     hex.EncodeToString(spanID[:]),
		TraceFlags: "01",
	}, nil
}

func ParseTraceparent(value string) (Context, error) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return Context{}, errors.New("traceparent must use W3C version 00 format")
	}
	traceID, err := hex.DecodeString(parts[1])
	if err != nil || allZero(traceID) {
		return Context{}, errors.New("traceparent contains an invalid trace ID")
	}
	spanID, err := hex.DecodeString(parts[2])
	if err != nil || allZero(spanID) {
		return Context{}, errors.New("traceparent contains an invalid parent span ID")
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return Context{}, errors.New("traceparent contains invalid trace flags")
	}
	return Context{
		TraceID:    strings.ToLower(parts[1]),
		SpanID:     strings.ToLower(parts[2]),
		TraceFlags: strings.ToLower(parts[3]),
	}, nil
}

func Child(parent Context) (Context, error) {
	if !parent.Valid() {
		return New()
	}
	var spanID [8]byte
	if _, err := rand.Read(spanID[:]); err != nil {
		return Context{}, fmt.Errorf("generate span ID: %w", err)
	}
	if allZero(spanID[:]) {
		return Child(parent)
	}
	flags := parent.TraceFlags
	if flags == "" {
		flags = "01"
	}
	return Context{
		TraceID:      parent.TraceID,
		SpanID:       hex.EncodeToString(spanID[:]),
		ParentSpanID: parent.SpanID,
		TraceFlags:   flags,
	}, nil
}

func (c Context) Valid() bool {
	traceID, traceErr := hex.DecodeString(c.TraceID)
	spanID, spanErr := hex.DecodeString(c.SpanID)
	return traceErr == nil && spanErr == nil && len(traceID) == 16 && len(spanID) == 8 && !allZero(traceID) && !allZero(spanID)
}

func (c Context) Traceparent() string {
	if !c.Valid() {
		return ""
	}
	flags := strings.ToLower(c.TraceFlags)
	if len(flags) != 2 {
		flags = "01"
	}
	if _, err := hex.DecodeString(flags); err != nil {
		flags = "01"
	}
	return "00-" + strings.ToLower(c.TraceID) + "-" + strings.ToLower(c.SpanID) + "-" + flags
}

func WithContext(ctx context.Context, trace Context) context.Context {
	return context.WithValue(ctx, contextKey{}, trace)
}

func FromContext(ctx context.Context) (Context, bool) {
	trace, ok := ctx.Value(contextKey{}).(Context)
	return trace, ok && trace.Valid()
}

type contextKey struct{}

func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}
