# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY *.go ./
RUN mkdir -p ./credsloader
COPY loadcreds/loadcreds.go ./credsloader/

#Pulsar pre-requisites in apk
RUN apk add gcc
RUN apk add musl-dev
RUN apk add bash

RUN go build -o /loader

#build the credential bootstrap loader
RUN go build ./credsloader/loadcreds.go

#store user credential data for processing
#and later removal
RUN mkdir -p /appconfig
RUN mkdir -p /applogs
RUN mkdir -p /userdata
COPY users /userdata
COPY wrapper.sh ./

#install kubectl for kube-ops
RUN apk add curl
RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.18.0/bin/linux/amd64/kubectl
RUN chmod +x ./kubectl
RUN mv ./kubectl /usr/local/bin/kubectl

RUN ln -sf /dev/stdout /applogs/loader.log

CMD /app/wrapper.sh
