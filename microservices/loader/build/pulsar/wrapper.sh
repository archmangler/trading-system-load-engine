#!/bin/bash
# turn on bash's job control
set -m
# Start the primary process and put it in the background
/loader &
# Start the helper process
#E.g:
#/app/gor -verbose 2 --input-raw :80 --output-http http://replay-service.ragnarok.svc.cluster.local/replay-admin
#Run the credential loader script to process credentials into REDIS
/app/loadcreds
# now we bring the primary process back into the foreground
# and leave it there
fg %1
