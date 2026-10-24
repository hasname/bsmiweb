#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
umask 077
dotenv_file=$(mktemp "$(pwd)/.env.deploy.XXXXXXXX")
trap 'rm -f "$dotenv_file"' EXIT HUP INT TERM

# Keep recursive make inside this script so `make -n deploy` cannot execute
# credential handling or Ansible as part of a recursive recipe line.
make --no-print-directory -C ../bsmiweb-credentials decrypt > "$dotenv_file"
ansible-playbook -i ansible/inventory.ini ansible/deploy.yml -e "dotenv_file=$dotenv_file"
