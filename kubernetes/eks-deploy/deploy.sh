#!/bin/bash
TF_VAR_AWS_CLUSTER_NAME=${AWS_CLUSTER_NAME}
echo "building eks cluster: $AWS_CLUSTER_NAME"

function install_prerequisites () {
  echo "skipping aws installation on Linux (for Mac only)"
  brew install awscli
  #force it ...
  brew link --overwrite awscli
}

function install_authenticator () {
   #On Macosx and for olrder versions of aws cli: https://docs.aws.amazon.com/eks/latest/userguide/install-aws-iam-authenticator.html
   brew install aws-iam-authenticator
   aws eks get-token 
}

function eks_deploy () {
 printf "******************** initializing EKS terraform state ..."
 terraform init
 printf "******************** generating EKS terraform plan ..."
 echo "running: terraform plan -var aws_cluster_name=$TF_VAR_AWS_CLUSTER_NAME -out terraform.plan"
 terraform plan -var aws_cluster_name=$TF_VAR_AWS_CLUSTER_NAME -out terraform.plan
 #TEMPORARY DISABLE
 #terraform apply terraform.plan
}

function get_kube_credentials () {

  CMD="aws eks --region $(terraform output -raw region) update-kubeconfig --name $(terraform output -raw cluster_name)"
  printf "Getting kubeconf with: aws eks --region $(terraform output -raw region) update-kubeconfig --name $(terraform output -raw cluster_name)"
  OUT=$($CMD)
  printf "$OUT\n"

}

function health_checks () {

  printf "=================== Basic Cluster Checks =================\n"
  kubectl get nodes -o wide
  kubectl get ns
  printf "==========================================================\n"

}

function install_metrics_service () {
  printf "metrics server ... coming soon ...\n"
  kubectl apply -f metrics-server-components.yaml
}

#
install_prerequisites
install_authenticator
eks_deploy
health_checks
get_kube_credentials
health_checks
install_metrics_service
