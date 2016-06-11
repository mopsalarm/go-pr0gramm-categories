#!/bin/sh
set -e

glide install

CGO_ENABLED=0 go build -a

docker build -t mopsalarm/pr0gramm-categories .
docker push mopsalarm/pr0gramm-categories
