package main

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
)

// SQSHandler hello world
type SQSHandler struct {
	client        sqsiface.SQSAPI
	messageID     string
	messageBody   string
	receiptHandle string
	queueURL      string
	awsRegion     string
}

// SQSClient hello world
type SQSClient struct {
	queueURL  string
	awsRegion string
	sqsClient sqsiface.SQSAPI
}

func (handler *SQSHandler) id() *string {
	return &handler.messageID
}

func (handler *SQSHandler) body() *string {
	return &handler.messageBody
}

func (handler *SQSHandler) initialize() {
	handler.newClient(sqs.New(session.New()))
	handler.queueURL = os.Getenv("TASK_QUEUE_URL")
}

func (handler *SQSHandler) newClient(client sqsiface.SQSAPI) {
	handler.client = client
}

func (handler *SQSHandler) receive() {
	receiveMessageParams := &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(handler.queueURL),
		MaxNumberOfMessages: aws.Int64(1),
		WaitTimeSeconds:     aws.Int64(20),
	}
	receiveMessageResponse, receiveMessageError := handler.client.ReceiveMessage(receiveMessageParams)

	if receiveMessageError != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		log.Println("E: ", receiveMessageError.Error())
		return
	}

	handler.messageBody = *receiveMessageResponse.Messages[0].Body
	handler.messageID = *receiveMessageResponse.Messages[0].MessageId
	handler.receiptHandle = *receiveMessageResponse.Messages[0].ReceiptHandle

	writeFileError := ioutil.WriteFile("payload.json", []byte(handler.messageBody), 0644)
	if writeFileError != nil {
		panic(writeFileError)
	}
}

func (handler *SQSHandler) success() {
	deleteMessageParams := &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(handler.queueURL),
		ReceiptHandle: aws.String(handler.receiptHandle),
	}
	_, deleteMessageError := handler.client.DeleteMessage(deleteMessageParams)

	if deleteMessageError != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		log.Println(deleteMessageError.Error())
		return
	}
}
