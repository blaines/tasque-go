package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/fsouza/go-dockerclient"
)

// AWSECS hello world
type AWSECS struct {
	ecsTaskDefinition     *string
	overrideContainerName *string
	overridePayloadKey    *string
	timeout               time.Duration
	docker                Docker
}

// Docker hello world
type Docker struct {
	client   *docker.Client
	eventsCh chan *docker.APIEvents
}

// InstanceMetadata hello world
type InstanceMetadata struct {
	client   *ec2metadata.EC2Metadata
	document ec2metadata.EC2InstanceIdentityDocument
}

// ECSMetadata hello world
type ECSMetadata struct {
	Cluster              string `json:"Cluster"`
	ContainerInstanceArn string `json:"ContainerInstanceArn"`
	Version              string `json:"Version"`
}

func (ecsmeta *ECSMetadata) init() {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://localhost:51678/v1/metadata", nil)
	if err != nil {
		log.Fatalln(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &ecsmeta); err != nil {
			fmt.Println(string(body))
			panic(err)
		}
	} else {
		panic("ECS metadata service did not return 200")
	}
}

// If the metadata service can't be reached, what do we do?
// -> Retry
// -> Alert?
// -> Devmode
// Currently this will run for 30 seconds and panic/die if it can't connect.
// Devmode option skips this
func (m *InstanceMetadata) init() *ec2metadata.EC2Metadata {
	// Locate this instance
	timeoutDuration, _ := time.ParseDuration("30s")
	timeout := time.After(timeoutDuration)
	i := 0
	for {
		i++
		select {
		default:
			log.Printf("[INFO] Connecting metadata service (%d)", i)
			sess, err := session.NewSession()
			if err != nil {
				fmt.Println("failed to create session,", err)
				panic("failed to create session")
			}

			m.client = ec2metadata.New(sess)
			m.document, _ = m.client.GetInstanceIdentityDocument()
			if m.client.Available() {
				log.Printf("[INFO] AWS EC2 instance detected via default metadata API endpoint")
				return m.client
			}
		case <-timeout:
			panic("AWS metadata service connection failed")
		}
	}
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
		ch <- executable.executionHelper(handler.body(), handler.id())
	}()
	select {
	case err := <-ch:
		if err != nil {
			log.Printf("E: %s %s", executable.ecsTaskDefinition, err.Error())
			handler.failure()
		} else {
			log.Printf("I: %s finished successfully", executable.ecsTaskDefinition)
			handler.success()
		}
	case <-time.After(executable.timeout):
		log.Printf("E: %s timed out after %f seconds", executable.ecsTaskDefinition, executable.timeout.Seconds())
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

func (executable *AWSECS) executionHelper(messageBody *string, messageID *string) error {
	var exitCode int
	var err error
	executable.startECSContainer(messageBody, messageID)
	executable.monitorDocker()
	// return err
	return nil
}

//  Task ARN is part of Docker labels...
//                 "com.amazonaws.ecs.task-arn": "arn:aws:ecs:us-west-2:770136283015:task/d8e65fde-65dc-4e46-aeaa-8b2b33215349",

func (executable *AWSECS) startECSContainer(messageBody *string, messageID *string) {
	e := &ECSMetadata{}
	m := &InstanceMetadata{}
	m.init()
	e.init()
	var ecsCluster *string
	var containerInstanceID *string

	ecsCluster = aws.String(e.Cluster)
	containerInstanceID = aws.String(e.ContainerInstanceArn)

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
		TaskDefinition: executable.ecsTaskDefinition,
		Cluster:        ecsCluster,
		Overrides: &ecs.TaskOverride{
			ContainerOverrides: []*ecs.ContainerOverride{
				{
					Environment: []*ecs.KeyValuePair{
						{
							Name:  executable.overridePayloadKey,
							Value: aws.String(*messageBody),
						},
					},
					Name: executable.overrideContainerName,
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

func (executable *AWSECS) monitorDocker() {
	// Connect to docker event service
	executable.docker.connect()
	executable.docker.addListener()
	// Monitor docker events for sibling Projector task
	status, err := executable.listenForDie()
	if err != nil {
		panic(err)
	}

	if status == "0" {
		// status is die
		log.Printf("[INFO] Execution completed successfully")
		executable.success()
	} else if status == "timeout" {
		// event is timeout
		log.Printf("[ERROR] Execution timed out")
		executable.failure()
	} else {
		// non-zero exit
		log.Printf("[ERROR] Execution completed with non-zero exit status")
		executable.failure()
	}
}

func (executable *AWSECS) listenForDie() (exitCode string, err error) {
	log.Printf("[INFO] Monitoring Docker events.")
	log.Printf("%+v\n", executable.docker)
	timeout := time.After(getTimeout())
	defer executable.docker.removeListener()
	for {
		select {
		case msg := <-executable.docker.eventsCh:
			if msg != nil {
				// log.Printf("%+v\n", msg)
				matched, _ := regexp.MatchString(
					fmt.Sprintf(".*%s.*", *executable.overrideContainerName),
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

func (executable *AWSECS) success() {}
func (executable *AWSECS) failure() {}
