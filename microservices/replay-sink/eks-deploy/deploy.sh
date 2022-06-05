#!/bin/bash
#simple deploy to EKS
#
kubectl delete -f replay-sink-eks-deployment.yaml
kubectl apply -f replay-sink-eks-deployment.yaml
