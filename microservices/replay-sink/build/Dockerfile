# syntax=docker/dockerfile:1

FROM golang:1.16-alpine
WORKDIR /app
RUN apk add bash
COPY go.mod ./
COPY go.sum ./

RUN go mod download
COPY *.go ./
COPY goreplay/gor ./
COPY wrapper.sh ./

RUN go build -o /anvil
RUN mkdir -p /replay

RUN ln -sf /dev/stdout /var/log/load-replay.log

CMD /app/wrapper.sh
