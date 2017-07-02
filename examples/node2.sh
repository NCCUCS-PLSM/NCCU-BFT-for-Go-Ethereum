../build/bin/geth \
\
--networkid 2234 \
--port 30304 \
--rpcport 8546 \
--datadir "data/node2" \
--nodiscover \
\
--rpc \
--rpccorsdomain "*" \
--rpcapi "eth,net,web3,debug" \
\
--bft \
--allow-empty \
--num-validators 4 \
--node-num 1
