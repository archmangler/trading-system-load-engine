# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
WORKDIR /app

RUN apk add gcc
RUN apk add musl-dev
RUN apk add python3

COPY go.mod ./
COPY go.sum ./

COPY oauth2/ ./oauth2/
COPY pulsar/ ./pulsar/
RUN go mod download && go get github.com/gomodule/redigo/redis
COPY *.go ./
COPY use.py ./
COPY sign384.py ./

RUN go build -o /serialout

RUN mkdir -p /data

CMD /serialout
