#!/bin/bash
#Little helper script to create the ECR docker login secret
#
#Set the secret as follows:
#export DOCKER_PASSWORD=`aws ecr get-login-password --region ap-southeast-1`
DOCKERHUB_SERVER="https://index.docker.io/v1/"
DOCKERHUB_USER="archbungle"
DOCKERHUB_PASS="188ab328-2cc1-4635-b908-453a317342f4"

printf "creating docker registry secret for image access ..."

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
echo "debug: kubectl create secret docker-registry ragnarok --namespace ragnarok --docker-server=${DOCKERHUB_SERVER} --docker-username=${DOCKERHUB_USER} --docker-password=${DOCKERHUB_PASS}"

kubectl create secret docker-registry ragnarok --namespace ragnarok --docker-server=${DOCKERHUB_SERVER} --docker-username="${DOCKERHUB_USER}" --docker-password="${DOCKERHUB_PASS}"
