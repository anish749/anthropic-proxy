package proxy

import (
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch starts a background goroutine that watches the prompts directory
// for changes and hot-reloads rules. Rapid edits are debounced to 500ms.
func (rw *Rewriter) Watch() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("rewriter: failed to create file watcher", "err", err)
		return
	}
	if err := watcher.Add(rw.dir); err != nil {
		slog.Error("rewriter: failed to watch directory", "dir", rw.dir, "err", err)
		watcher.Close()
		return
	}
	slog.Info("rewriter: watching for changes", "dir", rw.dir)

	go func() {
		defer watcher.Close()
		var debounce *time.Timer
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Remove) && !event.Has(fsnotify.Rename) {
					continue
				}
				// Debounce: editors often write multiple events for a single save.
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(500*time.Millisecond, func() {
					slog.Info("rewriter: detected file change, reloading rules", "event", event)
					state, err := loadRules(rw.dir)
					if err != nil {
						slog.Error("rewriter: reload failed, keeping previous rules", "err", err)
						return
					}
					rw.state.Store(state)
					rw.reqCount.Store(0)
					if rw.planMode != nil {
						if err := rw.planMode.Reload(); err != nil {
							slog.Error("rewriter: plan mode template reload failed", "err", err)
						}
					}
					slog.Info("rewriter: rules reloaded successfully")
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("rewriter: watcher error", "err", err)
			}
		}
	}()
}
