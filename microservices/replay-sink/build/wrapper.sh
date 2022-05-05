#!/bin/bash
# turn on bash's job control
set -m
# Start the primary process and put it in the background
/anvil &
# Start the helper process
/app/gor --input-raw :80 --output-file=`hostname`.gor
# now we bring the primary process back into the foreground
# and leave it there
fg %1
