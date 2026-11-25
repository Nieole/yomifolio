package delegated

import (
	"context"
	"slices"
)

// Libraries resolves the libraries one aggregate operation covers: the requested ones, or every
// library the credential currently names when none are requested. Every library must pass the
// same check as Authorize for capability; one refusal denies the whole call instead of shrinking
// it, so a caller never mistakes a partial answer for the complete one.
func (s *Service) Libraries(ctx context.Context, subject Subject, requested []int64, capability string) ([]int64, error) {
	if len(requested) > 256 {
		return nil, ErrInvalid
	}
	authority, err := s.store.Authority(ctx, subject)
	if err != nil || authority.Credential.Revoked || authority.MustChangePassword || !slices.Contains(authority.Credential.Capabilities, capability) {
		return nil, ErrDenied
	}
	libraries := slices.Clone(requested)
	if len(libraries) == 0 {
		libraries = slices.Clone(authority.Credential.LibraryIDs)
	}
	slices.Sort(libraries)
	libraries = slices.Compact(libraries)
	for _, id := range libraries {
		if id <= 0 {
			return nil, ErrInvalid
		}
		if err := authorizeAuthority(authority, id, capability); err != nil {
			return nil, err
		}
	}
	if libraries == nil {
		libraries = []int64{}
	}
	return libraries, nil
}
