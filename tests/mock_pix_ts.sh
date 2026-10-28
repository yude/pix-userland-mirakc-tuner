#!/bin/sh
# SPDX-License-Identifier: MIT
: "${PIX_MOCK_TS:?PIX_MOCK_TS must name synthetic TS data}"
while :; do
    cat "$PIX_MOCK_TS" || exit 4
    sleep 0.05
done
