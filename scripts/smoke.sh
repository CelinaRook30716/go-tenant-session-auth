#!/bin/sh
set -eu

: "${CAPTCHA_TOKEN:?set CAPTCHA_TOKEN to a browser-issued token}"

curl --fail-with-body --request POST http://localhost:8080/signup \
  --header 'Content-Type: application/json' \
  --data "{\"tenant_name\":\"Warehouse Labs\",\"email\":\"owner@example.com\",\"password\":\"pipeline-passphrase\",\"captcha_token\":\"$CAPTCHA_TOKEN\"}"
