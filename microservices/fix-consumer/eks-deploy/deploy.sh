#!/bin/bash
set -x
kubectl delete -f fix-consumer-manifest-eks.yaml
kubectl apply -f fix-consumer-manifest-eks.yaml
