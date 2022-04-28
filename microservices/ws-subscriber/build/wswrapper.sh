#!/bin/bash
echo "======== begin websocket run ==========="
source /app/.ws-subscriber/bin/activate && python3.7 ws-subscriber.py
echo "======== end websocket run ==========="
