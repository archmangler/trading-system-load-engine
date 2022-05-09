#!/bin/bash
#convenient helm install
set -x
helm install ragnarok charts/redis --namespace redis
