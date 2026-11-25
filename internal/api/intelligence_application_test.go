package api

import (
	"context"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/organize"
	"yomifolio/internal/task"
)

func (c *apiFixture) launchAIGroupingTask(id int64, locale string, trigger task.Trigger) error {
	_, err := c.intelligence.GroupLibrary(context.Background(), id, locale, trigger)
	return err
}
func (c *apiFixture) createAIGroupingReview(ctx context.Context, id int64, provider string, candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) (database.AiGroupingReview, int, error) {
	return organize.NewAIGrouping(c.store, nil).QueueGenerated(ctx, id, provider, candidates, groups)
}
