#!/bin/sh
set -eux

if ! docker info > /dev/null 2>&1; then
    echo "Starting docker..."
    service docker start
fi
