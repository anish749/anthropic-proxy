package proxy

import (
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
)

// SetupLogger installs a tint handler as the default slog logger,
// with colorful output and kitchen time format.
func SetupLogger() {
	h := tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.Kitchen,
	})
	slog.SetDefault(slog.New(h))
}
