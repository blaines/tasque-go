package main

import (
    "log"
    "github.com/fsouza/go-dockerclient"
    "io"
    "fmt"
    "bufio"
    "strings"
    "time"
    "bytes"
    "encoding/json"
)


type Payload struct {
    ImageId   string      `json:"ImageId"`
    MacAddress string `json:"MacAddress"`
}


//AWSDOCKER is a dockerobj. It is identified by an image container_name
type AWSDOCKER struct {
    container_name string
    taskArn        string
    timeout        time.Duration
    docker_client  *docker.Client
    eventsCh       chan *docker.APIEvents
    taskDefinition *string
    container_args []string
}

func (dockerobj *AWSDOCKER) createContainer(payload Payload, args []string, env []string, attachStdout bool) (string, error) {
    docker_config := docker.Config{
        Cmd: dockerobj.container_args,
        Image: payload.ImageId,
        AttachStdout: attachStdout,
        AttachStderr: attachStdout,
        MacAddress: payload.MacAddress,
    }
    copts := docker.CreateContainerOptions{Name: dockerobj.container_name, Config: &docker_config}
    log.Printf("Create container for image container name: %s\n", payload.ImageId)
    container, err := dockerobj.docker_client.CreateContainer(copts)
    if err != nil {
        return "", err
    }
    log.Printf("Created container container name: %s\n", container.ID)
    return container.ID, err
}

func (dockerobj *AWSDOCKER) deployImage(payload Payload, args []string, env []string, reader io.Reader) error {
    outputbuf := bytes.NewBuffer(nil)
    opts := docker.BuildImageOptions{
        Name:         payload.ImageId,
        Pull:         false,
        InputStream:  reader,
        OutputStream: outputbuf,
    }

    if err := dockerobj.docker_client.BuildImage(opts); err != nil {
        log.Printf("Error building images: %s", err)
        log.Printf("Image Output:\n********************\n%s\n********************", outputbuf.String())
        return err
    }

    log.Printf("Created image: %s", payload.ImageId)

    return nil
}

//Deploy use the reader containing targz to create a docker image
//for docker inputbuf is tar reader ready for use by docker.Client
//the stream from end docker_client to peer could directly be this tar stream
//talk to docker daemon using docker Client and build the image
func (dockerobj *AWSDOCKER) Deploy(payload Payload, args []string, env []string, reader io.Reader) error {
    if err := dockerobj.deployImage(payload, args, env, reader); err != nil {
        return err
    }
    return nil
}

type BuildSpecFactory func() (io.Reader, error)

func (dockerobj *AWSDOCKER) stopInternal(id string, timeout uint, dontkill bool, dontremove bool) error {

    err := dockerobj.docker_client.StopContainer(id, timeout)
    if err != nil {
        log.Printf("Stop container %s(%s)", id, err)
    } else {
        log.Printf("Stopped container %s", id)
    }
    if !dontkill {
        err = dockerobj.docker_client.KillContainer(docker.KillContainerOptions{ID: id})
        if err != nil {
            log.Printf("Kill container %s (%s)", id, err)
        } else {
            log.Printf("Killed container %s", id)
        }
    }
    if !dontremove {
        err = dockerobj.docker_client.RemoveContainer(docker.RemoveContainerOptions{ID: id, Force: true})
        if err != nil {
            log.Printf("Remove container %s (%s)", id, err)
        } else {
            log.Printf("Removed container %s", id)
        }
    }
    return err
}

//Start starts a container using a previously created docker image
func (dockerobj *AWSDOCKER) Start(messageBody *string, args []string, env []string, builder BuildSpecFactory,  messageID *string) error {

    attachStdout := true

    payload := Payload{}
    json.Unmarshal([]byte(*messageBody), &payload)


    //stop,force remove if necessary
    log.Printf("Cleanup image container_name %s", dockerobj.container_name)

    dockerobj.stopInternal(dockerobj.container_name, 0, false, false)

    log.Printf("Start container %s", dockerobj.container_name)
    containerID, err := dockerobj.createContainer(payload, args, env, attachStdout)
    if err != nil {
        //if image not found try to create image and retry
        if err == docker.ErrNoSuchImage {
            if builder != nil {
                log.Printf("start-could not find image ...attempt to recreate image %s", err)

                reader, err1 := builder()
                if err1 != nil {
                    log.Printf("Error creating image builder: %s", err1)
                }

                if err1 = dockerobj.deployImage(payload, args, env, reader); err1 != nil {
                    return err1
                }

                log.Printf("start-recreated image successfully")
                if containerID, err1 = dockerobj.createContainer(payload, args, env, attachStdout); err1 != nil {
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
            err := dockerobj.docker_client.AttachToContainer(docker.AttachToContainerOptions{
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

    err = dockerobj.docker_client.StartContainer(containerID, nil)
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

    err := dockerobj.docker_client.RemoveImageExtended(id, docker.RemoveImageOptions{Force: force, NoPrune: noprune})

    if err != nil {
        log.Printf("error while destroying image: %s", err)
    } else {
        log.Printf("Destroyed image %s", id)
    }

    return err
}

func (executable AWSDOCKER) execute(handler MessageHandler) {
    handler.initialize()
    if handler.receive() {
        executable.executableTimeoutHelper(handler)
    }
}

func (executable *AWSDOCKER) executableTimeoutHelper(handler MessageHandler) {
    ch := make(chan error)
    go func() {
        ch <- executable.executionHelper(handler.body(), handler.id())
    }()
    select {
    case err := <-ch:
        if err != nil {
            log.Printf("XX: %s %s", *executable.taskDefinition, err.Error())
            handler.failure(err)
        } else {
            log.Printf("I: %s finished successfully", *executable.taskDefinition)
            handler.success()
        }
    case <-time.After(executable.timeout):
        err := fmt.Errorf("%s timed out after %f seconds", *executable.taskDefinition, executable.timeout.Seconds())
        log.Println(err)
        handler.failure(err)
    }
}

func (executable *AWSDOCKER) executionHelper(messageBody *string, messageID *string) error {
    var err error

    args := make([]string, 1)
    env := make([]string, 1)
    env = append(env, *messageBody)

    err = executable.Start(messageBody, args, env, nil, messageID)
    if err != nil {
        return err
    }
    err = executable.monitorDocker()
    if err != nil {
        return err
    }
    return nil
}


func (executable *AWSDOCKER) monitorDocker() error {
    executable.addListener()
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
    err = fmt.Errorf("%s died with non-zero exit status (exit code %s)", *executable.taskDefinition, status)
    executable.failure()
    return err

}


func (executable *AWSDOCKER) listenForDie() (exitCode string, err error) {
    log.Printf("[INFO] Monitoring Docker events.")
    log.Printf("[DEBUG] %+v\n", executable)
    duration := getTimeout()
    timeout := time.After(duration)
    defer executable.removeListener()
    for {
        select {
        case msg := <-executable.eventsCh:
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
            err := fmt.Errorf("Docker container %s timed out after %f seconds", *executable.taskDefinition, duration.Seconds())
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
    dockerobj.docker_client = client
    dockerobj.eventsCh = make(chan *docker.APIEvents)
}


func (dockerobj *AWSDOCKER) addListener() {
    err := dockerobj.docker_client.AddEventListener(dockerobj.eventsCh)
    if err != nil {
        log.Fatal(err)
    }
}

func (dockerobj *AWSDOCKER) removeListener() {
    err := dockerobj.docker_client.RemoveEventListener(dockerobj.eventsCh)
    if err != nil {
        log.Fatal(err)
    }
}

func (executable *AWSDOCKER) success() {}
func (executable *AWSDOCKER) failure() {}