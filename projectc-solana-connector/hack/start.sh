#!/bin/bash

server="./projectc-solana-connector"
let item=0
item=`ps -ef | grep $server | grep -v grep | wc -l`

if [ $item -eq 1 ]; then
	echo "The projectc-solana-connector is running, shut it down..."
	pid=`ps -ef | grep $server | grep -v grep | awk '{print $2}'`
	kill -9 $pid
fi

echo "Start projectc-solana-connector now ..."
make build
./build/pkg/cmd/projectc-solana-connector/projectc-solana-connector  >> projectc-solana-connector.log 2>&1 &
