# syntax=docker/dockerfile:1

FROM ubuntu:bionic
WORKDIR /app

RUN apt-get update && apt-get install -y wget
RUN wget https://golang.org/dl/go1.17.linux-amd64.tar.gz
RUN tar -zxvf go1.17.linux-amd64.tar.gz -C /usr/local/
RUN rm -f go1.17.linux-amd64.tar.gz
ENV PATH=/usr/local/go/bin:${PATH}

COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY *.go ./

#Pulsar pre-requisites in apk
RUN wget --user-agent=Mozilla -O apache-pulsar-client.deb "https://archive.apache.org/dist/pulsar/pulsar-2.9.1/DEB/apache-pulsar-client.deb"
RUN wget --user-agent=Mozilla -O apache-pulsar-client-dev.deb "https://archive.apache.org/dist/pulsar/pulsar-2.9.1/DEB/apache-pulsar-client-dev.deb"
RUN apt install -y ./apache-pulsar-client.deb
RUN apt install -y ./apache-pulsar-client-dev.deb

ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y gcc apache-pulsar-client apache-pulsar-client-dev curl
RUN apt-get install -y musl-dev
RUN go get github.com/apache/pulsar/pulsar-client-go/pulsar 
RUN go build -o /streamer

#install kubectl for kube-ops
RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.18.0/bin/linux/amd64/kubectl
RUN chmod +x ./kubectl
RUN mv ./kubectl /usr/local/bin/kubectl
RUN apt-get install -y \
        python3 \
        python3-pip \
    && pip3 install --upgrade pip \
    && pip3 install --no-cache-dir \
        awscli \
    && rm -rf /var/cache/apk/*
RUN aws --version 
CMD /streamer
