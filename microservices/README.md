# Load Testing Platform Microservices Catalogue

# Overview

This directory contains a collection of microservices which provide load testing service for the EQONEX Trade Matching Engine via FIX,REST and Websocket APIs.
Briefly, the microservices required to provide this service are as follows:

- consumer    : Consumes order data from a Kafka/Pulsar topic and generates REST API order requests to the Trade Matching Engine
- fix-consumer: Consumes order data from a Kafka/Pulsar topic and generates FIX API order requests to the Trade Matching Engine 
- ingestor 	: Consumes orders in JSON file format stored in AWS S3 bucket for replaying test order data based on historical orders 
- load-sink 	: A dummy http service to accept REST orders from the consumer. For testing purposes
- loader 	: The Load Testing Management service / Management U.I. This provides a basic UI and functions for executing load tests
- order-dumper 	: Extracts historical orders from Kafka topic and dumps into REDIS for replay via HTTP REST and FIX
- producer      : Consumes order data from REDIS and pumpps it at high volume through the Kafka/Pulsar topic for consumption by workers
- ws-subscriber : WebSocket subscriber to load test websocket connections

Infrastructure related:

- storage	: Redis storage microservice
- ingress  	: Kubernetes ingress configuration for microservices. When modifying microservice endpoints or adding new ones, update here

Miscellaneous:

- replay-sink	: Obsoleted PoC for reference: Traffic capture using Gor
- replay	: Obsoleted PoC for reference

# TODO:

- Catalogue and explain each microservice under microservices/

