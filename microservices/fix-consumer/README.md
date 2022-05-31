# Service to simulate trading order creation using FIX protocol

- Consumes order data from Pulsar/Kafka queue
- Creates orders via the EQONEX FIX API using thew fix testing utility wrapped in a golang routine

