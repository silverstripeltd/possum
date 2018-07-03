package possum

import (
	"context"
	"testing"

	"time"

	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
)

func TestGetAutoScalingGroups(t *testing.T) {

	groupOutput := []*autoscaling.Group{
		{AutoScalingGroupName: aws.String("group1")},
		{AutoScalingGroupName: aws.String("group2")},
	}
	tagOutput := []*autoscaling.TagDescription{
		{ResourceId: aws.String("group1"), Value: aws.String("OfficeHours"), Key: aws.String(scheduleTag)},
		{ResourceId: aws.String("group2"), Value: aws.String("OfficeHours"), Key: aws.String(scheduleTag)},
	}

	client := &mockAutoscalingClient{
		describeAutoScalingGroupsResult: groupOutput,
		describeTagsResult:              tagOutput,
	}
	ctx := context.Background()

	list, err := getAutoScalingGroups(ctx, client)

	if err != nil {
		t.Error(err)
		return
	}

	if len(list) != len(groupOutput) {
		t.Errorf("Expected %d ec2 changes, got %d", len(groupOutput), len(list))
		return
	}

	for i := range groupOutput {
		if *groupOutput[i].AutoScalingGroupName != *list[i].resource.AutoScalingGroupName {
			t.Errorf("Expected group with Name %s, got %s", *groupOutput[i].AutoScalingGroupName, *list[i].resource.AutoScalingGroupName)
			return
		}
	}
}

func TestGetASGTagValue(t *testing.T) {
	tagSet := []*autoscaling.TagDescription{
		{Key: aws.String("aKey"), Value: aws.String("aValue")},
		{Key: aws.String("bKey"), Value: aws.String("bValue")},
		{Key: aws.String("cKey"), Value: aws.String("cValue")},
	}

	actual := getASGTagValue(tagSet, "bKey")
	expected := "bValue"

	if *actual != expected {
		t.Errorf("expected %s, got %s", expected, *actual)
	}

	actual = getASGTagValue(tagSet, "doesnt-exists")
	if actual != nil {
		t.Errorf("expected nil value, got %s", *actual)
	}
}

func TestGetASGGroupChanges(t *testing.T) {

	chkTime := newWeekday(time.Monday, 12, 0)

	always, err := NewPeriod("0:0", "23:59", AllWeekdays())
	if err != nil {
		t.Error(err)
		return
	}

	never, err := NewPeriod("0:00", "0:00", []time.Weekday{})
	if err != nil {
		t.Error(err)
		return
	}

	tests := []struct {
		capacity  int64
		instances int
		suspended bool
		period    *Period
		schedule  string
		expected  ScheduledAction
	}{
		{1, 1, false, always, "n", NoopAction},
		{0, 0, false, always, "n", StartAction},
		{1, 1, false, never, "n", StopAction},
		{1, 0, false, always, "n", NoopAction}, // transitional state
		{0, 1, false, always, "n", NoopAction}, // transitional state
		{0, 0, false, always, "x", NoopAction}, // transitional state
		{0, 0, true, always, "n", NoopAction},  // has suspended processes
	}

	for i, test := range tests {
		group := &autoscaling.Group{
			AutoScalingGroupName: aws.String(fmt.Sprintf("asg-%d", i)),
			DesiredCapacity:      aws.Int64(test.capacity),
			MinSize:              aws.Int64(test.capacity),
		}
		for i := 0; i < test.instances; i++ {
			group.Instances = append(group.Instances, &autoscaling.Instance{})
		}
		if test.suspended {
			group.SuspendedProcesses = []*autoscaling.SuspendedProcess{{ProcessName: aws.String("Termination")}}
		}
		list := []*GroupSchedule{
			{resource: group, schedule: test.schedule},
		}
		schedule := NewSchedule("n")
		schedule.AddPeriod(time.Local.String(), test.period)
		schedules := Schedules{schedule}

		changes := getASGGroupChanges(list, chkTime, schedules)

		for _, change := range changes {
			if change.Action != test.expected {
				t.Errorf("case %d. expected %s, got %s", i+1, test.expected, change.Action)
			}
		}
	}
}

func TestPerformASGChanges(t *testing.T) {
	tests := []struct {
		action           ScheduledAction
		currentlyRunning int64
		expectedMinSize  int64
		expectedDesired  int64
	}{
		{StartAction, 0, 0, 1},
		{StopAction, 0, 1, 0},
	}

	for _, test := range tests {
		client := &mockAutoscalingClient{}

		changes := Changes{
			{ID: aws.String("n"), Action: test.action, currentMinSize: test.currentlyRunning, minSize: test.expectedDesired},
		}

		err := performASGChanges(client, changes)
		if err != nil {
			t.Error(err)
		}

		desiredList := client.updateAutoScalingGroupDesiredInput
		if len(desiredList) != len(changes) {
			t.Errorf("expected UpdateAutoScalingGroup to have been called %d, but got %d", len(changes), len(desiredList))
			continue
		}

		for i := range desiredList {
			if test.expectedDesired != desiredList[i] {
				t.Errorf("expected update to set expectedDesired to %d, got %d", test.expectedDesired, client.updateAutoScalingGroupDesiredInput[i])
			}
		}

		// we want to check if stop action will tag instances with pre-change minSize values
		if test.action == StopAction {
			if len(client.createOrUpdateTagsInput) == 0 {
				t.Errorf("expected createOrUpdateTags to be called on stop action")
				continue
			}
			tags := client.createOrUpdateTagsInput[0]
			currentlyRunning := fmt.Sprintf("%d", test.currentlyRunning)
			if *tags[0].Key != minSizeTag || *tags[0].Value != currentlyRunning {
				t.Errorf("expected asg to be tagged with %s=%s, got %s=%s", minSizeTag, currentlyRunning, *tags[0].Key, *tags[0].Value)
			}

		}
	}
}

func TestGetASGTagInt64(t *testing.T) {

	tests := []struct {
		searchTag  string
		defaultVal int64
		tagKey     string
		tagValue   string
		expected   int64
	}{
		{"valid", 99, "valid", "2", 2},
		{"valid", 99, "valid", "0", 0},
		{"no_key", 99, "valid", "2", 99},
		{"valid", 99, "valid", "not_int", 99},
	}

	for _, test := range tests {
		tags := []*autoscaling.TagDescription{
			{Key: aws.String(test.tagKey), Value: aws.String(test.tagValue)},
		}
		actual := getASGTagInt64(tags, test.searchTag, test.defaultVal)
		if test.expected != actual {
			t.Errorf("expected %d, got %d", test.expected, actual)
		}

	}

}

type mockAutoscalingClient struct {
	autoscalingiface.AutoScalingAPI
	describeAutoScalingGroupsResult    []*autoscaling.Group
	describeTagsResult                 []*autoscaling.TagDescription
	updateAutoScalingGroupDesiredInput []int64
	createOrUpdateTagsInput            [][]*autoscaling.Tag
}

func (m *mockAutoscalingClient) DescribeAutoScalingGroupsPagesWithContext(ctx aws.Context, input *autoscaling.DescribeAutoScalingGroupsInput, fnc func(*autoscaling.DescribeAutoScalingGroupsOutput, bool) bool, options ...request.Option) error {
	res := &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: m.describeAutoScalingGroupsResult,
	}
	fnc(res, true)
	return nil
}

func (m *mockAutoscalingClient) DescribeTagsPagesWithContext(ctx aws.Context, input *autoscaling.DescribeTagsInput, fnc func(*autoscaling.DescribeTagsOutput, bool) bool, options ...request.Option) error {
	res := &autoscaling.DescribeTagsOutput{
		Tags: m.describeTagsResult,
	}
	fnc(res, true)
	return nil
}

func (m *mockAutoscalingClient) UpdateAutoScalingGroup(i *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error) {

	m.updateAutoScalingGroupDesiredInput = append(m.updateAutoScalingGroupDesiredInput, *i.DesiredCapacity)
	return &autoscaling.UpdateAutoScalingGroupOutput{}, nil
}

func (m *mockAutoscalingClient) CreateOrUpdateTags(i *autoscaling.CreateOrUpdateTagsInput) (*autoscaling.CreateOrUpdateTagsOutput, error) {
	m.createOrUpdateTagsInput = append(m.createOrUpdateTagsInput, i.Tags)
	return &autoscaling.CreateOrUpdateTagsOutput{}, nil
}
