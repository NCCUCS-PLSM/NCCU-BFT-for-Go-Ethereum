# Geth-w-BFT

Geth-w-BFT is a modulized Ethereum with BFT-like Consensus. This project is inspired by [HydraChain](https://github.com/HydraChain/hydrachain).

## Build from source
```sh
make geth
```
## Running geth

With all flags in origin geth, there are three new command line flags to setup a BFT-consensus private chain:


  * `--bft` Change PoW to BFT-consensus
  * `--num_validators` The number of the validators in this chain
  * `--node_num` The identity number of this node (start with 0)

You can specify the num_validators to 1 and node_num to 0 to start a private chain with BFT-consensus on only one node.

## Example

In examples/4nodes, there are the scripts to start a 4-nodes BFT chain. To start the chain, go to examples/4nodes and:

```sh
./start.sh
```

To stop:

```sh
./stop.sh
```

## Work In Progress

This project is still working in progress, and lots of part to improve.

  * The private key and address are currently created by program, but there shuold be a way to give.   
  * The chain's connection is based on static-nodes.json. There should be a more convenient way.
