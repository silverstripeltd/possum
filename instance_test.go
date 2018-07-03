package possum

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

func TestGetEC2Instances(t *testing.T) {

	output := []*ec2.Instance{
		{InstanceId: aws.String("i-abcdefgh1"), Tags: makeEc2Tags(scheduleTag, "SomeSchedule")},
		{InstanceId: aws.String("i-abcdefgh2"), Tags: makeEc2Tags(scheduleTag, "AnotherSchedule")},
		{InstanceId: aws.String("i-abcdefgh3")}, // no schedueTag, should not show up
		{InstanceId: aws.String("i-abcdefgh3"), Tags: makeEc2Tags(scheduleTag, "SomeSchedule", autoScalingGroupTag, "some-asg-group")},
	}

	expectedInstances := len(output) - 2

	client := &mockEC2Client{describeInstanceResult: output}

	ctx := context.Background()
	var changes []*instanceSchedule
	changes, err := getInstances(ctx, client)
	if err != nil {
		t.Error(err)
		return
	}

	if len(changes) != expectedInstances {
		t.Errorf("Expected %d ec2 changes, got %d", expectedInstances, len(changes))
		return
	}

	for i := 0; i < expectedInstances; i++ {
		if *changes[i].resource.InstanceId != *output[i].InstanceId {
			t.Errorf("Expected changes with ID %s, got %s", *output[i].InstanceId, *changes[i].resource.InstanceId)
			return
		}
	}
}

func TestGetInstanceChanges(t *testing.T) {

	alwaysSchedule := NewSchedule("AlwaysSchedule")
	p, err := NewPeriod("00:00", "23:59", []time.Weekday{})
	if err != nil {
		t.Error(err)
		return
	}
	alwaysSchedule.AddPeriod(time.Local.String(), p)

	neverSchedule := NewSchedule("NeverSchedule")
	p, err = NewPeriod("00:00", "00:01", []time.Weekday{time.Sunday})
	if err != nil {
		t.Error(err)
		return
	}
	neverSchedule.AddPeriod(time.Local.String(), p)
	schedules := Schedules{alwaysSchedule, neverSchedule}
	chkTime := newWeekday(time.Monday, 12, 0)

	tests := []struct {
		changes  []*instanceSchedule
		expected ScheduledAction
	}{
		{makeInstanceSchedule("a", alwaysSchedule.Name, ec2.InstanceStateNameStopped, false), StartAction},
		{makeInstanceSchedule("b", alwaysSchedule.Name, ec2.InstanceStateNameRunning, false), NoopAction},
		{makeInstanceSchedule("c", neverSchedule.Name, ec2.InstanceStateNameStopped, false), NoopAction},
		{makeInstanceSchedule("d", neverSchedule.Name, ec2.InstanceStateNameRunning, false), StopAction},
		{makeInstanceSchedule("e", neverSchedule.Name, ec2.InstanceStateNameTerminated, false), NoopAction},
		{makeInstanceSchedule("f", alwaysSchedule.Name, ec2.InstanceStateNameStopped, true), NoopAction},
		{makeInstanceSchedule("g", "dont_exists", ec2.InstanceStateNameStopped, false), NoopAction},
	}

	for _, test := range tests {
		changes := getInstanceChanges(test.changes, chkTime, schedules)
		for _, change := range changes {
			if change.Action != test.expected {
				t.Errorf("Expected change '%s', but got change '%s'", test.expected, change.Action)
			}
		}
	}
}

func TestGetEC2TagValue(t *testing.T) {
	tagSet := makeEc2Tags("aKey", "aValue", "bKey", "bValue", "cKey", "cValue")

	actual := getEC2TagValue(tagSet, "bKey")
	expected := "bValue"

	if *actual != expected {
		t.Errorf("expected %s, got %s", expected, *actual)
	}

	actual = getEC2TagValue(tagSet, "doesnt-exists")
	if actual != nil {
		t.Errorf("expected nil value, got %s", *actual)
	}
}

func TestChangeInstanceState(t *testing.T) {

	changes := Changes{
		{ID: aws.String("i-1"), Action: StopAction},
		{ID: aws.String("i-2"), Action: StartAction},
		{ID: aws.String("i-3"), Action: StopAction},
		{ID: aws.String("i-4"), Action: StartAction},
		{ID: aws.String("i-5"), Action: StartAction},
	}

	client := &mockEC2Client{}
	ctx := context.Background()
	err := performInstanceChanges(ctx, client, changes)
	if err != nil {
		t.Error(err)
	}

	if len(client.stopInstances) != 2 {
		t.Errorf("Expected 2 stopped instances, got %d", len(client.stopInstances))
	}

	if len(client.startInstances) != 3 {
		t.Errorf("Expected 3 started instances, got %d", len(client.startInstances))
	}

}

type mockEC2Client struct {
	ec2iface.EC2API
	describeInstanceResult []*ec2.Instance
	startInstances         []*string
	stopInstances          []*string
}

func (m *mockEC2Client) DescribeInstancesPagesWithContext(ctx aws.Context, input *ec2.DescribeInstancesInput, fnc func(*ec2.DescribeInstancesOutput, bool) bool, options ...request.Option) error {
	res := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{Instances: m.describeInstanceResult},
		},
	}
	fnc(res, true)
	return nil
}

func (m *mockEC2Client) StartInstancesWithContext(ctx aws.Context, input *ec2.StartInstancesInput, options ...request.Option) (*ec2.StartInstancesOutput, error) {
	m.startInstances = input.InstanceIds
	return &ec2.StartInstancesOutput{}, nil
}

func (m *mockEC2Client) StopInstancesWithContext(ctx aws.Context, input *ec2.StopInstancesInput, options ...request.Option) (*ec2.StopInstancesOutput, error) {
	m.stopInstances = input.InstanceIds
	return &ec2.StopInstancesOutput{}, nil
}

func makeInstanceSchedule(id string, scheduleName string, stateName string, isSpot bool) []*instanceSchedule {
	instance := &ec2.Instance{
		InstanceId: aws.String(id),
		State: &ec2.InstanceState{
			Name: aws.String(stateName),
		},
		Tags: []*ec2.Tag{
			{Key: aws.String(scheduleTag), Value: aws.String(scheduleName)},
		},
	}
	if isSpot {
		instance.InstanceLifecycle = aws.String("spot")
	}

	return []*instanceSchedule{
		{resource: instance, schedule: scheduleName},
	}
}

func makeEc2Tags(val ...string) []*ec2.Tag {

	var res []*ec2.Tag
	for i := 0; i < len(val); i += 2 {
		res = append(res, &ec2.Tag{Key: aws.String(val[i]), Value: aws.String(val[i+1])})
	}

	return res
}
