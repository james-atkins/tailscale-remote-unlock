#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o xtrace

DIR=/tmp/tailscale-remote-unlock-tests

tank=tsru-test-tank
dozer=tsru-test-dozer

if [ "$1" = "setup" ]; then
  mkdir -p $DIR

  truncate -s 64M $DIR/{tank,dozer}.img

  # Create pools
  zpool create $tank -O mountpoint=none $DIR/tank.img
  echo "password" | zpool create $dozer -O encryption=on -O keylocation=prompt -O keyformat=passphrase -O mountpoint=none $DIR/dozer.img

  # Create datasets, some encrypted and some not
  zfs create $tank/noenc
  echo "password" | zfs create -o encryption=on -o keylocation=prompt -o keyformat=passphrase $tank/enc
  zfs create $tank/enc/inherited
  echo "correct horse battery staple" | zfs create -o encryption=on -o keylocation=prompt -o keyformat=passphrase $tank/moreenc
  echo "correct horse battery staple" | zfs create -o encryption=on -o keylocation=prompt -o keyformat=passphrase $dozer/subenc

  # Finally, import the datasets but without their passwords
  zpool export $tank $dozer
  zpool import -a -d $DIR

elif [ "$1" = "cleanup" ]; then
  zpool export $tank $dozer
  rm -r $DIR
else
  echo "Unknown option: $1"
fi

