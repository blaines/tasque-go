package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

// Executable hello world
type Executable struct {
	binary    string
	arguments []string
	stdin     bufio.Scanner
	stdout    bufio.Scanner
	stderr    bufio.Scanner
	timeout   time.Duration
}

func (executable *Executable) execute(handler MessageHandler) {
	handler.initialize()
	if handler.receive() {
		executionHelper(executable.binary, executable.arguments, handler.body(), handler.id(), executable.timeout)
		handler.success()
	}
}

func executionHelper(binary string, executableArguments []string, messageBody *string, messageID *string, timeout time.Duration) {
	env := os.Environ()
	env = append(env, fmt.Sprintf("TASK_PAYLOAD=%s", *messageBody))
	env = append(env, fmt.Sprintf("TASK_ID=%s", *messageID))

	command := exec.Command(binary, executableArguments...)
	command.Env = env

	outputBufferOut, err := command.StdoutPipe()
	if err != nil {
		log.Println("E: Error creating StdoutPipe for Cmd", err)
		os.Exit(1)
	}

	outputBufferErr, err := command.StderrPipe()
	if err != nil {
		log.Println("E: Error creating StderrPipe for Cmd", err)
		os.Exit(1)
	}

	scannerOut := bufio.NewScanner(outputBufferOut)
	scannerErr := bufio.NewScanner(outputBufferErr)

	go func() {
		for scannerOut.Scan() {
			log.Printf("%s I: %s\n", *messageID, scannerOut.Text())
		}
	}()

	go func() {
		for scannerErr.Scan() {
			log.Printf("%s E: %s\n", *messageID, scannerErr.Text())
		}
	}()

	err = command.Start()
	if err != nil {
		log.Println("E: Error starting Cmd", err)
		os.Exit(1)
	}

	ch := make(chan error)
	go func() { ch <- command.Wait() }()
	select {
	case err := <-ch:
		if err != nil {
			log.Printf("E: %s %s", binary, err.Error())
			os.Exit(1)
		} else {
			log.Printf("I: %s finished successfully", binary)
		}
	case <-time.After(timeout):
		log.Printf("E: %s timed out after %f seconds", binary, timeout.Seconds())
	}

}
