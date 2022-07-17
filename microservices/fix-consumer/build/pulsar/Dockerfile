# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
WORKDIR /app
RUN apk update && \
    apk upgrade
#RUN apk add openjdk8=8.302.08-r2
RUN apk add openjdk8=8.322.06-r0
RUN apk add gcc
RUN apk add musl-dev
RUN apk add python3

COPY go.mod ./
COPY go.sum ./
ADD fix-perf-tools.tar.gz /app/

COPY oauth2/ ./oauth2/
COPY pulsar/ ./pulsar/
RUN go mod download
COPY *.go ./
COPY use.py ./
COPY sign384.py ./

RUN go get github.com/gomodule/redigo/redis
RUN go build -o /fxconsumer

RUN mkdir -p /data

CMD /fxconsumer
