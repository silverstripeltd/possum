package possum

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
)

const minSizeTag = "possum:min_size"

func DoAutoScalingGroups(ctx context.Context, client autoscalingiface.AutoScalingAPI, ts time.Time, schedules Schedules) (Changes, error) {
	groups, err := getAutoScalingGroups(ctx, client)
	if err != nil {
		return nil, err
	}
	changes := getASGGroupChanges(groups, ts, schedules)
	err = performASGChanges(client, changes)
	return changes, err
}

type GroupSchedule struct {
	resource *autoscaling.Group
	schedule string
}

func getAutoScalingGroups(ctx context.Context, client autoscalingiface.AutoScalingAPI) ([]*GroupSchedule, error) {

	// first we need to get all autoscaling tag description that has the tag key in it, since DescribeAutoScalingGroups
	// doesn't have a filter for tags
	tagParams := &autoscaling.DescribeTagsInput{
		Filters: []*autoscaling.Filter{
			{Name: aws.String("key"), Values: []*string{aws.String(scheduleTag)}},
		},
	}

	var groupNames []*string
	var schemas []string
	tagCollector := func(page *autoscaling.DescribeTagsOutput, lastPage bool) bool {
		for _, tag := range page.Tags {
			groupNames = append(groupNames, tag.ResourceId)
			schemas = append(schemas, *tag.Value)
		}
		return true
	}

	err := client.DescribeTagsPagesWithContext(ctx, tagParams, tagCollector)
	if err != nil {
		return nil, err
	}

	// no autoscaling group has been tagged with scheduleTag
	if len(groupNames) == 0 {
		return nil, nil
	}

	// now we have a list of asg
	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: groupNames,
	}

	var result []*GroupSchedule
	collector := func(page *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {
		for i, group := range page.AutoScalingGroups {
			result = append(result, &GroupSchedule{
				resource: group,
				schedule: schemas[i],
			})
		}
		return true
	}

	err = client.DescribeAutoScalingGroupsPagesWithContext(ctx, params, collector)
	return result, err
}

func getASGGroupChanges(list []*GroupSchedule, ts time.Time, schedules Schedules) Changes {

	var changes Changes

	for _, a := range list {
		group := a.resource
		// skip groups that are in a transitional state
		if *group.DesiredCapacity != int64(len(group.Instances)) {
			continue
		}

		// if any scaling processes are suspended, we back off to ensure we don't muck with any active transitions
		if len(group.SuspendedProcesses) > 0 {
			continue
		}

		effectiveSchedule := schedules.Find(a.schedule)
		if effectiveSchedule == nil {
			log.Printf("WARN could not find schedule %s for group %s", a.schedule, *getASGName(group))
			continue
		}

		isRunning := len(group.Instances) != 0
		act := effectiveSchedule.Action(ts, isRunning)
		if act == NoopAction {
			continue
		}

		changes = append(changes, Change{
			ID:             group.AutoScalingGroupName,
			Name:           *getASGName(group),
			Action:         act,
			Type:           "asg",
			minSize:        getASGTagInt64(group.Tags, minSizeTag, 1),
			currentMinSize: *group.MinSize,
		})
	}
	return changes
}

func performASGChanges(client autoscalingiface.AutoScalingAPI, changes []Change) error {
	for _, change := range changes {
		switch change.Action {
		case StartAction:
			err := updateASGSize(client, change.ID, change.minSize)
			if err != nil {
				return err
			}
		case StopAction:
			err := updateASGSize(client, change.ID, 0)
			if err != nil {
				return err
			}
			// tag current min size so that the StartAction can reset the value to this value
			if err := tagASGGroupSize(client, change.ID, change.currentMinSize); err != nil {
				return err
			}
		}
	}
	return nil
}

func updateASGSize(client autoscalingiface.AutoScalingAPI, name *string, minSize int64) error {
	params := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: name,
		MinSize:              aws.Int64(minSize),
		DesiredCapacity:      aws.Int64(minSize),
	}
	_, err := client.UpdateAutoScalingGroup(params)
	return err
}

func tagASGGroupSize(client autoscalingiface.AutoScalingAPI, name *string, minSize int64) error {
	_, err := client.CreateOrUpdateTags(&autoscaling.CreateOrUpdateTagsInput{
		Tags: []*autoscaling.Tag{
			{
				ResourceId:        name,
				Key:               aws.String(minSizeTag),
				Value:             aws.String(fmt.Sprintf("%d", minSize)),
				PropagateAtLaunch: aws.Bool(false),
				ResourceType:      aws.String("auto-scaling-group"),
			},
		},
	})
	return err
}

// getASGTagInt64 returns a int64 value parsed from the a specific AutoScalingGroups tag key, if parsing fails, return the defaultVal
func getASGTagInt64(tags []*autoscaling.TagDescription, tagKey string, defaultVal int64) int64 {
	val := getASGTagValue(tags, tagKey)
	if val == nil {
		return defaultVal
	}

	i, err := strconv.ParseInt(*val, 10, 64)
	if err != nil {
		return defaultVal
	}
	return i

}

// helper to get a specific value out of autoscaling tag descriptions
func getASGTagValue(tags []*autoscaling.TagDescription, keyName string) *string {
	if tags == nil {
		return nil
	}
	for _, tag := range tags {
		if *tag.Key == keyName {
			return tag.Value
		}
	}
	return nil
}

func getASGName(group *autoscaling.Group) *string {
	name := getASGTagValue(group.Tags, "Name")
	if name != nil {
		return name
	}
	return group.AutoScalingGroupName
}
