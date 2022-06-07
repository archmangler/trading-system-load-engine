#!/bin/bash
#Little helper script to create the ECR docker login secret
#
#Set the secret as follows:
echo "Docker Password => ${DOCKER_PASSWORD}"

#remove old secret ...
kubectl delete secret ragnarok \
    --namespace ragnarok

#replace old secret
kubectl create secret docker-registry ragnarok \
    --namespace ragnarok \
    --docker-server=605125156525.dkr.ecr.ap-southeast-1.amazonaws.com \
    --docker-username=AWS \
    --docker-password="${DOCKER_PASSWORD}"
