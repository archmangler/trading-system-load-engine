#!/bin/bash
#update/refresh settings from values.yaml
pulsar_version="2.9.2"

helm upgrade \
  pulsar apache/pulsar \
  --timeout 10m \
  --set initialize=true \
  --namespace pulsar \
  --version ${pulsar_version}\
  -f pulsar.yaml
