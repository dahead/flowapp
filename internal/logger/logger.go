// Package logger provides a simple levelled logger for FlowApp.
//
// Format:  2009/11/10 23:00:00 [INFO]  [module] message
// Levels:  DEBUG < INFO < WARN < ERROR
//
// Usage:
//
//	log := logger.New("store")
//	log.Info("loaded %d instances", n)
//	log.Warn("duplicate workflow name %q", name)
//	log.Error("save failed: %v", err)
//	log.Debug("step activated: %s", step)   // only printed when debug mode is on
//
// Enable debug output at startup:
//
//	logger.SetDebug(true)
package logger

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

// debugEnabled controls whether DEBUG messages are printed.
// Use atomic so it can be set from main without a mutex.
var debugEnabled atomic.Bool

// SetDebug enables or disables DEBUG-level output globally.
func SetDebug(on bool) {
	debugEnabled.Store(on)
}

// IsDebug returns true if debug logging is currently enabled.
func IsDebug() bool {
	return debugEnabled.Load()
}

// Logger is a module-scoped logger. Create one per package with New.
type Logger struct {
	module string
}

// New returns a Logger tagged with the given module name (e.g. "store", "engine").
func New(module string) *Logger {
	return &Logger{module: module}
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...any) {
	l.write("INFO ", format, args...)
}

// Warn logs a warning — something unexpected but recoverable.
func (l *Logger) Warn(format string, args ...any) {
	l.write("WARN ", format, args...)
}

// Error logs an error condition.
func (l *Logger) Error(format string, args ...any) {
	l.write("ERROR", format, args...)
}

// Debug logs a debug message — only printed when debug mode is enabled.
func (l *Logger) Debug(format string, args ...any) {
	if !debugEnabled.Load() {
		return
	}
	l.write("DEBUG", format, args...)
}

// Fatal logs an error and calls os.Exit(1) via log.Fatal.
func (l *Logger) Fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Fatalf("[ERROR] [%s] %s", l.module, msg)
}

func (l *Logger) write(level, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	// log.Printf prepends the date/time from the standard log package flags
	log.Printf("[%s] [%s] %s", level, l.module, strings.TrimRight(msg, "\n"))
}
