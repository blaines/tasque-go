package main

import (
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
)

// Tasque hello world
type Tasque struct {
	Handler    MessageHandler
	Executable ExecutableInterface
}

// Support three modes of operation
// -e environment variable TASK_PAYLOAD
// -i standard input
// -f file output
// TODO:
// func main() {
// 	c := cli.NewCLI("app", "1.0.0")
// 	c.Args = os.Args[1:]
// 	c.Commands = map[string]cli.CommandFactory{
// 	// "foo": fooCommandFactory,
// 	// "bar": barCommandFactory,
// 	}
//
// 	exitStatus, err := c.Run()
// 	if err != nil {
// 		log.Println(err)
// 	}
//
// 	os.Exit(exitStatus)
// }

func main() {
	var ecsTaskDefinition *string
	var overridePayloadKey *string
	var overrideContainerName *string
	var dockerEndpointPath string

	isDocker := os.Getenv("DOCKER")
	if isDocker != "" {
		log.Println("Docker mode")
		// Docker Mode
		// ECS_TASK_DEFINITION
		ecsTaskDefinition = aws.String(os.Getenv("ECS_TASK_DEFINITION"))
		if *ecsTaskDefinition == "" {
			panic("Environment variable ECS_TASK_DEFINITION not set")
		}
		// ECS_CONTAINER_NAME
		overrideContainerName = aws.String(os.Getenv("ECS_CONTAINER_NAME"))
		if *overrideContainerName == "" {
			panic("Environment variable ECS_CONTAINER_NAME not set")
		}
		// DOCKER_ENDPOINT
		dockerEndpointPath = os.Getenv("DOCKER_ENDPOINT")
		if dockerEndpointPath == "" {
			dockerEndpointPath = "unix:///var/run/docker.sock"
		}
		// OVERRIDE_PAYLOAD_KEY
		overridePayloadKey = aws.String("TASK_PAYLOAD")
		tasque := Tasque{}
		d := &Docker{}
		d.connect(dockerEndpointPath)
		tasque.Executable = &AWSECS{
			docker:                d,
			ecsTaskDefinition:     ecsTaskDefinition,
			overrideContainerName: overrideContainerName,
			overridePayloadKey:    overridePayloadKey,
			timeout:               getTimeout(),
		}
		tasque.runWithTimeout()
	} else {
		// CLI Mode
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
}

func (tasque *Tasque) getHandler() {
	var handler MessageHandler
	taskPayload := os.Getenv("TASK_PAYLOAD")
	taskQueueURL := os.Getenv("TASK_QUEUE_URL")
	activityARN := os.Getenv("TASK_ACTIVITY_ARN")
	if taskPayload != "" {
		handler = &ENVHandler{}
	} else if taskQueueURL != "" {
		handler = &SQSHandler{}
	} else if activityARN != "" {
		handler = &SFNHandler{
			activityARN: activityARN,
		}
	} else {
		panic("No handler")
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
