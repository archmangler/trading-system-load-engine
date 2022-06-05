#!/bin/bash
set -x
kubectl delete -f consumer-manifest-eks.yaml
kubectl apply -f consumer-manifest-eks.yaml
