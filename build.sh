#!/bin/sh
set -e

docker build -t mopsalarm/pr0gramm-categories .
docker push mopsalarm/pr0gramm-categories
