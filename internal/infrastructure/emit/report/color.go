// Package report renders the human-audience analysis reports: a colorized,
// TTY-aware terminal summary and a committable GitHub-flavored Markdown report.
// It is infrastructure: it reads the frozen application.Result / emit.View and
// the domain model but never mutates them (ADR 0004), and every emitter sorts
// its output so artifacts are byte-stable (golden tests depend on it).
package report

import (
	"io"
	"os"
)

// ANSI SGR codes used by the terminal report. Kept tiny and dependency-free
// (ADR 0002 prefers stdlib for trivial concerns).
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiDim    = "\033[2m"
)

// palette colorizes text for the terminal report. The zero value (enabled=false)
// is a no-op plain-text palette, which is exactly what we want when output is
// not a TTY or color is disabled.
type palette struct {
	enabled bool
}

func (p palette) wrap(code, s string) string {
	if !p.enabled {
		return s
	}
	return code + s + ansiReset
}

func (p palette) bold(s string) string   { return p.wrap(ansiBold, s) }
func (p palette) red(s string) string    { return p.wrap(ansiRed, s) }
func (p palette) green(s string) string  { return p.wrap(ansiGreen, s) }
func (p palette) yellow(s string) string { return p.wrap(ansiYellow, s) }
func (p palette) cyan(s string) string   { return p.wrap(ansiCyan, s) }
func (p palette) dim(s string) string    { return p.wrap(ansiDim, s) }

// ColorMode selects how the terminal report decides whether to emit ANSI color.
type ColorMode int

const (
	// ColorAuto enables color only when the writer is a terminal and NO_COLOR is
	// unset (the default).
	ColorAuto ColorMode = iota
	// ColorNever disables color unconditionally (--no-color or a non-TTY sink).
	ColorNever
	// ColorAlways forces color on (rarely needed; mainly for tests/pipes).
	ColorAlways
)

// useColor decides whether to colorize, honoring NO_COLOR and TTY detection.
// The rules, in order (most-specific first):
//   - ColorNever  → never.
//   - NO_COLOR set (to any value, even empty) → never (https://no-color.org).
//   - ColorAlways → always.
//   - ColorAuto   → only when w is an *os.File attached to a character device.
func useColor(mode ColorMode, w io.Writer, noColorEnv func(string) (string, bool)) bool {
	if mode == ColorNever {
		return false
	}
	if _, ok := noColorEnv("NO_COLOR"); ok {
		return false
	}
	if mode == ColorAlways {
		return true
	}
	return isTerminal(w)
}

// isTerminal reports whether w is an *os.File backed by a character device (a
// TTY). Pipes, regular files, and bytes.Buffers are not terminals, so piped or
// redirected output is never colorized.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
