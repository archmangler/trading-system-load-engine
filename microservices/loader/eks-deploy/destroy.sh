#!/bin/bash
#TODO: sed the manifest to update the ingress LB IP for grafana URL
set -x 

kubectl delete -f loader-manifest-eks.yaml
for i in  `ls ../rbac-config/*.yaml`
do
  kubectl delete -f ../rbac-config/$i
done
