#!/bin/bash
PATH=$(pwd):$PATH
CONSUL_VERSION="0.6.4"

function download_consul {
  if [ $(uname | grep -i Darwin) ]; then
    echo "Running Darwin"
    OS="darwin"
  else
    echo "Assuming Linux"
    OS="linux"
  fi

  if [ $(uname -m | grep 64) ]; then
    ARCH="amd64"
  else
    ARCH="386"
  fi

  wget -O  http://releases.hashicorp.com/consul/$CONSUL_VERSION/consul_"$CONSUL_VERSION"_"$OS"_$ARCH.zip
  unzip consul_"$CONSUL_VERSION"_"$OS"_$ARCH.zip
  rm consul_"$CONSUL_VERSION"_"$OS"_$ARCH.zip
}

if [ $(which consul) ]; then
  echo "Consul found in path. Skipping download"
else
  download_consul
fi

go test
