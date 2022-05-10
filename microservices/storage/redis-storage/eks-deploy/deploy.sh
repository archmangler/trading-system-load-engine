#!/bin/bash
#convenient helm install
set -x
printf "===================== BEGIN deploying redis cluster from helm chart ..."
helm install ragnarok charts/redis --namespace redis
printf "===================== DONE deploying redis cluster from helm chart ..."
