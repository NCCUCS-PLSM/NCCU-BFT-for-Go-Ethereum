../build/bin/geth \
\
--networkid 2234 \
--port 30303 \
--rpcport 8545 \
--datadir "data/node1" \
--nodiscover \
\
--rpc \
--rpccorsdomain "*" \
--rpcapi "eth,net,debug" \
\
--verbosity 5 \
--bft \
--allow-empty \
--num-validators 4 \
--node-num 0
