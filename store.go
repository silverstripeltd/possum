package possum

import (
	"encoding/json"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
)

func PutSchedules(client dynamodbiface.DynamoDBAPI, tableName string, schedules Schedules) error {
	b, err := json.Marshal(schedules)
	if err != nil {
		return err
	}
	item := make(map[string]*dynamodb.AttributeValue)
	item["id"] = &dynamodb.AttributeValue{S: aws.String("schedules")}
	item["content"] = &dynamodb.AttributeValue{S: aws.String(string(b))}
	_, err = client.PutItem(&dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item:      item,
		ReturnConsumedCapacity: aws.String("TOTAL"),
	})
	return err
}

func GetSchedules(client dynamodbiface.DynamoDBAPI, tableName string) (Schedules, error) {

	key := make(map[string]*dynamodb.AttributeValue)
	key["id"] = &dynamodb.AttributeValue{S: aws.String("schedules")}

	res, err := client.GetItem(&dynamodb.GetItemInput{
		ProjectionExpression: aws.String("content"),
		TableName:            aws.String(tableName),
		Key:                  key,
		ReturnConsumedCapacity: aws.String("TOTAL"),
	})
	if err != nil {
		return nil, err
	}

	content := res.Item["content"].S
	if content == nil {
		return nil, nil
	}

	var v Schedules
	err = json.Unmarshal([]byte(*content), &v)
	return v, err
}
