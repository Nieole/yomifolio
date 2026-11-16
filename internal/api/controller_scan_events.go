package api

import (
	"yomifolio/internal/application/scanning"
	"yomifolio/internal/runhandle"
	"yomifolio/internal/scanner"
)

func newTaskScanObserver(h *runhandle.Handle) scanner.ScanObserver { return scanning.NewObserver(h) }
