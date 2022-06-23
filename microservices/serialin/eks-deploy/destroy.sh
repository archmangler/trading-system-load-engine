#!/bin/bash
#simple deploy to EKS
#
set -x
kubectl delete -f serialin-manifest-eks-sts.yaml
