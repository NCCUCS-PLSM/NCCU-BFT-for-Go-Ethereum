rm -r ./bdata/node*/geth
rm -r ./bdata/node*/keystore
rm ./bdata/logs/*.log
../build/bin/geth --datadir "bdata/node1" init genesis.json
../build/bin/geth --datadir "bdata/node2" init genesis.json
#../build/bin/geth --datadir "bdata/node3" init genesis.json
#../build/bin/geth --datadir "bdata/node4" init genesis.json
#../build/bin/geth --datadir "bdata/node5" init genesis.json
#../build/bin/geth --datadir "bdata/node6" init genesis.json
#../build/bin/geth --datadir "bdata/node7" init genesis.json
#../build/bin/geth --datadir "bdata/node8" init genesis.json
