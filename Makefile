TASQUE_VERSION=0.02
LANGUAGES=node

build:
	go get -v
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tasque *.go

docker-build:
	docker run --rm -v "$(CURDIR)":/go/src/tasque -w /go/src/tasque -e GOPATH="/go" golang:latest bash -c make
	make -C Dockerfiles
	rm tasque

push: build
	make push -C Dockerfiles
