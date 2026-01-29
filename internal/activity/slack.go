package activity

import (
	"context"
	"os"

	"github.com/slack-go/slack"
	"go.temporal.io/sdk/activity"
)

// SlackActivities contains activities for Slack notifications.
type SlackActivities struct{}

// NewSlackActivities creates a new SlackActivities instance.
func NewSlackActivities() *SlackActivities {
	return &SlackActivities{}
}

// NotifySlack sends a notification to Slack.
func (a *SlackActivities) NotifySlack(ctx context.Context, channel, message string, threadTS *string) (*string, error) {
	logger := activity.GetLogger(ctx)

	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		logger.Warn("SLACK_BOT_TOKEN not set, skipping notification")
		return nil, nil
	}

	api := slack.New(token)

	opts := []slack.MsgOption{
		slack.MsgOptionText(message, false),
	}

	if threadTS != nil && *threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(*threadTS))
	}

	_, ts, err := api.PostMessageContext(ctx, channel, opts...)
	if err != nil {
		logger.Error("Slack API error", "error", err)
		// Don't fail the workflow for Slack errors
		return nil, nil
	}

	return &ts, nil
}
