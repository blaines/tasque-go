# Tasque Docker Image

## Build

```
go build -o tasque *.go
```

## Usage

### Standalone

Example:
```
./tasque node ../tasque-node-example/worker.js
```

```
TASK_QUEUE_URL='{SQS URL}' AWS_REGION='us-west-2' TASK_TIMEOUT="30s" ./tasque node ../tasque-node-example/worker.js
```

ECS Mode
```
docker run --rm --net=host \
  -e DOCKER=true \
  -e TASK_ACTIVITY_ARN=[ARN] \
  -e ECS_CONTAINER_NAME=container-in-definition \
  -e ECS_TASK_DEFINITION=task-definition-name \
  -e AWS_ACCESS_KEY_ID=[SECRET] \
  -e AWS_SECRET_ACCESS_KEY=[SECRET] \
  -e EXIT5=50000 \
  -v /var/run/docker.sock:/var/run/docker.sock tasque/tasque
```

### Message Handlers

AWS SQS

AWS Step Functions

TASK_PAYLOAD Environment Variable

### Execution Handlers

Docker

AWS ECS

Direct Execution

### Environment Variables

AWS_REGION

DEPLOY_METHOD

DOCKER

DOCKER_CONTAINER_NAME

DOCKER_ENDPOINT

DOCKER_ENDPOINT

DOCKER_TASK_DEFINITION

ECS_CONTAINER_NAME

ECS_TASK_DEFINITION

ERROR_MESSAGE_TEMPLATE

TASK_ACTIVITY_ARN

TASK_HEARTBEAT

TASK_PAYLOAD

TASK_PAYLOAD

TASK_QUEUE_URL

TASK_TIMEOUT

#### Error Translation Variables

Your application should use a non-zero exit status upon failure. There are 255 valid non-zero exit codes, and some are specially reserved (http://tldp.org/LDP/abs/html/exitcodes.html). To accommodate for this limitation Tasque will capture and raise those errors depending on it's messaging handler.

`EXIT_%d` - Translate an exit status code into the value of this environment variable.

`EXIT_AGENT` - Instance's agent is disconnected

`EXIT_ATTRIBUTE` - A required attribute is unavailable on the instance

`EXIT_CPU` - Not enough CPU

`EXIT_MEMORY` - Not enough memory

`EXIT_PARAMETER` - Bad parameter specified in ECS start task call (container name is usually the culprit)

`EXIT_RESOURCE` - Other resource error

`EXIT_TIMEOUT` - The execution timed out

`EXIT_UNKNOWN` - An unlabeled error occurred

## Build

```
make build
```

## Known Issues

#### Argument list too long
- Reduce the TASK_PAYLOAD size or increase the container stack size.
- Use the file output option or piped output option instead.

Resources:
http://man7.org/linux/man-pages/man2/execve.2.html
http://git.kernel.org/cgit/linux/kernel/git/torvalds/linux.git/commit/?id=b6a2fea39318e43fee84fa7b0b90d68bed92d2ba
http://unix.stackexchange.com/questions/120642/what-defines-the-maximum-size-for-a-command-single-argument


## Brain Dump

TASK_PAYLOAD - different every execution

DOCKER_TASK_DEFINITION - could be json string of params {MacAddress: 00-00-00, MaxMemory: 123gb}
