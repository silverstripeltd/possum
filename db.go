package possum

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
)

func DoDB(ctx context.Context, client rdsiface.RDSAPI, ts time.Time, schedules Schedules) (Changes, error) {
	instances, err := getDBInstances(ctx, client)
	if err != nil {
		return nil, err
	}
	changes := getDBInstanceChanges(instances, ts, schedules)
	err = performDBInstanceChanges(client, changes)
	return changes, err
}

type dbInstanceSchedule struct {
	resource *rds.DBInstance
	schedule string
}

func getDBInstances(ctx context.Context, client rdsiface.RDSAPI) ([]*dbInstanceSchedule, error) {

	var list []*dbInstanceSchedule

	var instances []*rds.DBInstance
	err := client.DescribeDBInstancesPagesWithContext(
		ctx,
		&rds.DescribeDBInstancesInput{},
		func(page *rds.DescribeDBInstancesOutput, lastPage bool) bool {
			for _, group := range page.DBInstances {
				instances = append(instances, group)
			}
			return true
		},
	)
	if err != nil {
		return list, err
	}

	for _, instance := range instances {
		res, err := client.ListTagsForResourceWithContext(
			ctx,
			&rds.ListTagsForResourceInput{
				ResourceName: instance.DBInstanceArn,
			},
		)
		if err != nil {
			return list, err
		}

		if schedule := getRDSTagValue(res.TagList, scheduleTag); schedule != nil {
			list = append(list, &dbInstanceSchedule{
				resource: instance,
				schedule: *schedule,
			})
		}
	}

	return list, nil
}

// @todo extend the schemas with the db maintenance window
func getDBInstanceChanges(list []*dbInstanceSchedule, ts time.Time, schedules Schedules) Changes {
	const runningState = "available"
	const stoppedState = "stopped"

	var changes Changes

	for _, a := range list {

		dbInstance := a.resource

		// skip db list that are in a transitional state
		if *dbInstance.DBInstanceStatus != runningState && *dbInstance.DBInstanceStatus != stoppedState {
			continue
		}

		effectiveSchedule := schedules.Find(a.schedule)
		if effectiveSchedule == nil {
			continue
		}

		isRunning := *dbInstance.DBInstanceStatus == runningState
		act := effectiveSchedule.Action(ts, isRunning)

		if act == NoopAction {
			continue
		}

		changes = append(changes, Change{
			ID:     dbInstance.DBInstanceIdentifier,
			Name:   *dbInstance.DBInstanceIdentifier,
			Action: act,
			Type:   "rds",
		})
	}
	return changes
}

func performDBInstanceChanges(client rdsiface.RDSAPI, list Changes) error {
	for _, a := range list {
		switch a.Action {
		case StartAction:
			_, err := client.StartDBInstance(&rds.StartDBInstanceInput{
				DBInstanceIdentifier: a.ID,
			})
			if err != nil {
				return err
			}
		case StopAction:
			_, err := client.StopDBInstance(&rds.StopDBInstanceInput{
				DBInstanceIdentifier: a.ID,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// helper to get a specific value out of rds tags
func getRDSTagValue(tags []*rds.Tag, keyName string) *string {
	for _, tag := range tags {
		if *tag.Key == keyName {
			return tag.Value
		}
	}
	return nil
}
