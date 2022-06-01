# Order Dumper Service: Capture orders from the order kafka topic into a DB for later replay
============================================================================================

# Overview

The purpose of the `order-dumper` microservice is to stream historical orders from kafka order queues between two provided timestamps and make them available to be "replayed" against the API.

NOTE: 

Currently, the replay is not meant to reproduce the exact state of the system at the time a given order was created, it is just meant to provide a rough approximation by re-creating the orders being generated at the time.
To create a solution to reproduce the exact state of the Order Matching system at a given time in the order stream, the entire Trade Matching system needs to be engineered to support this kind of "complete point in time" state rollback.

# Operation

In short, `order dumper` does these tasks:

- Dump Orders from the main order kafka topic into a local REDIS DB
- Allow selection and re-play of orders to the orders API (HTTP or FIX)

Once these orders have been dumped into REDIS cache table, they can be loaded for replay by concurrent or serial workers via the `manager service`.

