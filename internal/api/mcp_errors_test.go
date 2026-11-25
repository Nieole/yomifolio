package api

import (
	"errors"
	"fmt"
	"testing"

	"yomifolio/internal/application/delegated"
)

func TestMCPToolErrorsExposeOnlyCanonicalMessages(t *testing.T) {
	const private = "private/path.sqlite SELECT private_table"
	for _, mapper := range []struct {
		name                 string
		mapError             func(error) error
		invalid, unavailable string
	}{
		{"runs", runToolError, "invalid run parameters; use limit 1..100, nonnegative before_id and timeout_ms 0..30000", "run operation unavailable; retry later"},
		{"discovery", discoveryError, "invalid discovery filters, pagination or oversized result; use supported fields and limit 1..50, or reduce the target/page size", "local catalogue evidence unavailable; retry after current library work completes"},
	} {
		t.Run(mapper.name, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				input error
				want  string
			}{
				{"nil", nil, ""},
				{"storage", errors.New(private), mapper.unavailable},
				{"wrapped denial", fmt.Errorf("%s: %w", private, delegated.ErrDenied), errMCPDenied.Error()},
				{"wrapped validation", fmt.Errorf("%s: %w", private, delegated.ErrInvalid), mapper.invalid},
				{"denial text is not authority", fmt.Errorf("%s: %s", private, delegated.ErrDenied), mapper.unavailable},
				{"validation text is not authority", fmt.Errorf("%s: %s", private, delegated.ErrInvalid), mapper.unavailable},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got := mapper.mapError(tc.input)
					if tc.want == "" {
						if got != nil {
							t.Fatal(got)
						}
						return
					}
					if got == nil || got.Error() != tc.want {
						t.Fatalf("got %v; want %q", got, tc.want)
					}
				})
			}
		})
	}
}
