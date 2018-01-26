package main

import (
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/blaines/tasque-go/result"
	"github.com/fsouza/go-dockerclient"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

// AWSECS hello world
type AWSECS struct {
	ecsTaskDefinition     *string
	overrideContainerName *string
	overridePayloadKey    *string
	heartbeatDuration     time.Duration
	taskArn               string
	handler               MessageHandler
	timeout               time.Duration
	docker                *Docker
	result                result.Result
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

func (executable AWSECS) Execute(handler MessageHandler) {
	executable.handler = handler
	executable.execute(handler)
}

func (executable *AWSECS) Result() result.Result {
	return executable.result
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

func (executable AWSECS) execute(handler MessageHandler) {
	handler.initialize()
	if handler.receive() {
		executable.executableTimeoutHelper(handler)
	}
}

func (executable *AWSECS) executableTimeoutHelper(handler MessageHandler) {
	// Channel receives exit event
	ch := make(chan error)
	go func() {
		ch <- executable.executionHelper(handler.body(), handler.id())
	}()
	select {
	case err := <-ch:
		if err != nil {
			log.Printf("E: %s %s", *executable.ecsTaskDefinition, err.Error())
			if strings.Contains(err.Error(), "InvalidParameterException") {
				executable.result.SetExit("PARAMETER")
			} else if executable.result.Exit == "" {
				executable.result.SetExit("UNKNOWN")
			}
			handler.failure(executable.result)
		} else {
			log.Printf("I: %s finished successfully", *executable.ecsTaskDefinition)
			handler.success()
		}
	case <-time.After(executable.timeout):
		err := fmt.Errorf("%s timed out after %f seconds", *executable.ecsTaskDefinition, executable.timeout.Seconds())
		log.Println(err)
		executable.result.SetExit("TIMEOUT")
		handler.failure(executable.result)
	}
}

func (executable *AWSECS) executionHelper(messageBody *string, messageID *string) error {
	var err error
	var taskArn string
	taskArn, err = executable.startECSContainer(messageBody, messageID)
	executable.taskArn = taskArn
	if err != nil {
		return err
	}
	err = executable.monitorDocker()
	if err != nil {
		return err
	}
	return nil
}

//  Task ARN is part of Docker labels...
//                 "com.amazonaws.ecs.task-arn": "arn:aws:ecs:us-west-2:770136283015:task/d8e65fde-65dc-4e46-aeaa-8b2b33215349",

func (executable *AWSECS) startECSContainer(messageBody *string, messageID *string) (string, error) {
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
		return "", err
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
		return "", err
	}

	// Pretty-print the response data.
	fmt.Println(resp)
	if len(resp.Failures) > 0 {
		var err error
		// There were errors starting the container
		reason := resp.Failures[0].Reason
		if strings.Contains(*reason, "CPU") {
			executable.result.SetExit("CPU")
			err = fmt.Errorf("%s %s The cpu requested by the task is unavailable on the given container instance. You may need to add container instances to your cluster", *reason, *resp.Failures[0].Arn)
		} else if strings.Contains(*reason, "MEMORY") {
			executable.result.SetExit("MEMORY")
			err = fmt.Errorf("%s %s The memory requested by the task is unavailable on the given container instance. You may need to add container instances to your cluster", *reason, *resp.Failures[0].Arn)
		} else if strings.Contains(*reason, "RESOURCE") {
			executable.result.SetExit("RESOURCE")
			err = fmt.Errorf("%s %s The resource or resources requested by the task are unavailable on the given container instance. If the resource is CPU or memory, you may need to add container instances to your cluster", *reason, *resp.Failures[0].Arn)
		} else if strings.Contains(*reason, "AGENT") {
			executable.result.SetExit("AGENT")
			err = fmt.Errorf("%s %s The container instance that you attempted to launch a task onto has an agent which is currently disconnected. In order to prevent extended wait times for task placement, the request was rejected", *reason, *resp.Failures[0].Arn)
		} else if strings.Contains(*reason, "ATTRIBUTE") {
			executable.result.SetExit("ATTRIBUTE")
			err = fmt.Errorf("%s %s Your task definition contains a parameter that requires a specific container instance attribute that is not available on your container instances. For more information on which attributes are required for specific task definition parameters and agent configuration variables, see Task Definition Parameters and Amazon ECS Container Agent Configuration", *reason, *resp.Failures[0].Arn)
		} else {
			// Unrecognized error
			executable.result.SetExit("UNKNOWN")
			err = fmt.Errorf("Unrecognized error: '%s' %+v", *reason, resp)
		}
		return "", err
	}
	taskArn := resp.Tasks[0].Containers[0].TaskArn
	return *taskArn, nil
}

func (executable *AWSECS) monitorDocker() error {
	executable.docker.addListener()
	// Monitor docker events for sibling Projector task
	status, err := executable.listenForDie()
	if err != nil {
		return err
	}
	executable.result.SetExit(status)

	if status == "0" {
		// status is die
		log.Printf("[INFO] Execution completed successfully")
		executable.success()
		return nil
	}
	// non-zero exit
	log.Printf("[ERROR] Execution completed with non-zero exit status")
	err = fmt.Errorf("%s died with non-zero exit status (exit code %s)", *executable.ecsTaskDefinition, status)
	executable.failure()
	return err

}

func (executable *AWSECS) listenForDie() (exitCode string, err error) {
	log.Printf("[INFO] Monitoring Docker events.")
	log.Printf("[DEBUG] %+v\n", executable.docker)
	timeout := time.After(executable.timeout)
	ticker := time.NewTicker(executable.heartbeatDuration)
	defer func() {
		executable.docker.removeListener()
		ticker.Stop()
	}()
	for {
		select {
		case msg := <-executable.docker.eventsCh:
			if msg != nil {
				matched := msg.Actor.Attributes["com.amazonaws.ecs.task-arn"] == executable.taskArn
				if matched {
					log.Printf("[DEBUG] %+v\n", msg)
					switch msg.Action {
					case "die":
						log.Printf("[INFO] Container die event")
						return msg.Actor.Attributes["exitCode"], nil
					case "start":
						log.Printf("[INFO] Container start event")
						executable.result.SetHost(msg.ID[0:12])
						// Ticker to check docker container status
						go func() {
							for t := range ticker.C {
								container, err := executable.docker.client.InspectContainer(msg.ID)
								if err != nil {
									log.Println(fmt.Errorf("There was an error checking container status %s", err.Error()))
								}
								if container.State.Running == true {
									executable.handler.heartbeat()
									log.Println("Heartbeat", t)
								} else {
									log.Printf("Container state is %s", container.State.Status)
								}
							}
						}()
					}
				}
			}
		case <-timeout:
			log.Printf("[INFO] Instance timeout reached.")
			err := fmt.Errorf("Docker container %s timed out after %f seconds", *executable.ecsTaskDefinition, executable.timeout.Seconds())
			return "timeout", err
		}
	}
}

func (dockerobj *Docker) connect(dockerEndpointPath string) {
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
