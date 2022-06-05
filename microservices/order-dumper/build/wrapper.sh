#!/bin/bash
# turn on bash's job control
set -m
# Start the primary process and put it in the background
/dumper &
# Start the DNS configuration helper process
/app/dnsfix.sh
# now we bring the primary process back into the foreground
# and leave it there
fg %1
