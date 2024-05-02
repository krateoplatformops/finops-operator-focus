#!/bin/bash

if [ -z "${CONTAINER_TOOL}" ]; then
    CONTAINER_TOOL=docker
else
    CONTAINER_TOOL=${CONTAINER_TOOL}
fi

$(echo $CONTAINER_TOOL) build -t ${IMG} .
$(echo $CONTAINER_TOOL) push ${IMG}


