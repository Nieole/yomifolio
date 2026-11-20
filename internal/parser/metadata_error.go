package parser

import "errors"

// ErrMetadataNotFound distinguishes absent metadata from unreadable archive entries.
var ErrMetadataNotFound = errors.New("metadata file not found")
