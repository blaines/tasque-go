package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sfn"
	"github.com/blaines/tasque-go/result"
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
	token := handler.taskToken[0:32]
	return &token
}

func (handler *SFNHandler) body() *string {
	return &handler.messageBody
}

func (handler *SFNHandler) initialize() {
	log.Printf("Configuring handler. activityARN:%s", handler.activityARN)
	sess, err := session.NewSession(&aws.Config{Region: aws.String(strings.Split(handler.activityARN, ":")[3])})
	if err != nil {
		fmt.Println("failed to create session,", err)
		panic("failed to create session")
	}

	// client := sfn.New(sess, &aws.Config{
	// 	MaxRetries: aws.Int(30),
	// 	HTTPClient: &http.Client{
	// 		Timeout: 30 * time.Second,
	// 	},
	// })
	client := sfn.New(sess)
	handler.newClient(*client)
}

func (handler *SFNHandler) newClient(client sfn.SFN) {
	handler.client = client
}

func (handler *SFNHandler) receive() bool {
	for {
		log.Printf("Waiting for SFN activity data from %s", handler.activityARN)
		hostname, _ := os.Hostname()
		getActivityTaskParams := &sfn.GetActivityTaskInput{
			ActivityArn: aws.String(handler.activityARN),
			WorkerName:  aws.String(hostname),
		}
		receiveMessageResponse, receiveMessageError := handler.client.GetActivityTask(getActivityTaskParams)

		if receiveMessageError != nil {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			log.Fatal("E: ", receiveMessageError.Error())
			return false
		}

		if receiveMessageResponse.TaskToken != nil {
			handler.messageBody = *receiveMessageResponse.Input
			handler.taskToken = *receiveMessageResponse.TaskToken

			writeFileError := ioutil.WriteFile("payload.json", []byte(handler.messageBody), 0644)
			if writeFileError != nil {
				panic(writeFileError)
			}
			return true
		}
	}
}

func (handler *SFNHandler) success() {
	sendTaskSuccessParams := &sfn.SendTaskSuccessInput{
		Output:    aws.String(handler.messageBody),
		TaskToken: aws.String(handler.taskToken),
	}
	_, deleteMessageError := handler.client.SendTaskSuccess(sendTaskSuccessParams)

	if deleteMessageError != nil {
		return
	}
}

func (handler *SFNHandler) failure(err result.Result) {
	sendTaskFailureParams := &sfn.SendTaskFailureInput{
		TaskToken: aws.String(handler.taskToken),
		Error:     aws.String(err.Error),
		Cause:     aws.String(err.Message()),
	}
	_, deleteMessageError := handler.client.SendTaskFailure(sendTaskFailureParams)

	if deleteMessageError != nil {
		log.Printf("Couldn't send task failure %+v", deleteMessageError)
		return
	}
}

func (handler *SFNHandler) heartbeat() {
	sendTaskHeartbeatParams := &sfn.SendTaskHeartbeatInput{
		TaskToken: aws.String(handler.taskToken),
	}
	_, deleteMessageError := handler.client.SendTaskHeartbeat(sendTaskHeartbeatParams)

	if deleteMessageError != nil {
		return
	}
}
