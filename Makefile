# TASQUE_VERSION=0.02
# LANGUAGES=node

# build:
# 	go get -v
# 	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tasque *.go
#
# docker-build:
# 	make -C Dockerfiles
#
# push: build
# 	make push -C Dockerfiles

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o tasque .
	docker build -t tasque/tasque .

upload:
	docker push tasque/tasque
