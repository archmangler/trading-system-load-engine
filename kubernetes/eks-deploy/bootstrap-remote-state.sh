#!/bin/bash
#Purpose: script to bootstrap the remote terraform state  with S3 in AWS as the state storage
#Pre-requisites: Ensure Logged in to AWS account as the correct user with the correct permissions

TFSTATE_BUCKET_NAME="engeneon-ragnarok-tfstate"
REGION_CONSTRAINT="ap-southeast-1"

function create_bucket {
 OUT=$(aws s3api \
   create-bucket \
   --bucket ${TFSTATE_BUCKET_NAME} \
   --region ${REGION_CONSTRAINT} \
   --create-bucket-configuration LocationConstraint=${REGION_CONSTRAINT})
}

function check_bucket {
OUT=$(aws s3api \
  list-objects --bucket $TFSTATE_BUCKET_NAME \
  --query 'Contents[].{Key: Key, Size: Size}')
  printf "bucket contents: $OUT\n"
}

create_bucket
check_bucket
