rm -r ./data/node*/geth
rm -r ./data/node*/keystore
rm ./data/*.log
../build/bin/geth --datadir "data/node1" init genesis.json
../build/bin/geth --datadir "data/node2" init genesis.json
../build/bin/geth --datadir "data/node3" init genesis.json
../build/bin/geth --datadir "data/node4" init genesis.json
