#!/bin/bash
source .env
export GITHUB_TOKEN
./server > server.log 2>&1 &
echo $! > server.pid
echo "Server started with PID $(cat server.pid)"
