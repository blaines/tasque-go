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
