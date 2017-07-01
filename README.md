# NCCU BFT Consensus for Go Ethereum (geth)
NCCU BFT Consensus for Go Ethereum is developed by the Blockchain group of Dept. of Computer Science at the National Chengchi University, Taiwan.

# Background
This project, initiated in August 2016, aims to develop a variant of geth with Byzantine Fault Tolerance (BFT) consensus for use in a private deployment of Ethereum. In the beginning, we followed the consensus approach of [Hydrachain](https://github.com/HydraChain/hydrachain) to develop our consensus protocol. A salient feature of Hydrachain consensus protocol is that a block can be committed immediately by a node participating in the consensus once the node intercepts a quorum for the block. This can be viewed as an optimization attempting to minimize the latency of a block.

However, as we had finished the implementation we soon realized that such an optimized commitment might be premature and leads to a fork. Both safety and liveness of consensus will be endangered, though we had submitted only [an issue of liveness](https://github.com/HydraChain/hydrachain/issues/83). Therefore, we changed our protocol by adding a phase that prevents premature commitment. The newly added phase is invoked after a node intercepts a quorum. The resulting protocol is then similar to the well known PBFT by Castro-Liskov (1999), as this newly added phase serves the same purpose as the Commit phase in PBFT.

Note that HydraChain is inspired by [Tendermint](https://tendermint.com/), a PBFT-like consensus protocol. Indeed, one can deem HydraChain as a simplification of Tendermint. Hence, the demand to add one more phase to HydryChain inevitably steers our focus to Tendermint. Indeed, our revised implementation still keeps the proof-of-lock-change or PoLC mechanism that was first proposed in Tendermint and inherited in Hydrachain to gain further efficiency in the consensus process during round change. 

Finally, right after we finished the implementation of the revised protocol, geth released a new version, 1.6.0, with a pluggable framework of consensus engine. As such an framework is also part of the original goal of this project, we adapted our implementation to a large degree to fit into the new consensus engine of geth and completed the current version of NCCU BFT consensus for geth.

# Build from source
Building geth requires both a Go (version 1.7 or later) and a C compiler. Once the dependencies are installed, run
```sh
make geth
```

# Running geth

Besides the flags geth support, there are three new command line flags to setup a BFT-consensus private chain:


  * `--bft` Change the consensus engine to NCCU-BFT consensus.
  * `--num_validators value` The number of the validators in this chain.
  * `--node_num value` The identity number of this node (start with 0).
  * `--allow_empty` Allow blocks without transaction.

To start a NCCU-BFT chain with 2 validators, run the following command after *init*
```sh
geth --datadir "path_for_node1" --bft --allow-empty --num-validators 2 --node-num 0
```
and 
```sh
geth --datadir "path_for_node2" --bft --allow-empty --num-validators 2 --node-num 1
```
make sure your nodes are connected by using cli or static-nodes.json file.

If your nodes are conneted, you could run the miner with both nodes for consensus.
```sh
miner.start()
```
You should see the result within logs.

You may specify the **--num_validators** to 1 and **--node_num** to 0 to start a private chain with BFT-consensus on only one node.

# Example

In examples/4nodes, there are the scripts to start a 4-nodes NCCU-BFT chain example. To start the chain, go to examples/4nodes and run

```sh
./start.sh
```

To stop the process

```sh
./stop.sh
```


# Consensus
We sketch our consensus protocol as follows and refer the readers to  [NCCU-BFT](https://github.com/NCCUCS-PLSM/NCCU-BFT-for-Go-Ethereum/blob/nccu-bft/docs/consensus_protocol_detail.pdf) for more details.
 
### Terminology
  - Block: An Ethereum block, denoted by B.
  - Height: The height of processing block’s height, denoted by H.
  - Round: A round includes the 4 consensus steps, denoted by R. There may be multiple R in a H.
  - Validators: The nodes that could participate the consensus process. The total number of validators is denoted by N.
  - Proposer: A validator that could propose a Proposal in a round.
 
### Vote
There are two kinds of vote:
 * Prevote(H, R, B)
 * PrecommitVote(H, R, B)

Validators vote a Prevote/PrecommitVote to a block B to present their states at height H and round R.
### Lockset
The Lockset collects the votes for consensus steps in height H and round R, there are two kinds of Lockset:
 * Prevote Lockset(H, R)
 * PrecommitVote Lockset(H, R)

If there are over ⅔ N vote to the same block B within a Lockset, it has a **Quorum** to the block B.
 
### Proposals
The proposer should propose a proposal containing a block B at the beginning of a round.
BlockProposal(H, R, B): A BlockProposal includes a new block B for voting at height H and round R.
VotingInstruction(H, R, B): Includes a block B and Prevote Lockset at previous round with a **Quorum**.
 
### States Diagram
![alt text](https://github.com/NCCUCS-PLSM/NCCU-BFT-for-Go-Ethereum/blob/nccu-bft/docs/states_diagram.png)
 
### Consensus steps
##### Step 1 *Propose*(height:H, round:R): 
The proposer of (H, R) should propose a proposal with a block.
1. Check whether the validator is the proposer. If not, go to Step 2.
2. If there is a **Quorum** to a block B1 in the Prevote Lockset at previous round, propose a VotingInstruction(B1), broadcast Prevote(H, R, B1), and go to Step 3.
3. Else Create a new block B2 and propose a BlockProposal(B2), broadcast  Prevote(H, R, B2), and go to Step 3.
 
##### Step 2 *Prevote*(height:H, round:R):
Each validator should broadcast a Prevote(H, R, B) for a block or nothing(nil).
1. If the validator voted a PrecommitVote(H, R, B1) at previous round, broadcast Prevote(H, R, B1).
2. Else if node receives a VotingInstruction(H, R, B2), broadcast Prevote(H, R, B2).
3. Else if node receives a BlockProposal(H, R, B3), broadcast Prevote(H, R, B3).
4. Else if TimeoutProposal reaches, broadcast Prevote(H, R, nil).
5. Go to Step 3.
 
##### Step 3 *Precommit*(height:H, round:R):
Each validator should broadcast a PrecommitVote(H, R, B) for a block or nothing(nil).
1. Collect Prevotes to Prevote Lockset(H, R).  
2. If there is a  **Quorum** to a block B in Prevote Lockset(H, R), broadcast PrecommitVote(H, R, B) and go to Step 4.
3. Else, wait until TimeoutProposalPrevote to store more Prevotes. if there is still no **Quorum**, broadcast PrecommitVote(H, R, nil) and go to Step 4.
 
##### Step 4 *Commit*(height:H, round:R):
Each validator checks the PrecommitVote Lockset(H, R) to determine whether it could commit or not.
1. Collect PrecommitVotes to PrecommitVote Lockset(H, R).   
2. If there is a **Quorum** to block B in PrecommitVote Lockset(H, R), then commit B and go to Step 1 of round 0, height H+1.
3. Else, wait for TimeoutPrecommitVote to store more PrecommitVotes in PrecommitVote Lockset(H, R). If there is still no **Quorum**, go to Step 1 of round R+1, height H.
 
### Proposer selection:
We use Round-Robin to change proposer in each round.
 
### Optimization:
In Step 3 and Step 4, if the validator gets a **Quorum**, it can proceed immediately. 
 
### Constants
  - NumberOfValidators: Number of validators in the network.
  - AllowEmpty: A boolean value to determine whether to allow empty block creating. 
  - TimeoutProposal: The Maximum time for waiting a proposal. This is set to 3 seconds initially.
  - TimeoutProposalPrevote: The maximum time for waiting more Prevote. This is set to 1 second initially.
  - TimeoutPrecommitVote: The maximum time for waiting more PrecommitVote. This is set to 1 seconds initially. 
  - TimeoutFactor: This is the factor used to extend the above timeouts after each round. More specifically, TimeoutX in   - round R = TimeoutX * TimeoutFactorR. TimeoutFactor is set to 1.5.
### Block header
We set the following two fields to constants.
  - difficulty: Always set to 1.
  - nonce: Always set to 0.
 
### Proof of Consensus
For inserting a block and synchronization, a block should have a Precommit Lockset that includes a **Quorum**, to prove that the block is validated by the consensus process. 
 
# Future work
Currently the validator set is fixed when starting geth. We plan to add some mechanisms to allow dynamic changes of validators. 

