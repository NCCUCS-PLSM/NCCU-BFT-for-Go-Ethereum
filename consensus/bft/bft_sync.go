package bft

import (
	"sync"

	"github.com/ethereum/go-ethereum/consensus/bft/types"
	"github.com/ethereum/go-ethereum/log"
	"gopkg.in/fatih/set.v0"
)

type Synchronizer struct {
	timeout              int
	maxGetProposalsCount int
	maxQueued            int
	cm                   *ConsensusManager
	Requested            *set.Set
	Received             *set.Set
	lastActiveProtocol   *peer
	addProposalLock      sync.Mutex
}

func NewSynchronizer(cm *ConsensusManager) *Synchronizer {
	return &Synchronizer{
		timeout:              5,
		cm:                   cm,
		Requested:            set.New(),
		Received:             set.New(),
		maxGetProposalsCount: MaxGetproposalsCount,
		maxQueued:            MaxGetproposalsCount * 3,
	}
}

// func (self *HDCSynchronizer) Missing() []types.RequestProposalNumber {
// 	self.cm.getHeightMu.Lock()
// 	ls := self.cm.lastCommittingLockset().Copy()
// 	self.cm.getHeightMu.Unlock()
// 	if ls == nil {
// 		log.Debug("no highest comitting lockest")
// 		return []types.RequestProposalNumber{}
// 	}
// 	maxHeight := ls.Height()
// 	current := self.cm.Head().Number()
// 	log.Debug("max height: %d current: %d\n", maxHeight, current)

// 	if maxHeight < current.Uint64() {
// 		return []types.RequestProposalNumber{}
// 	}
// 	var missing []types.RequestProposalNumber

// 	for i := current.Uint64() + 1; i < maxHeight+1; i++ {
// 		missing = append(missing, types.RequestProposalNumber{i})
// 	}
// 	return missing
// }

func (self *Synchronizer) request(height uint64) bool {
	var blockNumbers []RequestNumber

	blockNumbers = append(blockNumbers, RequestNumber{height})

	// if self.lastActiveProtocol != nil {
	// 	err := self.lastActiveProtocol.RequestPrecommitLocksets(blockNumbers)
	// 	log.Debug("request end", "err", err)
	// } else {
	peer := self.cm.pm.peers.BestPeer()
	peer.RequestPrecommitLocksets(blockNumbers)
	self.Requested.Add(height)
	log.Debug("no active protocol")
	// }
	return true
}
func (self *Synchronizer) receivePrecommitLocksets(pls []*types.PrecommitLockSet) {
	for _, ls := range pls {
		if result, hash := ls.HasQuorum(); result == true {
			self.Requested.Remove(ls.Height())
			self.cm.storePrecommitLockset(hash, ls)
		} else {
			log.Error("receive PrecommitLocksets invalid")
			return
		}
	}
}

// func (self *HDCSynchronizer) receiveBlockproposals(bps []*types.BlockProposal) {
// 	for _, bp := range bps {
// 		log.Info("received Blocks", bp.Height)
// 		self.Received.Add(bp.Height)
// 		self.Requested.Remove(bp.Height)
// 		for _, v := range bp.SigningLockset.PrecommitVotes {
// 			self.cm.AddPrecommitVote(v, nil)
// 		}
// 	}
// 	// self.cm.Process()
// 	self.request()
// 	for _, bp := range bps {
// 		log.Info("add Bps", bp)
// 		self.cm.AddProposal(bp, nil)
// 		// self.cm.Process()
// 	}
// 	self.cleanup()
// }

func (self *Synchronizer) onProposal(proposal types.Proposal, p *peer) {
	log.Debug("synchronizer on proposal")
	if proposal.GetHeight() >= self.cm.Height() {
		if !proposal.LockSet().IsValid() && proposal.LockSet().EligibleVotesNum != 0 {
			panic("onProposal error")
		}
		self.lastActiveProtocol = p
	}
}

// func (self *HDCSynchronizer) process() {
// 	self.request()
// }

func (self *Synchronizer) cleanup() {
	// set.List() may have error
	height := self.cm.Height()
	for _, v := range self.Received.List() {
		if v.(uint64) < height {
			self.Received.Remove(v)
		}
	}
	for _, v := range self.Requested.List() {
		if v.(uint64) < height {
			self.Requested.Remove(v)
		}
	}
}
