package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/fsouza/go-dockerclient"
)

// AWSECS hello world
type AWSECS struct {
	ecsTaskDefinition     string
	overrideContainerName string
	overridePayloadKey    string
	timeout               time.Duration
	docker                Docker
}

// Docker hello world
type Docker struct {
	client   *docker.Client
	eventsCh chan *docker.APIEvents
}

func (executable *AWSECS) execute(handler MessageHandler) {
	handler.initialize()
	if handler.receive() {
		executable.executableTimeoutHelper(handler)
	}
}

func (executable *AWSECS) executableTimeoutHelper(handler MessageHandler) {
	ch := make(chan error)
	go func() {
		ch <- executionHelper(handler.body(), handler.id())
	}()
	select {
	case err := <-ch:
		if err != nil {
			log.Printf("E: %s %s", executable.binary, err.Error())
			handler.failure()
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

func executionHelper(messageBody *string, messageID *string) error {
	startECSContainer(messageBody, messageID)
	monitorDocker()
}

//  Task ARN is part of Docker labels...
//                 "com.amazonaws.ecs.task-arn": "arn:aws:ecs:us-west-2:770136283015:task/d8e65fde-65dc-4e46-aeaa-8b2b33215349",

func startECSContainer(messageBody *string, messageID *string) {

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
	// OVERRIDE_PAYLOAD_KEY
	overridePayloadKey = aws.String("TASK_PAYLOAD")

	// Start ECS task on self
	sess, err := session.NewSession(&aws.Config{Region: aws.String("us-west-2")})
	if err != nil {
		fmt.Println("failed to create session,", err)
		return
	}

	svc := ecs.New(sess)

	params := &ecs.StartTaskInput{
		ContainerInstances: []*string{
			containerInstanceID,
		},
		TaskDefinition: ecsTaskDefinition,
		Cluster:        ecsCluster,
		Overrides: &ecs.TaskOverride{
			ContainerOverrides: []*ecs.ContainerOverride{
				{
					Environment: []*ecs.KeyValuePair{
						{
							Name:  overridePayloadKey,
							Value: aws.String(messageBody),
						},
					},
					Name: overrideContainerName,
				},
			},
		},
		StartedBy: aws.String("tasque"),
	}
	resp, err := svc.StartTask(params)

	if err != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		fmt.Println("Error:", err.Error())
		return
	}

	// Pretty-print the response data.
	fmt.Println(resp)
}

func monitorDocker() {
	// Connect to docker event service
	d.connect()
	d.addListener()
	// Monitor docker events for sibling Projector task
	status, err := d.listenForDie()
	if err != nil {
		panic(err)
	}

	if status == "0" {
		// status is die
		log.Printf("[INFO] Execution completed successfully")
		x.success()
	} else if status == "timeout" {
		// event is timeout
		log.Printf("[ERROR] Execution timed out")
		x.failure()
	} else {
		// non-zero exit
		log.Printf("[ERROR] Execution completed with non-zero exit status")
		x.failure()
	}
}

func (dockerobj *Docker) listenForDie() (exitCode string, err error) {
	log.Printf("[INFO] Monitoring Docker events.")
	log.Printf("%+v\n", dockerobj)
	timeout := time.After(getTimeout())
	defer dockerobj.removeListener()
	for {
		select {
		case msg := <-dockerobj.eventsCh:
			if msg != nil {
				// log.Printf("%+v\n", msg)
				matched, _ := regexp.MatchString(
					fmt.Sprintf(".*%s.*", *overrideContainerName),
					msg.Actor.Attributes["name"])

				if matched {
					log.Printf("%+v\n", msg)
					switch msg.Action {
					case "die":
						log.Printf("[INFO] Container die event")
						return msg.Actor.Attributes["exitCode"], nil
					}
				}
			}
		case <-timeout:
			log.Printf("[INFO] Instance timeout reached.")
			// TODO this would possibly be an error
			return "timeout", nil
		}
	}
}

func (dockerobj *Docker) connect(string dockerEndpointPath) {
	log.Printf("[INFO] Connecting to Docker API.")
	endpoint := dockerEndpointPath
	client, err := docker.NewClient(endpoint)
	if err != nil {
		panic(err)
	}
	dockerobj.client = client
	dockerobj.eventsCh = make(chan *docker.APIEvents)
}

func (dockerobj *Docker) addListener() {
	// docker.eventsCh = make(chan *docker.APIEvents)
	// input_data := make(chan *sfn.GetActivityTaskOutput)
	err := dockerobj.client.AddEventListener(dockerobj.eventsCh)
	if err != nil {
		log.Fatal(err)
	}
}

func (dockerobj *Docker) removeListener() {
	err := dockerobj.client.RemoveEventListener(dockerobj.eventsCh)
	if err != nil {
		log.Fatal(err)
	}
}
