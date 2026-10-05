#!/bin/bash
set -euo pipefail
umask 077
if [[ $# != 5 ]]; then echo 'usage: prepare_support.sh SOURCE CONFIG DOMAIN RESOURCES NEW_OUTPUT' >&2; exit 2; fi
exec /usr/bin/python3 "$1/tools/private-beta/support_resources.py" "$@"
