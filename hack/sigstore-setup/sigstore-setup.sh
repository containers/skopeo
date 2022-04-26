#!/bin/bash
#
# Copyright 2021 The Sigstore Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script is copied from sigstore/sigstore
# replacing docker-compose with podman-compose
# See https://github.com/sigstore/sigstore/blob/main/test/e2e/e2e-test.sh

set -ex

cleanup() {
  echo "cleanup"
  $COMPOSE_CMD down
}

run=$1
if [ $run = "cleanup" ]
then
    cleanup
else
  trap cleanup ERR

  echo "starting services"

  $COMPOSE_CMD up -d

  count=0

  echo -n "waiting up to 60 sec for system to start"

  until [ $($CONTAINER_RUNTIME ps -a --format "{{.Status}} {{.Image}}" | grep -e vault | grep -c -e Up) == 1 -a $($COMPOSE_CMD logs localstack | grep -c Ready) == 1 ];
  do
    if [ $count -eq 12 ]; then
      echo "! timeout reached"
      exit 1
    else
      echo -n "."
      sleep 5
      let 'count+=1'
    fi
  done

  echo "sigstore setup complete"
fi
