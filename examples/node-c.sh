../build/bin/geth \
\
--networkid 2234 \
--port 30308 \
--rpcport 8549 \
--datadir "data/nodec" \
--nodiscover \
\
--rpc \
--rpccorsdomain "*" \
--rpcapi "eth,net,debug" \
\
--bft \
--num-validators 4 \
--node-num 4
