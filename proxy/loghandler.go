package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"
)

// prettyHandler is a slog.Handler that outputs human-friendly log lines
// with kitchen time (3:04:05PM), colored level tags, and key=value attrs.
type prettyHandler struct {
	level slog.Leveler
	mu    sync.Mutex
	out   io.Writer
	color bool
}

// SetupLogger installs a pretty slog handler as the default logger.
func SetupLogger() {
	h := &prettyHandler{
		level: slog.LevelDebug,
		out:   os.Stderr,
		color: isTerminal(os.Stderr),
	}
	slog.SetDefault(slog.New(h))
}

func (h *prettyHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level.Level()
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	kitchen := r.Time.Format("3:04:05PM")
	lvl, color := levelTag(r.Level)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.color {
		// time (dim) + level (colored) + message + attrs
		fmt.Fprintf(h.out, "\033[2m%s\033[0m %s%s\033[0m %s", kitchen, color, lvl, r.Message)
	} else {
		fmt.Fprintf(h.out, "%s %s %s", kitchen, lvl, r.Message)
	}

	r.Attrs(func(a slog.Attr) bool {
		if h.color {
			fmt.Fprintf(h.out, " \033[2m%s=\033[0m%s", a.Key, a.Value.String())
		} else {
			fmt.Fprintf(h.out, " %s=%s", a.Key, a.Value.String())
		}
		return true
	})

	fmt.Fprintln(h.out)
	return nil
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h // not needed for this project
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	return h // not needed for this project
}

// levelTag returns a short label and ANSI color prefix for each level.
func levelTag(l slog.Level) (label, color string) {
	switch {
	case l >= slog.LevelError:
		return "ERR", "\033[1;31m" // bold red
	case l >= slog.LevelWarn:
		return "WRN", "\033[1;33m" // bold yellow
	case l >= slog.LevelInfo:
		return "INF", "\033[1;36m" // bold cyan
	default:
		return "DBG", "\033[1;35m" // bold magenta
	}
}

// isTerminal reports whether w is a terminal (for color detection).
func isTerminal(f *os.File) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	o, err := f.Stat()
	if err != nil {
		return false
	}
	return (o.Mode() & os.ModeCharDevice) != 0
}
