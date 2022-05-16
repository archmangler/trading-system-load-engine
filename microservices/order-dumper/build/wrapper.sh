#!/bin/bash
# turn on bash's job control
set -m
# Start the primary process and put it in the background
/anvil &
# Start the helper process
#/app/gor -verbose 2 --input-raw :80 --output-http http://replay-service.ragnarok.svc.cluster.local/replay-admin
# now we bring the primary process back into the foreground
# and leave it there
fg %1
