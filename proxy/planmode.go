package proxy

import (
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
)

const planModeMarker = "Plan mode is active"
const planModePathPlaceholder = "{{PLAN_FILE_PATH}}"

var planModePathRe = regexp.MustCompile(`(/\S+\.md)`)

// PlanModeRewriter replaces plan mode system-reminder blocks with a custom
// template, preserving the dynamic plan file path from the original.
type PlanModeRewriter struct {
	template atomic.Value // stores string
	path     string       // template file path, for reload
}

// NewPlanModeRewriter loads the plan mode template from the given file path.
// Returns nil if the file doesn't exist (plan mode rewriting disabled).
func NewPlanModeRewriter(templatePath string) *PlanModeRewriter {
	pm := &PlanModeRewriter{path: templatePath}
	if err := pm.load(); err != nil {
		if os.IsNotExist(err) {
			slog.Info("planmode: no template found, plan mode rewriting disabled", "path", templatePath)
			return nil
		}
		slog.Error("planmode: failed to load template", "path", templatePath, "err", err)
		os.Exit(1)
	}
	return pm
}

func (pm *PlanModeRewriter) load() error {
	data, err := os.ReadFile(pm.path)
	if err != nil {
		return err
	}
	tmpl := string(data)
	if !strings.Contains(tmpl, planModePathPlaceholder) {
		slog.Warn("planmode: template does not contain path placeholder", "placeholder", planModePathPlaceholder)
	}
	pm.template.Store(tmpl)
	slog.Info("planmode: loaded template", "path", pm.path)
	return nil
}

// Reload reloads the template from disk. Called by the file watcher.
func (pm *PlanModeRewriter) Reload() error {
	return pm.load()
}

// IsPlanMode returns true if the text is a plan mode system-reminder.
func IsPlanMode(reminderContent string) bool {
	return strings.Contains(reminderContent, planModeMarker)
}

// Rewrite replaces a plan mode reminder's content with the template,
// preserving the plan file path from the original.
// Returns the replacement content and true if rewritten, or empty and false if not.
func (pm *PlanModeRewriter) Rewrite(originalContent string) (string, bool) {
	if !IsPlanMode(originalContent) {
		return "", false
	}

	tmpl, ok := pm.template.Load().(string)
	if !ok || tmpl == "" {
		return "", false
	}

	// Extract the plan file path from the original
	matches := planModePathRe.FindStringSubmatch(originalContent)
	if len(matches) == 0 {
		slog.Warn("planmode: could not extract plan file path from reminder")
		return "", false
	}
	planPath := matches[0]

	// Substitute the path into the template
	result := strings.ReplaceAll(tmpl, planModePathPlaceholder, planPath)

	// Strip outer <system-reminder> tags if present in the template,
	// since the caller wraps in tags
	result = strings.TrimPrefix(result, "<system-reminder>")
	result = strings.TrimSuffix(result, "</system-reminder>")
	result = strings.TrimSpace(result)

	slog.Info("planmode: rewrote plan mode reminder", "plan_file", planPath)
	return result, true
}
