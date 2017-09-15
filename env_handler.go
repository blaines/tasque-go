package main

import "os"
import "github.com/blaines/tasque-go/result"

// ENVHandler hello world
type ENVHandler struct {
	messageID, messageBody string
}

func (handler *ENVHandler) id() *string {
	return &handler.messageID
}

func (handler *ENVHandler) body() *string {
	return &handler.messageBody
}

func (handler *ENVHandler) initialize() {}

func (handler *ENVHandler) receive() bool {
	handler.messageID = "development"
	handler.messageBody = os.Getenv("TASK_PAYLOAD")
	return true
}

func (handler *ENVHandler) success()                  {}
func (handler *ENVHandler) failure(err result.Result) {}
func (handler *ENVHandler) heartbeat()                {}
