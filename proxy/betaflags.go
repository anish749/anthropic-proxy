package proxy

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type betaFlagConfig struct {
	Disable []string `yaml:"disable"`
}

type BetaFlagRewriter struct {
	dir    string
	config atomic.Pointer[betaFlagConfig]
}

func NewBetaFlagRewriter(dir string) *BetaFlagRewriter {
	bfr := &BetaFlagRewriter{dir: dir}
	cfg := bfr.loadConfig()
	bfr.config.Store(cfg)
	return bfr
}

const betaFlagFile = "beta_flags.yaml"

func (bfr *BetaFlagRewriter) loadConfig() *betaFlagConfig {
	data, err := os.ReadFile(filepath.Join(bfr.dir, betaFlagFile))
	if err != nil {
		return &betaFlagConfig{}
	}
	var cfg betaFlagConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		slog.Error("betaflags: failed to parse config", "err", err)
		return &betaFlagConfig{}
	}
	if len(cfg.Disable) > 0 {
		slog.Info("betaflags: loaded config", "disable", cfg.Disable)
	}
	return &cfg
}

// Watch starts a background goroutine that hot-reloads the config on changes.
func (bfr *BetaFlagRewriter) Watch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("betaflags: failed to create file watcher", "err", err)
		return
	}
	if err := watcher.Add(bfr.dir); err != nil {
		slog.Error("betaflags: failed to watch directory", "dir", bfr.dir, "err", err)
		watcher.Close()
		return
	}

	go func() {
		defer watcher.Close()
		var debounce *time.Timer
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(event.Name) != betaFlagFile {
					continue
				}
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(500*time.Millisecond, func() {
					slog.Info("betaflags: detected file change, reloading")
					bfr.config.Store(bfr.loadConfig())
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("betaflags: watcher error", "err", err)
			}
		}
	}()
}

// Rewrite removes disabled beta flags from the Anthropic-Beta header.
func (bfr *BetaFlagRewriter) Rewrite(req *http.Request) {
	cfg := bfr.config.Load()
	if len(cfg.Disable) == 0 {
		return
	}

	header := req.Header.Get("Anthropic-Beta")
	if header == "" {
		return
	}

	disabled := make(map[string]bool, len(cfg.Disable))
	for _, f := range cfg.Disable {
		disabled[f] = true
	}

	flags := strings.Split(header, ",")
	var kept []string
	var removed []string
	for _, f := range flags {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if disabled[f] {
			removed = append(removed, f)
		} else {
			kept = append(kept, f)
		}
	}

	if len(removed) == 0 {
		return
	}

	slog.Info("betaflags: removed flags", "removed", removed)
	if len(kept) == 0 {
		req.Header.Del("Anthropic-Beta")
	} else {
		req.Header.Set("Anthropic-Beta", strings.Join(kept, ","))
	}
}
