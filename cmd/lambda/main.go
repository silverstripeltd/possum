package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/slack-go/slack"
	"github.com/silverstripeltd/possum"
)

func main() {
	lambda.Start(Handler)
}

// @todo handle env variables with KMS
// @todo tag resources with possum last action time stamp
// @todo fetch period information from aws dynamodb / s3 bucket (dynamo with 1/1 capacity is around $0.67 / month
func Handler(ctx context.Context, evt events.CloudWatchEvent) (interface{}, error) {

	notifier := &Slack{
		channelID: os.Getenv("SLACK_CHANNEL"),
		token:     os.Getenv("SLACK_TOKEN"),
	}

	tableName := os.Getenv("CONFIG_TABLE")
	if tableName == "" {
		return nil, errors.New("env variable CONFIG_TABLE is empty, this should be the name of dynamodb table, see docs")
	}

	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String("ap-southeast-2")}))
	client := dynamodb.New(sess)
	schedules, err := possum.GetSchedules(client, tableName)
	if err != nil {
		return nil, err
	}

	if len(schedules) == 0 {
		return nil, fmt.Errorf("did not find any schedules in storage '%s'", tableName)
	}

	regions, err := getRegions(ctx)
	if err != nil {
		return nil, err
	}

	var errs []error
	regionalChanges := make(map[string]possum.Changes)

	var x sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(regions))
	for _, region := range regions {
		go func(r *string) {
			defer wg.Done()
			changes, err := perRegion(r, ctx, evt, schedules)
			if err != nil {
				errs = append(errs, err)
			}
			if len(changes) > 0 {
				x.Lock()
				regionalChanges[*r] = changes
				x.Unlock()
			}
		}(region)
		// wait a bit before next region so that we do not so easily get into rate limiting
		time.Sleep(time.Millisecond * 500)
	}
	wg.Wait()

	var outputErr error
	if len(errs) > 0 {
		fmt.Printf("%d errors, first error was: %s", len(errs), errs[len(errs)-1])
	}

	var notification string
	for region, changes := range regionalChanges {
		if len(changes) != 0 {
			str := format(changes)
			notification += fmt.Sprintf("*%s*\n%s\n", region, str)
		}
	}

	if notification != "" {
		if err := notifier.Notify(notification); err != nil {
			return regionalChanges, err
		}
	}

	return regionalChanges, outputErr
}

func perRegion(region *string, ctx context.Context, evt events.CloudWatchEvent, schedules possum.Schedules) (possum.Changes, error) {

	sess := session.Must(session.NewSession(&aws.Config{Region: region}))

	changes, err := possum.DoInstances(ctx, ec2.New(sess), evt.Time, schedules)
	if err != nil {
		return changes, err
	}

	asg, err := possum.DoAutoScalingGroups(ctx, autoscaling.New(sess), evt.Time, schedules)
	changes = changes.Append(asg)
	if err != nil {
		return changes, err
	}

	db, err := possum.DoDB(ctx, rds.New(sess), evt.Time, schedules)
	changes = changes.Append(db)
	if err != nil {
		return changes, err
	}

	return changes, nil
}

func getRegions(ctx context.Context) ([]*string, error) {

	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String("ap-southeast-2")}))
	client := ec2.New(sess)

	res, err := client.DescribeRegionsWithContext(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, err
	}
	var regions []*string
	for _, reg := range res.Regions {
		regions = append(regions, reg.RegionName)
	}
	return regions, err
}

func format(s possum.Changes) string {
	var str strings.Builder

	for _, a := range s {
		str.WriteString(fmt.Sprintf(" • %s `%s` (%s, %s)\n", a.Action, a.Name, a.Type, *a.ID))
	}

	return str.String()
}

type Slack struct {
	token     string
	channelID string
}

func (s *Slack) Notify(message string) error {

	if s.token == "" || s.channelID == "" {
		return errors.New("can't send slack notification, missing either SLACK_TOKEN or SLACK_CHANNEL")
	}

	api := slack.New(s.token)

	_, _, err := api.PostMessage(s.channelID, slack.MsgOptionText(message, false), slack.MsgOptionAsUser(true))
	return err
}
