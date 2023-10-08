sam:
	GOOS=linux GOARCH=amd64 go build -o build/possum
	aws-sam-local local generate-event schedule | aws-sam-local local invoke

test_day:
	GOOS=linux GOARCH=amd64 go build -o build/possum
	aws-sam-local local invoke --event event_day.json
test_night:
	GOOS=linux GOARCH=amd64 go build -o build/possum
	aws-sam-local local invoke --event event_night.json

test:
	go test

package:
	GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -tags lambda.norpc -o build/bootstrap main.go
	aws cloudformation package --template-file template.yml --s3-bucket playpen-rainforest-deploy --s3-prefix lambda-possum-prod --output-template-file ./build/packaged-template.yml

deploy:
	aws cloudformation deploy --template-file ./build/packaged-template.yml --capabilities CAPABILITY_IAM --stack-name lambda-possum-prod

start_instance:
	 aws ec2 run-instances --image-id ami-68a26a0a --count 1 --instance-type t2.micro --instance-market-options MarketType=spot --tag-specifications 'ResourceType=instance,Tags=[{Key=Schedule,Value=OfficeHours}]'
