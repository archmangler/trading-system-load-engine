#!/bin/bash
printf "Installing prometheus components ...\n"
for i in prometheus-service.yaml prometheus-deployment.yaml config-map.yml clusterRole.yaml;do echo "destroying $i" - $(kubectl delete -f $i);done
for i in prometheus-service.yaml prometheus-deployment.yaml config-map.yml clusterRole.yaml;do echo "applying $i" - $(kubectl apply -f $i);done
