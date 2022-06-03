# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
RUN apk update && \
    apk upgrade
RUN apk add openjdk8=8.302.08-r2
RUN apk add --no-cache gcompat
WORKDIR /app
RUN apk add bash
ADD kafka-dump-tool-distribution.tar.gz /app/
COPY go.mod ./
COPY go.sum ./
COPY wrapper.sh ./
COPY dnsfix.sh ./
COPY securities.csv ./

RUN go mod download
COPY *.go ./

RUN go get github.com/gomodule/redigo/ && go get github.com/prometheus/client_golang/prometheus && go get github.com/prometheus/client_golang/prometheus/promauto && go get github.com/prometheus/client_golang/prometheus/promhttp &&  go build -o /dumper
RUN mkdir -p /processed

RUN ln -sf /dev/stdout /var/log/load-sink.log

CMD /app/wrapper.sh
