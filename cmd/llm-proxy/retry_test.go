package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

func TestRetryWithBackoffLogsOperationName(t *testing.T) {
	logOutput := captureLogs(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := retryWithBackoff(ctx, "token sync", func(context.Context) error {
		return errors.New("boom")
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if !strings.Contains(logOutput.String(), "token sync failed (attempt 1), retrying in 1s: boom") {
		t.Fatalf("expected log entry with operation name, got %q", logOutput.String())
	}
}

func TestRetryWithBackoffDoublesDelay(t *testing.T) {
	logOutput := captureLogs(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	attempts := 0
	err := retryWithBackoff(ctx, "operation", func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	output := logOutput.String()
	if !strings.Contains(output, "operation failed (attempt 1), retrying in 1s: transient") {
		t.Fatalf("expected first backoff log, got %q", output)
	}
	if !strings.Contains(output, "operation failed (attempt 2), retrying in 2s: transient") {
		t.Fatalf("expected second backoff log, got %q", output)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})

	return &buf
}
