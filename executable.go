package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
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
		executable.executableTimeoutHelper(handler)
	}
}

func (executable *Executable) executableTimeoutHelper(handler MessageHandler) {
	ch := make(chan error)
	go func() {
		ch <- executionHelper(executable.binary, executable.arguments, handler.body(), handler.id())
	}()
	select {
	case err := <-ch:
		if err != nil {
			log.Printf("E: %s %s", executable.binary, err.Error())
			handler.failure(err)
		} else {
			log.Printf("I: %s finished successfully", executable.binary)
			handler.success()
		}
	case <-time.After(executable.timeout):
		log.Printf("E: %s timed out after %f seconds", executable.binary, executable.timeout.Seconds())
	}
}

func inputPipe(pipe io.WriteCloser, inputString *string, wg *sync.WaitGroup, e *error) {
	wg.Add(1)
	go func() {
		io.WriteString(pipe, *inputString)
		pipe.Close()
		wg.Done()
	}()
}

func outputPipe(pipe io.ReadCloser, annotation string, wg *sync.WaitGroup, e *error) {
	wg.Add(1)
	go func() {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, pipe); err == nil {
			log.Printf("%s %s\n", annotation, string(buf.Bytes()))
		} else {
			*e = err
		}
		wg.Done()
	}()
}

func executionHelper(binary string, executableArguments []string, messageBody *string, messageID *string) error {
	var exitCode int
	var err error
	var stdinPipe io.WriteCloser
	var stdoutPipe io.ReadCloser
	var stderrPipe io.ReadCloser

	environ := os.Environ()
	environ = append(environ, fmt.Sprintf("TASK_PAYLOAD=%s", *messageBody))
	environ = append(environ, fmt.Sprintf("TASK_ID=%s", *messageID))
	command := exec.Command(binary, executableArguments...)
	command.Env = environ

	if messageBody != nil {
		if stdinPipe, err = command.StdinPipe(); err != nil {
			return err
		}
	}
	if stdoutPipe, err = command.StdoutPipe(); err != nil {
		return err
	}
	if stderrPipe, err = command.StderrPipe(); err != nil {
		return err
	}

	if err = command.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	inputPipe(stdinPipe, messageBody, &wg, &err)
	outputPipe(stderrPipe, fmt.Sprintf("%s %s", *messageID, "!"), &wg, &err)
	outputPipe(stdoutPipe, fmt.Sprintf("%s", *messageID), &wg, &err)
	wg.Wait()
	if err != nil {
		return err
	}

	if err = command.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.Sys().(syscall.WaitStatus).ExitStatus()
			log.Printf("An error occured (%s %d)\n", binary, exitCode)
			log.Println(err)
		} else {
			return err
		}
	}

	return nil
}
