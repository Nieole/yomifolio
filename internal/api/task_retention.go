package api

import (
	"time"
	"yomifolio/internal/application"
)

const cleanupRunHistoryTaskKey = application.CleanupRunHistoryTaskKey

func retentionAge(days int) time.Duration { return application.RetentionAge(days) }
