# Service to read orders in order from REDIS and produce to pulsar/kafka topic preserving order (sequential)

#Overiew

This will generate a load consisting of orders in sequence.
This means there are limits to the load which can be generated, since order must be preserved and we cannot take advantage of concurrent serial workers.


