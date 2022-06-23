#!/bin/bash
set -x
kubectl delete -f serialout-manifest-eks.yaml
kubectl apply -f serialout-manifest-eks.yaml
