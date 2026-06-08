#!/bin/bash
# Phaze Local Test Runner
# Starts a local Nexus server. Open the web client at http://localhost:8080/web/
# (or run the Android app pointed at this host) to connect.

# 1. Kill any existing instance
pkill -f "phaze-nexus"

echo "--- Starting Phaze Nexus (Local) ---"
cd nexus_server
go build -o phaze-nexus .
DB_PATH=./nexus.db PORT=8080 ./phaze-nexus &
SERVER_PID=$!

echo "Waiting for server to spin up..."
sleep 2

echo "Ready! Nexus running on http://localhost:8080"
echo "Open the web client:  http://localhost:8080/web/"

# Cleanup on exit
trap "kill $SERVER_PID" EXIT
wait
