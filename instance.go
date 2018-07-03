package possum

import (
	"context"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

const autoScalingGroupTag = "aws:autoscaling:groupName"

func DoInstances(ctx context.Context, client ec2iface.EC2API, ts time.Time, schedules Schedules) (Changes, error) {
	instances, err := getInstances(ctx, client)
	if err != nil {
		return nil, err
	}
	changes := getInstanceChanges(instances, ts, schedules)
	err = performInstanceChanges(ctx, client, changes)
	return changes, err
}

type instanceSchedule struct {
	resource *ec2.Instance
	schedule string
}

func getInstances(ctx context.Context, client ec2iface.EC2API) ([]*instanceSchedule, error) {
	var list []*instanceSchedule
	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("tag-key"), Values: []*string{aws.String(scheduleTag)}},
		},
	}
	err := client.DescribeInstancesPagesWithContext(ctx, params, func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
		for _, reservation := range page.Reservations {
			for _, inst := range reservation.Instances {
				asgName := getEC2TagValue(inst.Tags, autoScalingGroupTag)
				if asgName != nil {
					continue
				}
				if schedule := getEC2TagValue(inst.Tags, scheduleTag); schedule != nil {
					list = append(list, &instanceSchedule{resource: inst, schedule: *schedule})

				}
			}
		}
		return true
	})

	return list, err
}

func getInstanceChanges(list []*instanceSchedule, ts time.Time, schedules Schedules) Changes {

	var changes Changes

	for _, a := range list {
		// skip instance that are in a transitional state
		if *a.resource.State.Name != ec2.InstanceStateNameStopped && *a.resource.State.Name != ec2.InstanceStateNameRunning {
			continue
		}

		// check if instance is a spot changes, they cannot be stopped by ordinary means
		if a.resource.InstanceLifecycle != nil && *a.resource.InstanceLifecycle == "spot" {
			log.Printf("INFO possum doesn't support schedules on spot instances like '%s'", *a.resource.InstanceId)
			continue
		}

		// Try to find the period, warn if it doesn't exist
		effectiveSchedule := schedules.Find(a.schedule)
		if effectiveSchedule == nil {
			log.Printf("INFO could not find schedule '%s' for '%s'", a.schedule, *getInstanceName(a.resource))
			continue
		}

		isRunning := *a.resource.State.Name == ec2.InstanceStateNameRunning
		action := effectiveSchedule.Action(ts, isRunning)
		if action == NoopAction {
			continue
		}

		changes = append(changes, Change{
			Name:   *getInstanceName(a.resource),
			ID:     a.resource.InstanceId,
			Action: action,
			Type:   "instance",
		})
	}
	return changes
}

func performInstanceChanges(ctx context.Context, client ec2iface.EC2API, list Changes) error {

	// Since ec2 instances can be started in bulk, we sort out the changes into a start and stop list
	var toStart []*string
	var toStop []*string
	for _, a := range list {
		switch a.Action {
		case StartAction:
			toStart = append(toStart, a.ID)
		case StopAction:
			toStop = append(toStop, a.ID)
		}
	}

	var err error
	if len(toStart) > 0 {
		_, err = client.StartInstancesWithContext(ctx, &ec2.StartInstancesInput{
			InstanceIds: toStart,
		})
	}
	if len(toStop) > 0 {
		_, err = client.StopInstancesWithContext(ctx, &ec2.StopInstancesInput{
			InstanceIds: toStop,
		})
	}
	return err
}

func getInstanceName(instance *ec2.Instance) *string {
	instanceName := getEC2TagValue(instance.Tags, "Name")
	if instanceName == nil {
		instanceName = instance.InstanceId
	}
	return instanceName
}

// helper to get a specific tag value out of ec2 resource tags
func getEC2TagValue(tags []*ec2.Tag, keyName string) *string {
	for _, tag := range tags {
		if *tag.Key == keyName {
			return tag.Value
		}
	}
	return nil
}
