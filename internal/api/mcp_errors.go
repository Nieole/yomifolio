package api

import (
	"errors"

	"yomifolio/internal/application/delegated"
)

// Never return the matched error: even approved sentinels may wrap private details.
func publicMCPToolError(err error, invalid, unavailable string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, delegated.ErrDenied):
		return delegated.ErrDenied
	case errors.Is(err, delegated.ErrInvalid):
		return errors.New(invalid)
	default:
		return errors.New(unavailable)
	}
}
