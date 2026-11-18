package database

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
	"modernc.org/sqlite"
)

// NormalizeSearchText defines the locale-independent catalogue search representation.
// The case transformer belongs to this call; transformers are not shared by callers.
func NormalizeSearchText(text string) string {
	text = cases.Fold().String(norm.NFC.String(width.Fold.String(text)))
	text = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return r
	}, text)
	return norm.NFC.String(text)
}

func init() {
	// Registration on the default driver covers every subsequently opened connection,
	// including migration, maintenance and pooled Store connections. External writers
	// must provide this same function before modifying series or books.
	if err := sqlite.RegisterDeterministicScalarFunction("search_normalize", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if args[0] == nil {
				return "", nil
			}
			text, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("search_normalize: expected text, got %T", args[0])
			}
			return NormalizeSearchText(text), nil
		}); err != nil {
		panic(fmt.Errorf("register search_normalize: %w", err))
	}
}
