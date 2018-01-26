package main

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"encoding/json"
	"gopkg.in/urfave/cli.v1"
)

const (
	StepFunctionArnFormat = "arn:aws:states:[^:]+:[^:]+:activity:[^:]+"
	SQSURLFormat          = "https://sqs.([a-zA-Z0-9-]+).amazonaws.com/[^/]+/.+"
)

var (
	// Version is set by Makefile ldflags
	Version = "undefined"
	// BuildDate is set by Makefile ldflags
	BuildDate string
	// GitCommit is set by Makefile ldflags
	GitCommit string
	// GitBranch is set by Makefile ldflags
	GitBranch string
	// GitSummary is set by Makefile ldflags
	GitSummary string
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
	app := cli.NewApp()
	app.Name = "tasque"
	app.Usage = "Pass messages to executables and Docker containers from AWS SQS or Step Functions"
	app.Version = Version
	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Printf("version=%s buildDate=%s sha=%s branch=%s (%s)\n", c.App.Version, BuildDate, GitCommit, GitBranch, GitSummary)
	}
	app.Action = func(c *cli.Context) error {
		otherMain(c)
		return nil
	}

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "execute-method, deploy-method, m",
			Usage:  "execution environment: local, docker, or ecs",
			Value:  "local",
			EnvVar: "EXECUTE_METHOD,DEPLOY_METHOD",
		},
		cli.StringFlag{
			Name:   "container-name, n",
			Usage:  "name for the new container",
			Value:  "tasque_executable",
			EnvVar: "CONTAINER_NAME,DOCKER_CONTAINER_NAME,ECS_CONTAINER_NAME",
		},
		cli.StringFlag{
			Name:   "docker-endpoint, e",
			Usage:  "the unix socket for Docker API connections",
			Value:  "unix:///var/run/docker.sock",
			EnvVar: "DOCKER_ENDPOINT",
		},
		cli.StringFlag{
			Name:   "task-definition, f",
			Usage:  "ARN of the ECS task or JSON suitable for Docker API /container/create",
			EnvVar: "TASK_DEFINITION,DOCKER_TASK_DEFINITION,ECS_TASK_DEFINITION",
		},
		cli.StringFlag{
			Name:   "sfn-activity-arn, sqs-queue-url, q",
			Usage:  "the Step Functions activity ARN or SQS queue URL to receive messages on",
			EnvVar: "TASK_ACTIVITY_ARN,TASK_QUEUE_URL,RECEIVE_PATH",
		},
		cli.DurationFlag{
			Name:   "sfn-heartbeat, b",
			Usage:  "sends a message to the Step Function activity that the task is making progress (example: 10s 40m 1h 3d)",
			Value:  time.Second * 30,
			EnvVar: "TASK_HEARTBEAT",
		},
		cli.StringFlag{
			Name:   "payload, p",
			Usage:  "the data payload to pass to the executable, useful for testing",
			EnvVar: "TASK_PAYLOAD,PAYLOAD",
		},
		cli.StringFlag{
			Name:   "payload-key",
			Usage:  "the env var to set in the executable environment",
			Value:  "TASK_PAYLOAD",
			EnvVar: "TASK_PAYLOAD_KEY",
		},
		cli.DurationFlag{
			Name:   "task-timeout, t",
			Usage:  "the maximimum amount of time allowed for the executable to run (example: 10s 40m 1h 3d)",
			Value:  time.Second * 30,
			EnvVar: "TASK_TIMEOUT",
		},
		cli.StringFlag{
			Name:   "docker-auth",
			Usage:  "a docker authentication json string",
			EnvVar: "DOCKER_AUTH_DATA",
		},
	}

	app.Run(os.Args)
}
func otherMain(c *cli.Context) {
	var (
		executeMethod         string
		taskDefinition        string
		payload               string
		overrideContainerName string
		dockerEndpointPath    string
	)

	taskDefinition = c.String("task-definition")
	executeMethod = c.String("execute-method")
	payload = c.String("payload")
	overrideContainerName = c.String("container-name")
	dockerEndpointPath = c.String("docker-endpoint")

	tasque := Tasque{}

	sfnfmt := regexp.MustCompile(StepFunctionArnFormat)
	sqsfmt := regexp.MustCompile(SQSURLFormat)
	var handler MessageHandler
	switch {
	case sfnfmt.MatchString(c.String("q")):
		handler = &SFNHandler{
			activityARN: c.String("q"),
		}
	case sqsfmt.MatchString(c.String("q")):
		handler = &SQSHandler{
			awsRegion: sqsfmt.FindStringSubmatch(c.String("q"))[1],
			queueURL:  c.String("q"),
		}
	default:
		handler = &ENVHandler{
			localPayload: payload,
		}
	}
	tasque.Handler = handler

	switch executeMethod {
	case "local":
		var argSlice []string
		if len(c.Args().Tail()) > 0 {
			argSlice = c.Args().Tail()
		}
		tasque.Executable = &Executable{
			binary:    c.Args().Get(0),
			arguments: argSlice,
			timeout:   c.Duration("task-timeout"),
		}
	case "ecs":
		d := &Docker{}
		d.connect(dockerEndpointPath)
		payloadKey := c.String("payload-key")
		tasque.Executable = &AWSECS{
			docker:                d,
			ecsTaskDefinition:     &taskDefinition,
			overrideContainerName: &overrideContainerName,
			overridePayloadKey:    &payloadKey,
			timeout:               c.Duration("task-timeout"),
			heartbeatDuration:     c.Duration("sfn-heartbeat"),
		}
	case "docker":
		dockerTaskDefinition := DockerTaskDefinition{}
		json.Unmarshal([]byte(taskDefinition), &dockerTaskDefinition)
		d := &AWSDOCKER{
			containerName:        overrideContainerName,
			timeout:              c.Duration("task-timeout"),
			containerArgs:        payload,
			dockerTaskDefinition: dockerTaskDefinition,
			authData:             c.String("docker-auth"),
		}
		d.connect(dockerEndpointPath)
		tasque.Executable = d
	}
	tasque.runWithTimeout()
}

func (tasque *Tasque) runWithTimeout() {
	// Commented code is for potential future "daemon"
	// var wg sync.WaitGroup
	// for i := 0; i < 5; i++ {
	// 	wg.Add(1)
	// 	go func() {
	// 		defer wg.Done()
	// 		for i := 0; i < 5; i++ {
	tasque.Executable.Execute(tasque.Handler)
	// 		}
	// 	}()
	// }
	// wg.Wait()
}
