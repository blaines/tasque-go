package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
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
	taskArn               string
	timeout               time.Duration
	docker                *Docker
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

func (executable AWSECS) execute(handler MessageHandler) {
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
			log.Printf("E: %s %s", *executable.ecsTaskDefinition, err.Error())
			handler.failure(err)
		} else {
			log.Printf("I: %s finished successfully", *executable.ecsTaskDefinition)
			handler.success()
		}
	case <-time.After(executable.timeout):
		err := fmt.Errorf("%s timed out after %f seconds", *executable.ecsTaskDefinition, executable.timeout.Seconds())
		log.Println(err)
		handler.failure(err)
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
		if strings.Contains(*reason, "RESOURCE") {
			err = fmt.Errorf("%s %s The resource or resources requested by the task are unavailable on the given container instance. If the resource is CPU or memory, you may need to add container instances to your cluster", *reason, *resp.Failures[0].Arn)
		} else if strings.Contains(*reason, "AGENT") {
			err = fmt.Errorf("%s %s The container instance that you attempted to launch a task onto has an agent which is currently disconnected. In order to prevent extended wait times for task placement, the request was rejected", *reason, *resp.Failures[0].Arn)
		} else if strings.Contains(*reason, "ATTRIBUTE") {
			err = fmt.Errorf("%s %s Your task definition contains a parameter that requires a specific container instance attribute that is not available on your container instances. For more information on which attributes are required for specific task definition parameters and agent configuration variables, see Task Definition Parameters and Amazon ECS Container Agent Configuration", *reason, *resp.Failures[0].Arn)
		} else {
			// Unrecognized error
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
	duration := getTimeout()
	timeout := time.After(duration)
	defer executable.docker.removeListener()
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
					}
				}
			}
		case <-timeout:
			log.Printf("[INFO] Instance timeout reached.")
			err := fmt.Errorf("Docker container %s timed out after %f seconds", *executable.ecsTaskDefinition, duration.Seconds())
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
