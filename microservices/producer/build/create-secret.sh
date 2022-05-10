#!/bin/bash
#Little helper script to create the ECR docker login secret
#
#Set the secret as follows:
#export DOCKER_PASSWORD=`aws ecr get-login-password --region ap-southeast-1`
DOCKERHUB_SERVER="https://index.docker.io/v1/"

printf "creating docker registry secret for image access ..."

printf "making sure ns is there ...\n"
kubectl create namespace ragnarok

#remove old secret ...
printf "deleting docker image pull secret if present ...\n"

kubectl delete secret ragnarok \
    --namespace ragnarok

#For AWS registry
#replace old secret
#kubectl create secret docker-registry ragnarok \
#    --namespace ragnarok \
#    --docker-server=605125156525.dkr.ecr.ap-southeast-1.amazonaws.com \
#    --docker-username=AWS \
#    --docker-password="${DOCKER_PASSWORD}"

#for dockerhub private registry ...
echo "debug: kubectl create secret docker-registry ragnarok --namespace ragnarok --docker-server=${DOCKERHUB_SERVER} --docker-username=${DOCKERHUB_USERNAME} --docker-password=${DOCKERHUB_PASSWORD}"

kubectl create secret docker-registry ragnarok --namespace ragnarok --docker-server=${DOCKERHUB_SERVER} --docker-username="${DOCKERHUB_USERNAME}" --docker-password="${DOCKERHUB_PASSWORD}"
