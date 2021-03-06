#!/bin/bash
set -x
namespace="ragnarok"
POLICY_ARN="arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess"

#function s3_deploy () {
# printf "============ begin deploying order data storage archive =================="
# current_dir = `pwd`
# printf "initializing S3 terraform state in $current_dir ..."
# terraform init
# terraform plan -out terraform.plan
# terraform apply terraform.plan
#}

function create_eks_storage_access() {
  echo "eksctl utils associate-iam-oidc-provider --cluster=$AWS_CLUSTER_NAME"
  eksctl utils associate-iam-oidc-provider --cluster=$AWS_CLUSTER_NAME  --region ${AWS_DEPLOY_REGION} 
  echo "eksctl delete iamserviceaccount --cluster=$AWS_CLUSTER_NAME --name=eks-s3-access --namespace=$namespace"
  eksctl delete iamserviceaccount --cluster=$AWS_CLUSTER_NAME --name=eks-s3-access --namespace=$namespace  --region ${AWS_DEPLOY_REGION}
  echo "eksctl create iamserviceaccount --cluster=$AWS_CLUSTER_NAME --name=eks-s3-access --namespace=$namespace --attach-policy-arn="$POLICY_ARN" --approve --override-existing-serviceaccounts"
  eksctl create iamserviceaccount --cluster=$AWS_CLUSTER_NAME --name=eks-s3-access --namespace=$namespace --attach-policy-arn="$POLICY_ARN" --approve --override-existing-serviceaccounts  --region ${AWS_DEPLOY_REGION}
 printf "============ end deploying order data storage archive =================="
}


function check_kubernetes_access () {
  printf "checking access to kubernetes cluster before going ahead ...\n"
  eksctl get clusters --region ${AWS_DEPLOY_REGION}
}

check_kubernetes_access
#s3_deploy
create_eks_storage_access
