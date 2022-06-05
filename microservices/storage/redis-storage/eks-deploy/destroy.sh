#!/bin/bash
#convenient helm install
set -x
printf "===================== BEGIN removing redis cluster from helm chart ..."

helm uninstall ragnarok charts/redis --namespace redis

for i in `seq 1 3`;do echo "checking redis delete: " - $(kubectl get pods -n redis;sleep 1);done
printf "===================== DONE removing redis cluster from helm chart ..."
