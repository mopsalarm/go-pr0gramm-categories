#!/bin/sh

docker run --rm \
  -v "$(pwd):/src" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  centurylink/golang-builder \
  mopsalarm/pr0gramm-categories

docker push mopsalarm/pr0gramm-categories
