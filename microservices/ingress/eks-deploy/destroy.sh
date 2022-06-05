#!/bin/bash
set -x
INGRESSNAME="ragnarok-ingress"
namespace="ragnarok"
#because: https://github.com/kubernetes/kubernetes/issues/95983
kubectl patch ingress $INGRESSNAME -n $namespace -p '{"metadata":{"finalizers":[]}}' --type=merge
kubectl delete -f eks-alb-ingress.yaml
