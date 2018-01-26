git_sha = $(shell git rev-parse --short HEAD)
git_branch = $(shell git rev-parse --abbrev-ref HEAD)
git_summary = $(shell git describe --tags --dirty --always)
build_date = $(shell date)
version = $(shell cat VERSION)

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
	-a -installsuffix cgo \
	-ldflags "-X 'main.Version=${version}' -X 'main.GitSummary=${git_summary}' -X 'main.BuildDate=${build_date}' -X main.GitCommit=${git_sha} -X main.GitBranch=${git_branch}" \
	-o tasque .
	docker build -t tasque/tasque .

upload:
	docker push tasque/tasque
