// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.
package bft

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	btypes "github.com/ethereum/go-ethereum/consensus/bft/types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/params"
)

const (
	softResponseLimit = 2 * 1024 * 1024 // Target maximum size of returned blocks, headers or node data.
	estHeaderRlpSize  = 500             // Approximate size of an RLP encoded block header
)

var (
	daoChallengeTimeout = 15 * time.Second // Time allowance for a node to reply to the DAO handshake challenge
)

// errIncompatibleConfig is returned if the requested protocols and configs are
// not compatible (low protocol version restrictions and high requirements).
var errIncompatibleConfig = errors.New("incompatible configuration")

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}

type ProtocolManager struct {
	networkId uint64

	txpool      txPool
	blockchain  *core.BlockChain
	chaindb     ethdb.Database
	chainconfig *params.ChainConfig
	vmConfig    vm.Config

	peers *peerSet

	SubProtocols []p2p.Protocol

	eventMux *event.TypeMux

	// bft parameters
	bftdb              ethdb.Database // bft database
	validators         []common.Address
	consensusManager   *ConsensusManager
	consensusContract  *ConsensusContract
	privateKeyHex      string
	addTransactionLock sync.Mutex
	eventMu            sync.Mutex
}

// NewProtocolManager returns a new ethereum sub protocol manager. The Ethereum sub protocol manages peers capable
// with the ethereum network.
func NewProtocolManager(config *params.ChainConfig, networkId uint64, mux *event.TypeMux, txpool *core.TxPool, blockchain *core.BlockChain, chaindb ethdb.Database, bftdb ethdb.Database, vmConfig vm.Config, validators []common.Address, privateKeyHex string, etherbase common.Address, allowEmpty bool) (*ProtocolManager, error) {
	// Create the protocol manager with the base fields
	manager := &ProtocolManager{
		networkId:   networkId,
		eventMux:    mux,
		txpool:      txpool,
		blockchain:  blockchain,
		chaindb:     chaindb,
		chainconfig: config,
		peers:       newPeerSet(),
		vmConfig:    vmConfig,
	}

	manager.SubProtocols = make([]p2p.Protocol, 0, len(ProtocolVersions))
	for i, version := range ProtocolVersions {
		version := version // Closure for the run
		manager.SubProtocols = append(manager.SubProtocols, p2p.Protocol{
			Name:    ProtocolName,
			Version: version,
			Length:  ProtocolLengths[i],
			Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
				peer := manager.newPeer(int(version), p, rw)
				return manager.handle(peer)
			},
			NodeInfo: func() interface{} {
				return manager.NodeInfo()
			},
			PeerInfo: func(id discover.NodeID) interface{} {
				if p := manager.peers.Peer(fmt.Sprintf("%x", id[:8])); p != nil {
					return p.Info()
				}
				return nil
			},
		})
	}

	manager.bftdb = bftdb
	manager.privateKeyHex = privateKeyHex
	manager.validators = validators
	manager.consensusContract = NewConsensusContract(mux, etherbase, txpool, validators)
	manager.consensusManager = NewConsensusManager(manager, blockchain, bftdb, manager.consensusContract, manager.privateKeyHex)
	manager.consensusManager.isAllowEmptyBlocks = allowEmpty

	return manager, nil
}

func (pm *ProtocolManager) Start() {
	if !pm.consensusContract.isValidators(pm.consensusContract.coinbase) {
		log.Info("not Validator")
		return
	}
	go pm.announce()
}

func (pm *ProtocolManager) Stop() {
	log.Info("Stopping Ethereum protocol")
}

func (pm *ProtocolManager) newPeer(pv int, p *p2p.Peer, rw p2p.MsgReadWriter) *peer {
	return newPeer(pv, p, newMeteredMsgWriter(rw))
}

func (pm *ProtocolManager) removePeer(id string) {
	// Short circuit if the peer was already removed
	peer := pm.peers.Peer(id)
	if peer == nil {
		return
	}
	log.Debug("Removing Ethereum peer", "peer", id)

	// Unregister the peer from the downloader and Ethereum peer set
	if err := pm.peers.Unregister(id); err != nil {
		log.Error("Peer removal failed", "peer", id, "err", err)
	}
	// Hard disconnect at the networking layer
	if peer != nil {
		peer.Peer.Disconnect(p2p.DiscUselessPeer)
	}
}

func (pm *ProtocolManager) handle(p *peer) error {
	// Execute the Ethereum handshake
	td, head, genesis := pm.blockchain.Status()
	if err := p.Handshake(pm.networkId, td, head, genesis); err != nil {
		p.Log().Debug("Ethereum handshake failed", "err", err)
		return err
	}
	if rw, ok := p.rw.(*meteredMsgReadWriter); ok {
		rw.Init(p.version)
	}
	// Register the peer locally
	if err := pm.peers.Register(p); err != nil {
		p.Log().Error("Ethereum peer registration failed", "err", err)
		return err
	}
	defer pm.removePeer(p.id)

	for {
		if err := pm.handleBFTMsg(p); err != nil {
			p.Log().Debug("Ethereum message handling failed", "err", err)
			return err
		}
	}
}

func (pm *ProtocolManager) announce() {
	pm.eventMu.Lock()
	defer pm.eventMu.Unlock()
	for !pm.consensusManager.isReady() {
		if pm.consensusManager.synchronizer.Requested.Size() != 0 {
			time.Sleep(5 * time.Second)
		}
		log.Debug("consensusManager not ready ")
		pm.consensusManager.SendReady(false)
		time.Sleep(0.5 * 1000 * 1000 * 1000)
	}
	pm.consensusManager.SendReady(true)
	log.Debug("-----------------consensusManager Ready-------------------------")
}

func (pm *ProtocolManager) handleBFTMsg(p *peer) error {
	// Read the next message from the remote peer, and ensure it's fully consumed
	msg, err := p.rw.ReadMsg()
	// log.Info("Handle BFT Msg,", "msg", msg)
	if err != nil {
		return err
	}
	if msg.Size > ProtocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, ProtocolMaxMsgSize)
	}
	defer msg.Discard()

	// Handle the message depending on its contents
	switch {
	case msg.Code == StatusMsg:
		// Status messages should never arrive after the handshake
		return errResp(ErrExtraStatusMsg, "uncontrolled status message")
	case msg.Code == GetPrecommitLocksetsMsg:
		log.Debug("GetBlockProposalsMsg from:", p.id)
		var query []RequestNumber
		if err := msg.Decode(&query); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		var found []*btypes.PrecommitLockSet
		log.Debug("GetPrecommitLockSetsMsg request: ", query)
		for _, height := range query {
			if height.Number > pm.blockchain.CurrentBlock().NumberU64() {
				log.Info("Request future block")
				break
			}
			ls := pm.consensusManager.getPrecommitLocksetByHeight(height.Number)
			found = append(found, ls)
		}
		if len(found) != 0 {
			p.SendPrecommitLocksets(found)
		}

	case msg.Code == PrecommitLocksetMsg:
		var pls []*btypes.PrecommitLockSet
		if err := msg.Decode(&pls); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		pm.consensusManager.synchronizer.receivePrecommitLocksets(pls)

	case msg.Code == NewBlockProposalMsg:
		var bpData newBlockProposals
		if err := msg.Decode(&bpData); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		bp := bpData.BlockProposal
		if p.broadcastFilter.Has(bp.Hash()) {
			return nil
		}
		if isValid := pm.consensusManager.AddProposal(bp, p); isValid {
			pm.BroadcastBFTMsg(bp)
		} else {
			return nil
		}
	case msg.Code == VotingInstructionMsg:
		var viData votingInstructionData
		if err := msg.Decode(&viData); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		vi := viData.VotingInstruction
		if p.broadcastFilter.Has(vi.Hash()) {
			return nil
		}
		if isValid := pm.consensusManager.AddProposal(vi, p); isValid {
			pm.BroadcastBFTMsg(vi)
		}
	case msg.Code == VoteMsg:
		var vData voteData
		if err := msg.Decode(&vData); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		vote := vData.Vote

		if p.broadcastFilter.Has(vote.Hash()) {
			return nil
		}
		// log.Debug("receive vote with HR ", vote.Height, vote.Round)
		if isValid := pm.consensusManager.AddVote(vote, p); isValid {
			pm.BroadcastBFTMsg(vote)
		}
	case msg.Code == PrecommitVoteMsg:
		var vData precommitVoteData
		if err := msg.Decode(&vData); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		vote := vData.PrecommitVote

		if p.broadcastFilter.Has(vote.Hash()) {
			return nil
		}
		// log.Debug("receive precommit vote with HR ", vote.Height, vote.Round)
		if isValid := pm.consensusManager.AddPrecommitVote(vote, p); isValid {
			pm.BroadcastBFTMsg(vote)
		}
	case msg.Code == ReadyMsg:
		var r readyData
		if err := msg.Decode(&r); err != nil {
			log.Debug("err: ", err)
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		ready := r.Ready
		pm.consensusManager.AddReady(ready)
		pm.BroadcastBFTMsg(ready)
	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
}

func (pm *ProtocolManager) BroadcastBFTMsg(msg interface{}) {
	// TODO: expect origin
	var err error
	switch m := msg.(type) {
	case *btypes.Ready:
		peers := pm.peers.PeersWithoutHash(m.Hash())
		// log.Debug("There are ", "peer count", len(peers))
		for _, peer := range peers {
			log.Info("send Ready msg")
			err = peer.SendReadyMsg(m)
			if err != nil {
				log.Debug("err: ", err)
			}
		}
	case *btypes.BlockProposal:
		peers := pm.peers.PeersWithoutHash(m.Hash())
		// log.Info("Send Bp: ", m)
		for _, peer := range peers {
			peer.SendNewBlockProposal(m)
		}
	case *btypes.VotingInstruction:
		peers := pm.peers.PeersWithoutHash(m.Hash())

		for _, peer := range peers {
			peer.SendVotingInstruction(m)
		}
	case *btypes.Vote:
		peers := pm.peers.PeersWithoutHash(m.Hash())
		for _, peer := range peers {
			peer.SendVote(m)
		}
	case *btypes.PrecommitVote:
		peers := pm.peers.PeersWithoutPrecommit(m.Hash())
		// log.Debug("peers to broadcast: ", len(peers))
		for _, peer := range peers {
			err := peer.SendPrecommitVote(m)
			if err != nil {
				log.Debug("err: ", err)
			}
		}
	default:
		log.Info("broadcast unknown type:", m)
	}
}

func (self *ProtocolManager) linkBlock(block *types.Block) *types.Block {
	self.addTransactionLock.Lock()
	defer self.addTransactionLock.Unlock()
	// _link_block
	if self.blockchain.HasBlock(block.Hash()) {
		log.Debug("KNOWN BLOCK")
		return nil
	}
	if !self.blockchain.HasBlock(block.ParentHash()) {
		log.Debug("missing parent")
		return nil
	}

	return block
}

func (self *ProtocolManager) validateBlock(block *types.Block) error {
	bc := self.blockchain
	parent := bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
	stateCache, err := bc.State()
	if err != nil {
		log.Error("Error occur when new statedb in validating block", "err", err)
		return err
	}

	// err = stateCache.Reset(parent.Root())
	receipts, _, usedGas, err := bc.Processor().Process(block, stateCache, self.vmConfig)
	if err != nil {
		log.Error("Error occur when process block in validating block", "err", err)
		return err
	}
	// Validate the state using the default validator
	err = bc.Validator().ValidateState(block, parent, stateCache, receipts, usedGas)
	if err != nil {
		log.Error("Error occur when validate state in validating block", "err", err)
		return err
	}

	return nil
}

// EthNodeInfo represents a short summary of the Ethereum sub-protocol metadata known
// about the host peer.
type EthNodeInfo struct {
	Network    uint64      `json:"network"`    // Ethereum network ID (1=Frontier, 2=Morden, Ropsten=3)
	Difficulty *big.Int    `json:"difficulty"` // Total difficulty of the host's blockchain
	Genesis    common.Hash `json:"genesis"`    // SHA3 hash of the host's genesis block
	Head       common.Hash `json:"head"`       // SHA3 hash of the host's best owned block
}

// NodeInfo retrieves some protocol metadata about the running host node.
func (self *ProtocolManager) NodeInfo() *EthNodeInfo {
	currentBlock := self.blockchain.CurrentBlock()
	return &EthNodeInfo{
		Network:    self.networkId,
		Difficulty: self.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64()),
		Genesis:    self.blockchain.Genesis().Hash(),
		Head:       currentBlock.Hash(),
	}
}
