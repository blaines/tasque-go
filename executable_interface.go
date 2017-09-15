package main

import "github.com/blaines/tasque-go/result"

// ExecutableInterface hello world
type ExecutableInterface interface {
	execute(handler MessageHandler)
	Result() result.Result
}
