package delegated

import (
	"context"
	"maps"
	"strconv"

	"yomifolio/internal/runhandle"
	"yomifolio/internal/task"
	"yomifolio/internal/taskcontrol"
)

// Checkpoint rechecks live authority. Revocation is cooperative cancellation,
// not compensation for previously committed work or already issued requests.
func (s *Service) Checkpoint(ctx context.Context, subject Subject, library int64, capability string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Authorize(ctx, subject, library, capability); err != nil {
		return context.Canceled
	}
	return nil
}

// PrepareRun binds durable attribution and isolates queue coalescing by credential.
// Callers must supply all actual libraries, and use Checkpoint before external calls
// and a transaction-bound authorization fence for writes. Subject is never a grant.
func (s *Service) PrepareRun(ctx context.Context, subject Subject, library int64, capability string, spec task.RunSpec, body task.Body) (task.RunSpec, task.Body, error) {
	if len(spec.LibraryIDs) != 1 || spec.LibraryIDs[0] != library || body == nil {
		return spec, nil, ErrInvalid
	}
	if err := s.Authorize(ctx, subject, library, capability); err != nil {
		return spec, nil, err
	}
	spec.Identity.Variant = task.Variant("delegated:" + subject.CredentialID + ":" + string(spec.Identity.Variant))
	spec.AtomicArgs = true
	spec.Args = maps.Clone(spec.Args)
	if spec.Args == nil {
		spec.Args = map[string]string{}
	}
	spec.Args["delegated_user_id"] = strconv.FormatInt(subject.UserID, 10)
	spec.Args["delegated_credential_id"] = subject.CredentialID
	spec.Args["delegated_library_id"] = strconv.FormatInt(library, 10)
	spec.Args["delegated_capability"] = capability
	prior := spec.ValidateAccess
	spec.ValidateAccess = func() error {
		if err := s.Authorize(context.Background(), subject, library, capability); err != nil {
			return err
		}
		if prior != nil {
			return prior()
		}
		return nil
	}
	check := func(ctx context.Context) error { return s.Checkpoint(ctx, subject, library, capability) }
	spec.ExecutionCheck = check
	return spec, func(ctx context.Context, h *runhandle.Handle) (task.Result, error) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		fence := func(ctx context.Context) error {
			err := check(ctx)
			if err != nil {
				cancel()
			}
			return err
		}
		ctx = taskcontrol.WithCheckpoint(ctx, fence)
		if err := fence(ctx); err != nil {
			return task.Result{}, err
		}
		result, err := body(ctx, h)
		if err == nil {
			err = fence(ctx)
		}
		return result, err
	}, nil
}
