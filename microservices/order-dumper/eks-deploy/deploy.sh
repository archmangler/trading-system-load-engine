#!/bin/bash
#simple deploy to EKS
#
kubectl delete -f order-dumper-eks-deployment.yaml
kubectl apply -f order-dumper-eks-deployment.yaml
