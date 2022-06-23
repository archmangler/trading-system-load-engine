#!/bin/bash
#Little helper script to create the ECR docker login secret
#
#Set the secret as follows:
#export DOCKER_PASSWORD=`aws ecr get-login-password --region ap-southeast-1`
echo "Docker Password => ${DOCKER_PASSWORD}"

#remove old secret ...
kubectl delete secret ragnarok \
    --namespace ragnarok

#replace old secret
kubectl create secret docker-registry \
    ragnarok \
    --namespace ragnarok \
    --docker-server=engeneon.jfrog.io \
    --docker-username=traiano@gmail.com \
    --docker-password="${DOCKER_PASSWORD}"
