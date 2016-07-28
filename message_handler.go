package main

// MessageHandler hello world
type MessageHandler interface {
	id() *string
	body() *string
	initialize()
	receive()
	success()
}
