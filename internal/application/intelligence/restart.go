package intelligence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"yomifolio/internal/metadata"
	"yomifolio/internal/task"
)

// ErrAutomaticResumeUnsupported protects paid generation from unattended replay.
var ErrAutomaticResumeUnsupported = errors.New("intelligence work cannot automatically resume")

// RestartDefinition is the single assembly-time retry and resume declaration.
type RestartDefinition struct {
	Type      task.Type
	Variant   task.Variant
	Resumable bool
}

// Restarts declares supported manual retry variants; none resume automatically.
func Restarts() []RestartDefinition {
	return []RestartDefinition{{Type: "ai_grouping"}}
}

// Restart replays the persisted locale; a run that persisted none takes the request locale.
func (c *Service) Restart(ctx context.Context, identity task.Identity, args map[string]string, trigger task.Trigger) (task.Launched, error) {
	for _, definition := range Restarts() {
		if definition.Type != identity.Type || definition.Variant != identity.Variant {
			continue
		}
		if trigger == task.TriggerResumed && !definition.Resumable {
			return task.Launched{}, ErrAutomaticResumeUnsupported
		}
		locale := strings.TrimSpace(args["locale"])
		if locale == "" {
			locale = metadata.LocaleFromContext(ctx)
		}
		ctx = metadata.WithLocale(ctx, locale)
		if identity.Type == "ai_grouping" && identity.Scope == task.ScopeLibrary && identity.ScopeID > 0 {
			return c.GroupLibrary(ctx, identity.ScopeID, locale, trigger)
		}
	}
	return task.Launched{}, fmt.Errorf("unsupported generation identity: %+v", identity)
}
