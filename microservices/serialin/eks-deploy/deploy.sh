#!/bin/bash
#simple deploy to EKS
#
set -x
kubectl apply -f serialin-manifest-eks-sts.yaml
