../build/bin/geth \
\
--networkid 2234 \
--port 30305 \
--rpcport 8547 \
--datadir "bdata/node3" \
--nodiscover \
\
--rpc \
--rpccorsdomain "*" \
--rpcapi "eth,net,web3,debug" \
\
--bft \
--allow-empty \
--num-validators 4 \
--node-num 2
