package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sfn"
)

// SFNHandler hello world
type SFNHandler struct {
	client      sfn.SFN
	messageBody string
	taskToken   string
	activityARN string
	awsRegion   string
}

// SFNClient hello world
type SFNClient struct {
	activityARN string
	awsRegion   string
	sfnClient   sfn.SFN
}

func (handler *SFNHandler) id() *string {
	// There's no real use for the full token
	return &handler.taskToken[0:32]
}

func (handler *SFNHandler) body() *string {
	return &handler.messageBody
}

func (handler *SFNHandler) initialize() {
	handler.newClient(sfn.New(session.New(), &aws.Config{
		MaxRetries: aws.Int(30),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}))
	handler.activityARN = os.Getenv("TASK_ACTIVITY_ARN")
}

func (handler *SFNHandler) newClient(client sfn.SFN) {
	val = sfn.New(p, cfgs)
	sfn.SFN
	handler.client = client
}

func (handler *SFNHandler) receive() bool {
	getActivityTaskParams := &sfn.ReceiveMessageInput{
		ActivityArn: aws.String(handler.activityARN),
		WorkerName:  aws.String("WorkerDemo"),
	}
	receiveMessageResponse, receiveMessageError := handler.client.GetActivityTask(getActivityTaskParams)

	if receiveMessageError != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		log.Println("E: ", receiveMessageError.Error())
		return false
	}

	handler.messageBody = *receiveMessageResponse.Input
	handler.taskToken = *receiveMessageResponse.TaskToken

	writeFileError := ioutil.WriteFile("payload.json", []byte(handler.messageBody), 0644)
	if writeFileError != nil {
		panic(writeFileError)
	}
	return true
}

func (handler *SFNHandler) success() {
	sendTaskSuccessParams := &sfn.SendTaskSuccessInput{
		Output:    aws.String(handler.messageBody),
		TaskToken: aws.String(handler.taskToken),
	}
	_, deleteMessageError := handler.client.SendTaskSuccess(sendTaskSuccessParams)

	if deleteMessageError != nil {
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				log.Fatalf("AWS SDK Error: %s %s", awsErr.Code(), awsErr.Message())
			}
		}
		return
	}
}

func (handler *SFNHandler) failure() {
	sendTaskFailureParams := &sfn.SendTaskFailureInput{
		TaskToken: aws.String(handler.taskToken),
		Cause:     aws.String("TBD"),
		Error:     aws.String("TBD"),
	}
	_, deleteMessageError := handler.client.SendTaskFailure(sendTaskFailureParams)

	if deleteMessageError != nil {
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				log.Fatalf("AWS SDK Error: %s %s", awsErr.Code(), awsErr.Message())
			}
		}
		return
	}
}

func (handler *SFNHandler) heartbeat() {
	sendTaskHeartbeatParams := &sfn.SendTaskHeartbeatInput{
		TaskToken: aws.String(handler.taskToken),
	}
	_, deleteMessageError := handler.client.SendTaskHeartbeat(sendTaskHeartbeatParams)

	if deleteMessageError != nil {
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				log.Fatalf("AWS SDK Error: %s %s", awsErr.Code(), awsErr.Message())
			}
		}
		return
	}
}
