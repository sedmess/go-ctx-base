package logconfig

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/sedmess/go-ctx/ctx"
)

type logCapture struct {
	reader *os.File
	writer *os.File
}

func startConfiguredLogCapture(t *testing.T, config loggingConfig, extraHandlers []slog.Handler) *logCapture {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout := os.Stdout
	os.Stdout = writer
	config.configure(extraHandlers)
	os.Stdout = previousStdout
	return &logCapture{reader: reader, writer: writer}
}

func (c *logCapture) finish(t *testing.T) string {
	t.Helper()
	if err := c.writer.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(c.reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestLoggingConfigurationLevelOutputAndReconfiguration(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { ctx.SetSlogHandler(previous.Handler()) })

	first := startConfiguredLogCapture(t, loggingConfig{level: "debug"}, nil)
	slog.Debug("first-debug-message")
	slog.Info("first-info-message")

	const syntheticSecret = "synthetic-logging-secret"
	second := startConfiguredLogCapture(t, loggingConfig{level: "error"}, nil)
	firstOutput := first.finish(t)
	if !strings.Contains(firstOutput, "first-debug-message") || !strings.Contains(firstOutput, "first-info-message") {
		t.Fatalf("debug output missing configured messages: %q", firstOutput)
	}
	slog.Warn("filtered-" + syntheticSecret)
	slog.Error("second-error-message")
	ctx.SetSlogHandler(previous.Handler())
	secondOutput := second.finish(t)
	if strings.Contains(secondOutput, syntheticSecret) || strings.Contains(secondOutput, "filtered-") {
		t.Fatalf("filtered secret-bearing message reached output: %q", secondOutput)
	}
	if !strings.Contains(secondOutput, "second-error-message") {
		t.Fatalf("error output missing configured message: %q", secondOutput)
	}
	if strings.Contains(secondOutput, "first-debug-message") {
		t.Fatalf("reconfigured output retained the prior sink: %q", secondOutput)
	}
}

func TestLoggingConfigurationFansOutToExtraHandlers(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { ctx.SetSlogHandler(previous.Handler()) })
	var captured bytes.Buffer
	extra := slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})
	stdout := startConfiguredLogCapture(t, loggingConfig{level: "info"}, []slog.Handler{extra})
	slog.Debug("extra-debug-message")
	slog.Info("extra-info-message")
	ctx.SetSlogHandler(previous.Handler())
	_ = stdout.finish(t)
	output := captured.String()
	if !strings.Contains(output, "extra-debug-message") || !strings.Contains(output, "extra-info-message") {
		t.Fatalf("extra handler output = %q", output)
	}
}

func TestUnknownLoggingLevelRetainsInfoDefault(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { ctx.SetSlogHandler(previous.Handler()) })
	stdout := startConfiguredLogCapture(t, loggingConfig{level: "not-a-level"}, nil)
	slog.Debug("default-debug-message")
	slog.Info("default-info-message")
	ctx.SetSlogHandler(previous.Handler())
	output := stdout.finish(t)
	if strings.Contains(output, "default-debug-message") || !strings.Contains(output, "default-info-message") {
		t.Fatalf("unknown-level output = %q", output)
	}
}
