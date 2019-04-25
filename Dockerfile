FROM golang:1.10.3-alpine3.8
MAINTAINER Platform team <platformdev@appdirect.com>
LABEL version="1.0"

RUN apk update
RUN apk add curl
RUN apk add docker
RUN apk add git
                               
RUN curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
RUN go get github.com/google/ko/cmd/ko

ARG KO_DOCKER_REPO=docker.appdirect.tools/knative-eventing
ENV KO_DOCKER_REPO=$KO_DOCKER_REPO

WORKDIR $GOPATH/src/github.com/knative/eventing-sources
COPY . .
RUN addgroup -g 1000 gouser
RUN adduser -G gouser -u 1000 gouser -D -h /home/gouser
RUN chown -R 1000:1000 $GOPATH