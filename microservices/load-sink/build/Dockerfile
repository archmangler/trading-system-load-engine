# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
WORKDIR /app
RUN apk add bash

COPY go.mod ./
COPY go.sum ./
COPY goreplay/gor ./
COPY wrapper.sh ./

RUN go mod download
COPY *.go ./

RUN go build -o /anvil
RUN mkdir -p /processed

RUN ln -sf /dev/stdout /var/log/load-sink.log

CMD /app/wrapper.sh
