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

### Environment Variables

TASK_QUEUE_URL

AWS_REGION

TASK_PAYLOAD

TASK_TIMEOUT

## Build

__Dockerfile__:
```
FROM blaines/tasque:latest

CMD [ "node", "worker.js" ] # < Change to your executable
```

__docker run__:
```
docker build -t {your-name/your-image-name} .
docker run --volume ~/.aws:/root/.aws -e TASK_QUEUE_URL='{aws-queue-url}' -e AWS_REGION='{aws-region}' {your-name/your-image-name}
```

__example.js__:
```
'use strict';
console.log("region: ", process.env.AWS_REGION);
console.log("queue: ", process.env.TASK_QUEUE_URL);
console.log("payload: ", JSON.parse(process.env.TASK_PAYLOAD));
console.log("id: ", process.env.TASK_ID);

console.log("Hello World. Task Complete.");
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