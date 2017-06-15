package main

import (
    "log"
    "github.com/fsouza/go-dockerclient"
    "github.com/spf13/viper"
    "io"
    "fmt"
    "bufio"
    "strings"
    "time"
    "bytes"
)


var hostConfig   *docker.HostConfig

// DockerInterface represents a docker client
type DockerInterface interface {
    // CreateContainer creates a docker container, returns an error in case of failure
    CreateContainer(opts docker.CreateContainerOptions) (*docker.Container, error)
    // StartContainer starts a docker container, returns an error in case of failure
    StartContainer(id string, cfg *docker.HostConfig) error
    // AttachToContainer attaches to a docker container, returns an error in case of
    // failure
    AttachToContainer(opts docker.AttachToContainerOptions) error
    // BuildImage builds an image from a tarball's url or a Dockerfile in the input
    // stream, returns an error in case of failure
    BuildImage(opts docker.BuildImageOptions) error
    // RemoveImageExtended removes a docker image by its name or ID, returns an
    // error in case of failure
    RemoveImageExtended(id string, opts docker.RemoveImageOptions) error
    // StopContainer stops a docker container, killing it after the given timeout
    // (in seconds). Returns an error in case of failure
    StopContainer(id string, timeout uint) error
    // KillContainer sends a signal to a docker container, returns an error in
    // case of failure
    KillContainer(opts docker.KillContainerOptions) error
    // RemoveContainer removes a docker container, returns an error in case of failure
    RemoveContainer(opts docker.RemoveContainerOptions) error
}

// getDockerInterface returns an instance that implements DockerInterface interface
type getDockerInterface func() (DockerInterface, error)

//AWSDOCKER is a dockerobj. It is identified by an image id
type AWSDOCKER struct {
    id             string
    getDockerFnc   getDockerInterface
    taskArn        string
    timeout        time.Duration
    client         *docker.Client
    eventsCh       chan *docker.APIEvents
    taskDefinition *string
}

func getDockerHostConfig() *docker.HostConfig {
    if hostConfig != nil {
        return hostConfig
    }
    dockerKey := func(key string) string {
        return "dockerobj.docker.hostConfig." + key
    }
    getInt64 := func(key string) int64 {
        defer func() {
            if err := recover(); err != nil {
                log.Printf("load dockerobj.docker.hostConfig.%s failed, error: %v\n", key, err)
            }
        }()
        n := viper.GetInt(dockerKey(key))
        return int64(n)
    }

    var logConfig docker.LogConfig
    err := viper.UnmarshalKey(dockerKey("LogConfig"), &logConfig)
    if err != nil {
        log.Printf("load docker HostConfig.LogConfig failed, error: %s\n", err.Error())
    }
    networkMode := viper.GetString(dockerKey("NetworkMode"))
    if networkMode == "" {
        networkMode = "host"
    }
    log.Printf("docker container hostconfig NetworkMode: %s\n", networkMode)

    hostConfig = &docker.HostConfig{
        CapAdd:  viper.GetStringSlice(dockerKey("CapAdd")),
        CapDrop: viper.GetStringSlice(dockerKey("CapDrop")),

        DNS:         viper.GetStringSlice(dockerKey("Dns")),
        DNSSearch:   viper.GetStringSlice(dockerKey("DnsSearch")),
        ExtraHosts:  viper.GetStringSlice(dockerKey("ExtraHosts")),
        NetworkMode: networkMode,
        IpcMode:     viper.GetString(dockerKey("IpcMode")),
        PidMode:     viper.GetString(dockerKey("PidMode")),
        UTSMode:     viper.GetString(dockerKey("UTSMode")),
        LogConfig:   logConfig,

        ReadonlyRootfs:   viper.GetBool(dockerKey("ReadonlyRootfs")),
        SecurityOpt:      viper.GetStringSlice(dockerKey("SecurityOpt")),
        CgroupParent:     viper.GetString(dockerKey("CgroupParent")),
        Memory:           getInt64("Memory"),
        MemorySwap:       getInt64("MemorySwap"),
        MemorySwappiness: getInt64("MemorySwappiness"),
        OOMKillDisable:   viper.GetBool(dockerKey("OomKillDisable")),
        CPUShares:        getInt64("CpuShares"),
        CPUSet:           viper.GetString(dockerKey("Cpuset")),
        CPUSetCPUs:       viper.GetString(dockerKey("CpusetCPUs")),
        CPUSetMEMs:       viper.GetString(dockerKey("CpusetMEMs")),
        CPUQuota:         getInt64("CpuQuota"),
        CPUPeriod:        getInt64("CpuPeriod"),
        BlkioWeight:      getInt64("BlkioWeight"),
    }

    return hostConfig
}


func (dockerobj *AWSDOCKER) createContainer(client DockerInterface,
    imageID string, containerID string, args []string,
    env []string, attachStdout bool) error {
    config := docker.Config{Cmd: args, Image: imageID, Env: env, AttachStdout: attachStdout, AttachStderr: attachStdout}
    copts := docker.CreateContainerOptions{Name: containerID, Config: &config, HostConfig: getDockerHostConfig()}
    log.Printf("Create container: %s\n", containerID)
    _, err := client.CreateContainer(copts)
    if err != nil {
        return err
    }
    log.Printf("Created container: %s\n", imageID)
    return nil
}

func (dockerobj *AWSDOCKER) deployImage(client DockerInterface, id string,
    args []string, env []string, reader io.Reader) error {
    outputbuf := bytes.NewBuffer(nil)
    opts := docker.BuildImageOptions{
        Name:         id,
        Pull:         false,
        InputStream:  reader,
        OutputStream: outputbuf,
    }

    if err := client.BuildImage(opts); err != nil {
        log.Printf("Error building images: %s", err)
        log.Printf("Image Output:\n********************\n%s\n********************", outputbuf.String())
        return err
    }

    log.Printf("Created image: %s", id)

    return nil
}

//Deploy use the reader containing targz to create a docker image
//for docker inputbuf is tar reader ready for use by docker.Client
//the stream from end client to peer could directly be this tar stream
//talk to docker daemon using docker Client and build the image
func (dockerobj *AWSDOCKER) Deploy(id string, args []string, env []string, reader io.Reader) error {

    client, err := dockerobj.getDockerFnc()
    switch err {
    case nil:
        if err = dockerobj.deployImage(client, id, args, env, reader); err != nil {
            return err
        }
    default:
        return fmt.Errorf("Error creating docker client: %s", err)
    }
    return nil
}

type BuildSpecFactory func() (io.Reader, error)

func (dockerobj *AWSDOCKER) stopInternal(client DockerInterface,
    id string, timeout uint, dontkill bool, dontremove bool) error {
    err := client.StopContainer(id, timeout)
    if err != nil {
        log.Printf("Stop container %s(%s)", id, err)
    } else {
        log.Printf("Stopped container %s", id)
    }
    if !dontkill {
        err = client.KillContainer(docker.KillContainerOptions{ID: id})
        if err != nil {
            log.Printf("Kill container %s (%s)", id, err)
        } else {
            log.Printf("Killed container %s", id)
        }
    }
    if !dontremove {
        err = client.RemoveContainer(docker.RemoveContainerOptions{ID: id, Force: true})
        if err != nil {
            log.Printf("Remove container %s (%s)", id, err)
        } else {
            log.Printf("Removed container %s", id)
        }
    }
    return err
}

//Start starts a container using a previously created docker image
func (dockerobj *AWSDOCKER) Start(imageID string,
    args []string, env []string, builder BuildSpecFactory) error {


    client, err := dockerobj.getDockerFnc()
    if err != nil {
        log.Printf("start - cannot create client %s", err)
        return err
    }

    containerID := strings.Replace(imageID, ":", "_", -1)
    attachStdout := viper.GetBool("dockerobj.docker.attachStdout")

    //stop,force remove if necessary
    log.Printf("Cleanup container %s", containerID)
    dockerobj.stopInternal(client, containerID, 0, false, false)

    log.Printf("Start container %s", containerID)
    err = dockerobj.createContainer(client, imageID, containerID, args, env, attachStdout)
    if err != nil {
        //if image not found try to create image and retry
        if err == docker.ErrNoSuchImage {
            if builder != nil {
                log.Printf("start-could not find image ...attempt to recreate image %s", err)

                reader, err1 := builder()
                if err1 != nil {
                    log.Printf("Error creating image builder: %s", err1)
                }

                if err1 = dockerobj.deployImage(client, imageID, args, env, reader); err1 != nil {
                    return err1
                }

                log.Printf("start-recreated image successfully")
                if err1 = dockerobj.createContainer(client, imageID, containerID, args, env, attachStdout); err1 != nil {
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
            err := client.AttachToContainer(docker.AttachToContainerOptions{
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

    // start container with HostConfig was deprecated since v1.10 and removed in v1.2
    err = client.StartContainer(containerID, nil)
    if err != nil {
        log.Printf("start-could not start container: %s", err)
        return err
    }

    log.Printf("Started container %s", containerID)
    return nil
}


//Stop stops a running chaincode
func (dockerobj *AWSDOCKER) Stop(id string, timeout uint, dontkill bool, dontremove bool) error {
    client, err := dockerobj.getDockerFnc()
    if err != nil {
        log.Printf("stop - cannot create client %s", err)
        return err
    }
    id = strings.Replace(id, ":", "_", -1)

    err = dockerobj.stopInternal(client, id, timeout, dontkill, dontremove)

    return err
}


//Destroy destroys an image
func (dockerobj *AWSDOCKER) Destroy(id string, force bool, noprune bool) error {
    client, err := dockerobj.getDockerFnc()
    if err != nil {
        log.Printf("destroy-cannot create client %s", err)
        return err
    }
    id = strings.Replace(id, ":", "_", -1)

    err = client.RemoveImageExtended(id, docker.RemoveImageOptions{Force: force, NoPrune: noprune})

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
            log.Printf("E: %s %s", *executable.taskDefinition, err.Error())
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
    err = executable.Start("pipeline-agisoft", args, env, nil)
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

func (dockerobj *AWSDOCKER) addListener() {
    err := dockerobj.client.AddEventListener(dockerobj.eventsCh)
    if err != nil {
        log.Fatal(err)
    }
}

func (dockerobj *AWSDOCKER) removeListener() {
    err := dockerobj.client.RemoveEventListener(dockerobj.eventsCh)
    if err != nil {
        log.Fatal(err)
    }
}

func (executable *AWSDOCKER) success() {}
func (executable *AWSDOCKER) failure() {}