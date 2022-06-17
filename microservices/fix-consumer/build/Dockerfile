# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
WORKDIR /app
RUN apk update && \
    apk upgrade
RUN apk add openjdk8=8.302.08-r2
COPY go.mod ./
COPY go.sum ./

RUN go mod download
COPY *.go ./
COPY use.py ./
COPY sign384.py ./

RUN go get github.com/apache/pulsar-client-go/pulsar
RUN go build -o /fxconsumer
RUN mkdir -p /data

CMD /fxconsumer
