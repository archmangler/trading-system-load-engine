#!/bin/bash
#simple deploy to EKS
#
set -x
kubectl apply -f producer-manifest-eks-sts.yaml
