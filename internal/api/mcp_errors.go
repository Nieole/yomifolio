package api

import (
	"errors"

	"yomifolio/internal/application/delegated"
)

// errMCPDenied is the one refusal every MCP tool returns for a denial, whatever its cause. It
// names a next step but never tells a missing target from one outside the credential.
var errMCPDenied = errors.New("delegated access denied: unknown ID, or one this credential may not use with this tool (deliberately indistinguishable). Next: take IDs only from this credential's own results, such as library_find_series or library_list_books; if it already came from there, the grant no longer covers it, so report it instead of retrying")

// Never return the matched error: even approved sentinels may wrap private details.
func publicMCPToolError(err error, invalid, unavailable string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, delegated.ErrDenied):
		return errMCPDenied
	case errors.Is(err, delegated.ErrInvalid):
		return errors.New(invalid)
	default:
		return errors.New(unavailable)
	}
}
