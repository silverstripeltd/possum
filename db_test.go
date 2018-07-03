package possum

import (
	"context"
	"testing"

	"time"

	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
)

func TestGetDBInstances(t *testing.T) {

	tests := []struct {
		instance  *rds.DBInstance
		hasSchema bool
	}{
		{&rds.DBInstance{DBInstanceStatus: aws.String("available"), DBInstanceIdentifier: aws.String("a")}, true},
		{&rds.DBInstance{DBInstanceStatus: aws.String("available"), DBInstanceIdentifier: aws.String("b")}, true},
		{&rds.DBInstance{DBInstanceStatus: aws.String("available"), DBInstanceIdentifier: aws.String("c")}, false},
		{nil, false},
	}

	for _, test := range tests {
		client := &mockRDSClient{}
		if test.instance != nil {
			client.describeDBInstancesResult = []*rds.DBInstance{test.instance}
		}
		if test.hasSchema {
			client.listTagsForResource = []*rds.Tag{{Key: aws.String(scheduleTag), Value: aws.String("value")}}
		}

		list, err := getDBInstances(context.Background(), client)
		if err != nil {
			t.Error(err)
			continue
		}

		if test.instance == nil {
			if len(list) > 0 {
				t.Errorf("Did not expect to get non empty list on empty search result")
			}
			continue
		}

		if !test.hasSchema {
			if len(list) > 0 {
				t.Errorf("Didnt expect schemaless dbinstances to be in list")
				continue
			}
			continue
		}

		if test.instance.DBInstanceIdentifier != list[0].resource.DBInstanceIdentifier {
			t.Errorf("expected %s, got %s", *test.instance.DBInstanceIdentifier, *list[0].resource.DBInstanceIdentifier)
		}
		//for i := range output {
		//	if *list[i].resource.DBName != *output[i].DBName {
		//		t.Errorf("Expected db changes with ID %s, got %s", *output[i].DBName, *list[i].resource.DBName)
		//		return
		//	}
		//}
	}
}

func TestGetDBInstanceChanges(t *testing.T) {

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
		status   string
		period   *Period
		schedule string
		expected ScheduledAction
	}{
		{"available", always, "n", NoopAction},
		{"stopping", always, "n", NoopAction},
		{"stopped", always, "n", StartAction},
		{"available", never, "n", StopAction},
		{"stopping", never, "n", NoopAction},
		{"stopped", never, "n", NoopAction},
		{"stopped", always, "x", NoopAction},
	}

	for i, test := range tests {
		schedule := NewSchedule("n")
		schedule.AddPeriod(time.Local.String(), test.period)
		schedules := Schedules{schedule}

		action := &dbInstanceSchedule{
			resource: &rds.DBInstance{
				DBInstanceIdentifier: aws.String(fmt.Sprintf("test-%d", i)),
				DBInstanceStatus:     aws.String(test.status),
			},
			schedule: test.schedule,
		}
		list := []*dbInstanceSchedule{action}
		changes := getDBInstanceChanges(list, chkTime, schedules)
		for _, change := range changes {
			if change.Action != test.expected {
				t.Errorf("case %d. expected %s, got %s", i+1, test.expected, change.Action)
			}
		}
	}
}

func TestPerformDBInstanceChanges(t *testing.T) {

	tests := []struct {
		action          ScheduledAction
		expectedStarted int
		expectedStopped int
	}{
		{StartAction, 1, 0},
		{StopAction, 0, 1},
	}

	for i, test := range tests {
		client := &mockRDSClient{}
		change := Change{
			ID:     aws.String(fmt.Sprintf("test-%d", i)),
			Action: test.action,
		}
		changes := Changes{change}

		if err := performDBInstanceChanges(client, changes); err != nil {
			t.Error(err)
			return
		}
		if test.expectedStarted != client.startedInstances {
			t.Errorf("expected %d db instances started, got %d", test.expectedStarted, client.startedInstances)
		}
		if test.expectedStopped != client.stoppedInstances {
			t.Errorf("expected %d db instances stopped, got %d", test.expectedStopped, client.stoppedInstances)
		}

	}

}

type mockRDSClient struct {
	rdsiface.RDSAPI
	describeDBInstancesResult []*rds.DBInstance
	listTagsForResource       []*rds.Tag
	startedInstances          int
	stoppedInstances          int
}

func (m *mockRDSClient) DescribeDBInstancesPagesWithContext(ctx aws.Context, input *rds.DescribeDBInstancesInput, fnc func(*rds.DescribeDBInstancesOutput, bool) bool, options ...request.Option) error {
	res := &rds.DescribeDBInstancesOutput{
		DBInstances: m.describeDBInstancesResult,
	}
	fnc(res, true)
	return nil
}

func (m *mockRDSClient) ListTagsForResourceWithContext(ctx aws.Context, input *rds.ListTagsForResourceInput, options ...request.Option) (*rds.ListTagsForResourceOutput, error) {

	return &rds.ListTagsForResourceOutput{
		TagList: m.listTagsForResource,
	}, nil
}

func (m *mockRDSClient) StartDBInstance(*rds.StartDBInstanceInput) (*rds.StartDBInstanceOutput, error) {
	m.startedInstances += 1
	return &rds.StartDBInstanceOutput{}, nil
}

func (m *mockRDSClient) StopDBInstance(*rds.StopDBInstanceInput) (*rds.StopDBInstanceOutput, error) {
	m.stoppedInstances += 1
	return &rds.StopDBInstanceOutput{}, nil
}
