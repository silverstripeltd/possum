package main

import (
	"fmt"
	"os"
	"time"

	"encoding/json"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/silverstripeltd/possum"
)

func main() {
	err := _main()
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		os.Exit(1)
	}
}

func _main() error {
	schedule := possum.NewSchedule("OfficeHours")
	officeHourPeriod, err := possum.NewPeriod("8:00", "19:00", []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday})
	if err != nil {
		return err
	}
	if err = schedule.AddPeriod("Pacific/Auckland", officeHourPeriod); err != nil {
		return err
	}
	schedules := possum.Schedules{schedule}

	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String("ap-southeast-2")}))
	client := dynamodb.New(sess)
	//tableName := aws.String(os.Getenv("CONFIG_TABLE"))
	tableName := "lambda-possum-prod-ConfigTable-1S735A37BBT16"

	if err := possum.PutSchedules(client, tableName, schedules); err != nil {
		return err
	}

	out, err := possum.GetSchedules(client, tableName)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(out, "", "\t")
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", b)
	return nil
}
