package main

import "os"

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

func (handler *ENVHandler) receive() {
	handler.messageID = "development"
	handler.messageBody = os.Getenv("TASK_PAYLOAD")
}

func (handler *ENVHandler) success() {}
