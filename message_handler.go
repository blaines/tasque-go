package main

// MessageHandler hello world
type MessageHandler interface {
	id() *string
	body() *string
	initialize()
	receive() bool
	success()
	failure(err error)
	heartbeat()
}
