#!/bin/bash
#simple deploy to EKS
#
set -x
kubectl delete -f producer-manifest-eks-sts.yaml
