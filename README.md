# Possum

Automatically schedule AWS resources running time

## Summary

By tagging AWS resources with a schedule, possum will ensure that they are in the state that the schedule supports.
It was build mainly as a cost saving technique for my personal development servers and was inspired by AWS [https://aws.amazon.com/answers/infrastructure-management/instance-scheduler/] but that
solution did not support auto scaling groups and some other minor things I wanted to have.

It can optionally send change notifications to a slack room.

## Schedule definition



## Running cost

this highly depends on how long the lambda function is running, and the run time is dependent how many resources an
AWS account has

Memory size ~ 32mb at peak


## Technical details

Possum runs as a go1.X lambda function triggered by a X min schedule via Cloudwatch Events.

Can only start and stop in the deployed account



## Start and stopping actions

### EC2 instances

_note_: possum cannot start or stop reserved instances

### RDS database instances ()

_note_: only tested on DBInstances, not DBClusters and other special database types

### Auto scaling groups

Stops auto scaling groups by zeroing out the min size and the desired capacity, during this step it also tags the
auto scaling group with the current min size and desired capacity so that it can on start reset those values

Start tries to find the previous min size and desired capacity from tags set on the group by the stop stage, if those
values cannot be parsed it sets the min size and desired capacity to 1.



## Building and Deploying to Lambda

The `root` directory of this project contains the `possum` application. However the execution via Lambda is controlled via a separate application stored in `cmd/lambda`.

You don't need to build the `possum` application itself as it is included in the `lambda` function of this repository.

### Building package

In order to deploy your changes, you will need to package the deployment using `aws cloudformation package`. To simplify this process, a `Makefile` has been created to perform this action for you.

```
cd cmd/lambda

aws-vault exec <account> -- make package
```

This command will build the golang `lambda` application, combine it with the `template.yml` Cloudformation, and package it together to be consumed by Cloudformation.

### Deploying function

Once you have your package build, you can push out your changes using `aws cloudformation deploy` functionality. To simplify this process, the `Makefile` contains a `deploy` function that does this for you.

```
cd cmd/lambda

aws-vault exec <account> -- make deploy
```

This command will tell Cloudformation to deploy the package created using `make package`.