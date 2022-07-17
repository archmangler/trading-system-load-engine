#!/bin/bash
#TODO: sed the manifest to update the ingress LB IP for grafana URL
set -x 

kubectl apply -f batch-automation.conf
