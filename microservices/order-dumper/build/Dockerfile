# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
RUN apk update && \
    apk upgrade
RUN apk add openjdk8=8.302.08-r2
WORKDIR /app
RUN apk add bash
ADD kafka-dump-tool-distribution.tar.gz /app/
COPY go.mod ./
COPY go.sum ./
COPY wrapper.sh ./

RUN go mod download
COPY *.go ./

RUN go build -o /anvil
RUN mkdir -p /processed

RUN ln -sf /dev/stdout /var/log/load-sink.log

CMD /app/wrapper.sh
