package main

import (
	"log"
	"os"
	"time"
)

// Tasque hello world
type Tasque struct {
	Handler    MessageHandler
	Executable *Executable
}

func main() {
	arguments := os.Args[1:]
	if len(os.Args) > 1 {
		tasque := Tasque{}
		tasque.Executable = &Executable{
			binary:    arguments[0],
			arguments: arguments[1:],
			timeout:   getTimeout(),
		}
		tasque.runWithTimeout()
	} else {
		log.Println("Expecting tasque to be run with an application")
		log.Println("Usage: tasque npm start")
	}
}

func (tasque *Tasque) getHandler() {
	var handler MessageHandler
	taskPayload := os.Getenv("TASK_PAYLOAD")
	if taskPayload != "" {
		handler = &ENVHandler{}
	} else {
		handler = &SQSHandler{}
	}
	tasque.Handler = handler
}

func (tasque *Tasque) runWithTimeout() {
	tasque.getHandler()
	// Commented code is for potential future "daemon"
	// var wg sync.WaitGroup
	// for i := 0; i < 5; i++ {
	// 	wg.Add(1)
	// 	go func() {
	// 		defer wg.Done()
	// 		for i := 0; i < 5; i++ {
	tasque.Executable.execute(tasque.Handler)
	// 		}
	// 	}()
	// }
	// wg.Wait()
}

func getTimeout() time.Duration {
	taskTimeout := os.Getenv("TASK_TIMEOUT")
	if taskTimeout == "" {
		log.Println("Default timeout: 30s")
		timeout, _ := time.ParseDuration("30s")
		return timeout
	}
	timeout, err := time.ParseDuration(taskTimeout)
	if err != nil {
		log.Println(err.Error())
		os.Exit(1)
		return time.Duration(0)
	}
	return timeout
}
