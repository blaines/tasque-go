package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fsouza/go-dockerclient"
)

// AuthData is the authentication information for a docker repository
type AuthData struct {
	Auth   string `json:"auth"`
	Email  string `json:"email"`
	Server string `json:"server"`
}

//DockerTaskDefinition is the varaible setting requests set by the user
type DockerTaskDefinition struct {
	ImageName  string   `json:"ImageName"`
	MacAddress string   `json:"MacAddress"`
	Env        []string `json:"Env"`
}

//AWSDOCKER is a dockerobj. It is identified by an image containerName
type AWSDOCKER struct {
	containerName        string
	taskArn              string
	timeout              time.Duration
	dockerClient         *docker.Client
	eventsCh             chan *docker.APIEvents
	containerArgs        string
	dockerTaskDefinition DockerTaskDefinition
}

func (dockerobj *AWSDOCKER) createDockerContainer(messageBody *string, args []string, env []string, attachStdout bool) (string, error) {
	var taskPayloadEnv []string
	fmt.Println(dockerobj.dockerTaskDefinition.Env)
	taskPayloadEnv = append(taskPayloadEnv, fmt.Sprintf("TASK_PAYLOAD=%s", *messageBody))
	taskPayloadEnv = append(taskPayloadEnv, dockerobj.dockerTaskDefinition.Env...)

	dockerConfig := docker.Config{
		Env:          taskPayloadEnv,
		Image:        dockerobj.dockerTaskDefinition.ImageName,
		AttachStdout: attachStdout,
		AttachStderr: attachStdout,
		MacAddress:   dockerobj.dockerTaskDefinition.MacAddress,
	}
	copts := docker.CreateContainerOptions{Name: dockerobj.containerName, Config: &dockerConfig}
	log.Printf("Create container for image container name: %s\n", dockerobj.dockerTaskDefinition.ImageName)
	container, err := dockerobj.dockerClient.CreateContainer(copts)
	if err != nil {
		return "", err
	}
	log.Printf("Created container container name: %s\n", container.ID)
	return container.ID, err
}

func (dockerobj *AWSDOCKER) deployImage(args []string, env []string, reader io.Reader) error {
	outputbuf := bytes.NewBuffer(nil)
	result := strings.Split(dockerobj.dockerTaskDefinition.ImageName, ":")
	opts := docker.PullImageOptions{
		Repository: result[0],
		Tag:        result[1],
	}
	// We probably need to configure explicit authorization as the library/docker client doesn't appear
	// to be authorized to pull images yet.
	auth, err := fetchAuthConfiguration()
	if err != nil {
		log.Printf("Error authenticating to repository. Is DOCKER_AUTH_DATA set?")
		return err
	}

	if err := dockerobj.dockerClient.PullImage(opts, auth); err != nil {
		log.Printf("Error building images: %s", err)
		log.Printf("Image Output:\n********************\n%s\n********************", outputbuf.String())
		return err
	}

	log.Printf("Created image: %s", dockerobj.dockerTaskDefinition.ImageName)

	return nil
}

// Sets up authentication for pulling docker images from a repository
func fetchAuthConfiguration() (docker.AuthConfiguration, error) {
	authDataString := os.Getenv("DOCKER_AUTH_DATA")
	authData := AuthData{}
	json.Unmarshal([]byte(authDataString), &authData)
	decodedToken, err := base64.StdEncoding.DecodeString(string(authData.Auth))
	if err != nil {
		return docker.AuthConfiguration{}, err
	}
	parts := strings.SplitN(string(decodedToken), ":", 2)
	return docker.AuthConfiguration{
		Username:      parts[0],
		Password:      parts[1],
		ServerAddress: string(authData.Server),
	}, nil
}

//Deploy use the reader containing targz to create a docker image
//for docker inputbuf is tar reader ready for use by docker.Client
//the stream from end dockerClient to peer could directly be this tar stream
//talk to docker daemon using docker Client and build the image
func (dockerobj *AWSDOCKER) Deploy(args []string, env []string, reader io.Reader) error {
	if err := dockerobj.deployImage(args, env, reader); err != nil {
		return err
	}
	return nil
}

//BuildSpecFactory Should be removed
type BuildSpecFactory func() (io.Reader, error)

func (dockerobj *AWSDOCKER) stopInternal(id string, timeout uint, dontkill bool, dontremove bool) error {

	err := dockerobj.dockerClient.StopContainer(id, timeout)
	if err != nil {
		log.Printf("Stop container %s(%s)", id, err)
	} else {
		log.Printf("Stopped container %s", id)
	}
	if !dontkill {
		err = dockerobj.dockerClient.KillContainer(docker.KillContainerOptions{ID: id})
		if err != nil {
			log.Printf("Kill container %s (%s)", id, err)
		} else {
			log.Printf("Killed container %s", id)
		}
	}
	if !dontremove {
		err = dockerobj.dockerClient.RemoveContainer(docker.RemoveContainerOptions{ID: id, Force: true})
		if err != nil {
			log.Printf("Remove container %s (%s)", id, err)
		} else {
			log.Printf("Removed container %s", id)
		}
	}
	return err
}

//Start starts a container using a previously created docker image
func (dockerobj *AWSDOCKER) Start(messageBody *string, args []string, env []string, builder BuildSpecFactory, messageID *string) error {

	attachStdout := true

	//start ECS Task

	//stop,force remove if necessary
	log.Printf("Cleanup image containerName %s", dockerobj.containerName)

	dockerobj.stopInternal(dockerobj.containerName, 0, false, false)

	log.Printf("Start container %s", dockerobj.containerName)
	// Pull image every time to ensure latest
	if err := dockerobj.deployImage(args, env, nil); err != nil {
		return err
	}
	containerID, err := dockerobj.createDockerContainer(messageBody, args, env, attachStdout)
	if err != nil {
		//if image not found try to create image and retry
		if err == docker.ErrNoSuchImage {
			if builder == nil {
				log.Printf("start-could not find image ...attempt to recreate image %s", err)
				var err1 error
				//reader, err1 := builder()
				//if err1 != nil {
				//    log.Printf("Error creating image builder: %s", err1)
				//}

				if err1 = dockerobj.deployImage(args, env, nil); err1 != nil {
					return err1
				}

				log.Printf("start-recreated image successfully")
				if containerID, err1 = dockerobj.createDockerContainer(messageBody, args, env, attachStdout); err1 != nil {
					log.Printf("start-could not recreate container post recreate image: %s", err1)
					return err1
				}
			} else {
				log.Printf("start-could not find image: %s", err)
				return err
			}
		} else {
			log.Printf("start-could not recreate container: %s", err)
			return err
		}
	}
	dockerobj.taskArn = containerID

	if attachStdout {
		// Launch a few go-threads to manage output streams from the container.
		// They will be automatically destroyed when the container exits
		attached := make(chan struct{})
		r, w := io.Pipe()

		go func() {
			// AttachToContainer will fire off a message on the "attached" channel once the
			// attachment completes, and then block until the container is terminated.
			// The returned error is not used outside the scope of this function. Assign the
			// error to a local variable to prevent clobbering the function variable 'err'.
			err := dockerobj.dockerClient.AttachToContainer(docker.AttachToContainerOptions{
				Container:    containerID,
				OutputStream: w,
				ErrorStream:  w,
				Logs:         true,
				Stdout:       true,
				Stderr:       true,
				Stream:       true,
				Success:      attached,
			})

			// If we get here, the container has terminated.  Send a signal on the pipe
			// so that downstream may clean up appropriately
			_ = w.CloseWithError(err)
		}()

		go func() {
			// Block here until the attachment completes or we timeout
			select {
			case <-attached:
				// successful attach
			case <-time.After(10 * time.Second):
				log.Printf("Timeout while attaching to IO channel in container %s", containerID)
				return
			}

			// Acknowledge the attachment?  This was included in the gist I followed
			// (http://bit.ly/2jBrCtM).  Not sure it's actually needed but it doesn't
			// appear to hurt anything.
			attached <- struct{}{}

			// Establish a buffer for our IO channel so that we may do readline-style
			// ingestion of the IO, one log entry per line
			is := bufio.NewReader(r)

			for {
				// Loop forever dumping lines of text into the containerLogger
				// until the pipe is closed
				line, err2 := is.ReadString('\n')
				if err2 != nil {
					switch err2 {
					case io.EOF:
						log.Printf("Container %s has closed its IO channel", containerID)
					default:
						log.Printf("Error reading container output: %s", err2)
					}

					return
				}

				log.Printf(line)
			}
		}()
	}

	err = dockerobj.dockerClient.StartContainer(containerID, nil)
	if err != nil {
		log.Printf("start-could not start container: %s", err)
		return err
	}

	log.Printf("Started container %s", containerID)
	return nil
}

//Stop stops a running chaincode
func (dockerobj *AWSDOCKER) Stop(id string, timeout uint, dontkill bool, dontremove bool) error {

	id = strings.Replace(id, ":", "_", -1)
	err := dockerobj.stopInternal(id, timeout, dontkill, dontremove)

	return err
}

//Destroy destroys an image
func (dockerobj *AWSDOCKER) Destroy(id string, force bool, noprune bool) error {
	id = strings.Replace(id, ":", "_", -1)

	err := dockerobj.dockerClient.RemoveImageExtended(id, docker.RemoveImageOptions{Force: force, NoPrune: noprune})

	if err != nil {
		log.Printf("error while destroying image: %s", err)
	} else {
		log.Printf("Destroyed image %s", id)
	}

	return err
}

func (dockerobj AWSDOCKER) execute(handler MessageHandler) {
	handler.initialize()
	if handler.receive() {
		dockerobj.dockerobjTimeoutHelper(handler)
	}
}

func (dockerobj *AWSDOCKER) dockerobjTimeoutHelper(handler MessageHandler) {
	ch := make(chan error)
	go func() {
		ch <- dockerobj.executionHelper(handler.body(), handler.id())
	}()
	select {
	case err := <-ch:
		if err != nil {
			log.Printf("E: %s %s", dockerobj.containerName, err.Error())
			handler.failure(err)
		} else {
			log.Printf("I: %s finished successfully", dockerobj.containerName)
			handler.success()
		}
	case <-time.After(dockerobj.timeout):
		err := fmt.Errorf("%s timed out after %f seconds", dockerobj.containerName, dockerobj.timeout.Seconds())
		log.Println(err)
		handler.failure(err)
	}
}

func (dockerobj *AWSDOCKER) executionHelper(messageBody *string, messageID *string) error {
	var err error

	args := make([]string, 1)
	env := make([]string, 1)
	env = append(env, *messageBody)

	//taskArn, err = dockerobj.startECSTask(messageBody, messageID)
	//dockerobj.taskArn = taskArn

	err = dockerobj.Start(messageBody, args, env, nil, messageID)
	if err != nil {
		return err
	}
	err = dockerobj.monitorDocker()
	if err != nil {
		return err
	}
	return nil
}

func (dockerobj *AWSDOCKER) monitorDocker() error {
	dockerobj.addListener()
	// Monitor docker events for sibling Projector task
	status, err := dockerobj.listenForDie()
	if err != nil {
		return err
	}

	if status == "0" {
		// status is die
		log.Printf("[INFO] Execution completed successfully")
		dockerobj.success()
		return nil
	}
	// non-zero exit
	log.Printf("[ERROR] Execution completed with non-zero exit status")
	err = fmt.Errorf("%s died with non-zero exit status (exit code %s)", dockerobj.containerName, status)
	dockerobj.failure()
	return err

}

func (dockerobj *AWSDOCKER) listenForDie() (exitCode string, err error) {
	log.Printf("[INFO] Monitoring Docker events.")
	log.Printf("[DEBUG] %+v\n", dockerobj)
	duration := getTimeout()
	timeout := time.After(duration)
	defer dockerobj.removeListener()
	for {
		select {
		case msg := <-dockerobj.eventsCh:
			if msg != nil {
				matched := msg.Actor.ID == dockerobj.taskArn
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
			err := fmt.Errorf("Docker container %s timed out after %f seconds", dockerobj.containerName, duration.Seconds())
			return "timeout", err
		}
	}
}

func (dockerobj *AWSDOCKER) connect(dockerEndpointPath string) {
	log.Printf("[INFO] Connecting to Docker API.")
	endpoint := dockerEndpointPath
	client, err := docker.NewClient(endpoint)
	if err != nil {
		panic(err)
	}
	dockerobj.dockerClient = client
	dockerobj.eventsCh = make(chan *docker.APIEvents)
}

func (dockerobj *AWSDOCKER) addListener() {
	err := dockerobj.dockerClient.AddEventListener(dockerobj.eventsCh)
	if err != nil {
		log.Fatal(err)
	}
}

func (dockerobj *AWSDOCKER) removeListener() {
	err := dockerobj.dockerClient.RemoveEventListener(dockerobj.eventsCh)
	if err != nil {
		log.Fatal(err)
	}
}

func (dockerobj *AWSDOCKER) success() {}
func (dockerobj *AWSDOCKER) failure() {}
