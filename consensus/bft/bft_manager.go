package bft

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	btypes "github.com/ethereum/go-ethereum/consensus/bft/types"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	TimeoutRound     = 3 // basic timeout time for
	TimeoutPrecommit = 0.5
	TimeoutFactor    = 1.5
)

type ConsensusContract struct {
	eventMux   *event.TypeMux
	coinbase   common.Address
	txpool     *core.TxPool
	validators []common.Address
}

func NewConsensusContract(eventMux *event.TypeMux, coinbase common.Address, txpool *core.TxPool, validators []common.Address) *ConsensusContract {
	return &ConsensusContract{
		eventMux:   eventMux,
		txpool:     txpool,
		coinbase:   coinbase,
		validators: validators,
	}
}

func chosen(h uint64, r uint64, length int) int {
	sum := h - r
	return int(math.Abs(float64(sum))) % length
}

func (cc *ConsensusContract) proposer(height uint64, round uint64) common.Address {
	addr := cc.validators[chosen(height, round, len(cc.validators))]
	return addr
}

func (cc *ConsensusContract) isValidators(v common.Address) bool {
	return containsAddress(cc.validators, v)
}

func (cc *ConsensusContract) isProposer(p btypes.Proposal) bool {
	if addr, err := p.From(); err != nil {
		log.Error("invalid sender %v", err)
		return false
	} else {
		return addr == cc.proposer(p.GetHeight(), p.GetRound())
	}

}

func (cc *ConsensusContract) numEligibleVotes(height uint64) uint64 {
	if height == 0 {
		return 0
	} else {
		return uint64(len(cc.validators))
	}
}

func containsAddress(s []common.Address, e common.Address) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

type StrategyConfig struct {
	DifferentProposal bool
	AlwaysVote        bool
	AlwaysAgree       bool
	NoResponse        bool
}

type ConsensusManager struct {
	pm                      *ProtocolManager
	isAllowEmptyBlocks      bool
	numInitialBlocks        uint64
	roundTimeout            uint64
	roundTimeoutFactor      float64
	transactionTimeout      float64
	chain                   *core.BlockChain
	coinbase                common.Address
	readyValidators         map[common.Address]struct{}
	privkey                 *ecdsa.PrivateKey
	contract                *ConsensusContract
	trackedProtocolFailures []string
	heights                 map[uint64]*HeightManager
	proposalLock            *types.Block
	readyNonce              uint64
	blockCandidates         map[common.Hash]*btypes.BlockProposal
	hdcDb                   ethdb.Database
	synchronizer            *Synchronizer
	// lastCommittingLockset   *btypes.LockSet

	currentBlock *types.Block
	found        chan *types.Block

	mu          sync.Mutex
	currentMu   sync.Mutex
	uncleMu     sync.Mutex
	writeMapMu  sync.RWMutex
	getHeightMu sync.RWMutex

	processMu sync.Mutex

	Enable bool
	Config StrategyConfig
}

func NewConsensusManager(manager *ProtocolManager, chain *core.BlockChain, db ethdb.Database, cc *ConsensusContract, privkeyhex string) *ConsensusManager {

	privkey, _ := crypto.HexToECDSA(privkeyhex)
	cm := &ConsensusManager{
		pm:                 manager,
		isAllowEmptyBlocks: false,
		numInitialBlocks:   10,
		roundTimeout:       3,
		roundTimeoutFactor: 1.5,
		transactionTimeout: 0.5,
		hdcDb:              db,
		chain:              chain,
		privkey:            privkey,
		readyValidators:    make(map[common.Address]struct{}),
		heights:            make(map[uint64]*HeightManager),
		readyNonce:         0,
		blockCandidates:    make(map[common.Hash]*btypes.BlockProposal),
		contract:           cc,
		coinbase:           cc.coinbase,
		Enable:             true,
		getHeightMu:        sync.RWMutex{},
		Config:             StrategyConfig{false, false, false, false},
	}

	cm.initializeLocksets()

	// old votes don't count
	cm.readyValidators = make(map[common.Address]struct{})
	cm.readyValidators[cm.coinbase] = struct{}{}

	cm.synchronizer = NewSynchronizer(cm)
	return cm
}

// properties
func (cm *ConsensusManager) Head() *types.Block {
	return cm.chain.CurrentBlock()
}

func (cm *ConsensusManager) Now() int64 {
	return time.Now().Unix()
}

func (cm *ConsensusManager) Height() uint64 {
	h := cm.chain.CurrentBlock().NumberU64()
	return h + 1
}

func (cm *ConsensusManager) Round() uint64 {
	return cm.getHeightManager(cm.Height()).Round()
}

func (cm *ConsensusManager) getHeightManager(h uint64) *HeightManager {
	if _, ok := cm.heights[h]; !ok {
		cm.heights[h] = NewHeightManager(cm, h)
	}
	return cm.heights[h]
}

func (cm *ConsensusManager) activeRound() *RoundManager {
	hm := cm.getHeightManager(cm.Height())
	return hm.getRoundManager(hm.Round())
}

func (cm *ConsensusManager) enable() {
	cm.Enable = true
}

func (cm *ConsensusManager) disable() {
	cm.Enable = false
}

func (cm *ConsensusManager) initializeLocksets() {
	// initializing locksets
	// sign genesis
	log.Debug("initialize locksets")
	v := btypes.NewPrecommitVote(0, 0, cm.chain.Genesis().Hash(), 1) // voteBlock

	cm.Sign(v)
	cm.AddPrecommitVote(v, nil)
	// add initial lockset
	log.Debug("inintial lockset")
	lastCommittingLockset := cm.loadLastCommittingLockset()
	if lastCommittingLockset != nil {
		_, hash := lastCommittingLockset.HasQuorum()
		if hash != cm.Head().Hash() {
			log.Error("initialize_locksets error: hash not match")
			return
		}
		for _, v := range lastCommittingLockset.PrecommitVotes {
			cm.AddPrecommitVote(v, nil)
		}
	}
}

// persist proposals and last committing lockset
func (cm *ConsensusManager) storeLastCommittingLockset(ls *btypes.PrecommitLockSet) error {
	bytes, err := rlp.EncodeToBytes(ls)
	if err != nil {
		return err
	}
	if err := cm.hdcDb.Put([]byte("last_committing_lockset"), bytes); err != nil {
		log.Error("failed to store last committing lockset into database", "err", err)
		return err
	}
	return nil
}

func (cm *ConsensusManager) loadLastCommittingLockset() *btypes.PrecommitLockSet {
	key := fmt.Sprintf("last_committing_lockset")
	data, _ := cm.hdcDb.Get([]byte(key))
	if len(data) == 0 {
		return nil
	}
	var lockset *btypes.PrecommitLockSet
	if err := rlp.Decode(bytes.NewReader(data), &lockset); err != nil {
		log.Error("invalid last_committing_lockset ", "err:", err)
		return nil
	}
	return lockset
}

func (cm *ConsensusManager) storePrecommitLockset(blockhash common.Hash, pls *btypes.PrecommitLockSet) error {
	bytes, err := rlp.EncodeToBytes(pls)
	if err != nil {
		panic(err)
	}
	key := fmt.Sprintf("precommitLockset:%s", blockhash)
	if err := cm.hdcDb.Put([]byte(key), bytes); err != nil {
		log.Error("failed to store proposal into database", "err", err)
		return err
	}
	return nil
}

func (cm *ConsensusManager) loadPrecommitLockset(blockhash common.Hash) *btypes.PrecommitLockSet {
	key := fmt.Sprintf("precommitLockset:%s", blockhash)
	data, _ := cm.hdcDb.Get([]byte(key))
	if len(data) == 0 {
		return nil
	}
	var pls *btypes.PrecommitLockSet
	if err := rlp.Decode(bytes.NewReader(data), &pls); err != nil {
		log.Error("invalid precommitLockset RLP for hash", "blockhash", blockhash, "err", err)
		return nil
	}
	return pls
}

func (cm *ConsensusManager) getPrecommitLocksetByHeight(height uint64) *btypes.PrecommitLockSet {
	if height >= cm.Height() {
		log.Error("getPrecommitLocksetByHeight error")
		return nil
	} else {
		bh := cm.chain.GetBlockByNumber(uint64(height)).Hash()
		return cm.loadPrecommitLockset(bh)
	}
}

func (cm *ConsensusManager) setupTimeout(h uint64) {
	cm.getHeightMu.Lock()
	ar := cm.activeRound()
	if cm.isWaitingForProposal() {
		delay := ar.getTimeout()
		// if timeout is setup already, skip
		if delay > 0 {
			log.Debug("delay time :", "delay", delay)
		}
	}
	cm.getHeightMu.Unlock()

}

func (cm *ConsensusManager) isWaitingForProposal() bool {
	return cm.isAllowEmptyBlocks || cm.hasPendingTransactions() || cm.Height() <= cm.numInitialBlocks
}

func (cm *ConsensusManager) hasPendingTransactions() bool {
	if txs, err := cm.pm.txpool.Pending(); err != nil {
		log.Debug("error occur")
		panic(err)
	} else {
		return len(txs) > 0
	}
}

func (cm *ConsensusManager) Process(block *types.Block, abort chan struct{}, found chan *types.Block) {
	if !cm.contract.isValidators(cm.coinbase) {
		log.Info("Node is Not a Validator")
		return
	}

	if pls := cm.lastCommittingLockset(); pls != nil {
		cm.storeLastCommittingLockset(pls)
	}

	cm.currentBlock = block
	cm.found = found
	cm.enable()

	if cm.Height() != block.Number().Uint64() || !cm.Enable {
		return
	}
	for cm.Enable {
		select {
		case <-abort:
			cm.currentBlock = nil
			// cm.found = nil
			return
		default:
			cm.process()
			time.Sleep(100 * time.Millisecond)

		}
	}
}

func (cm *ConsensusManager) process() {
	if !cm.isReady() {
		log.Debug("---------------not ready------------------")
		// cm.setupAlarm(h)
		return
	} else {
		log.Debug("---------------process------------------")
		cm.setupTimeout(cm.Height())
		cm.getHeightMu.Lock()
		heightManager := cm.getHeightManager(cm.Height())
		log.Debug("hm process")
		heightManager.process()
		cm.getHeightMu.Unlock()
		cm.cleanup()

	}
}

func (cm *ConsensusManager) commitPrecommitLockset(hash common.Hash, pls *btypes.PrecommitLockSet) {
	cm.writeMapMu.Lock()
	defer cm.writeMapMu.Unlock()
	proposal, ok := cm.blockCandidates[hash]
	if ok {
		if proposal.Block.ParentHash() != cm.Head().Hash() {
			log.Debug("wrong parent hash: ", proposal.Block.ParentHash(), cm.Head().Hash())
			return
		}
		if pls != nil {
			_, hash := pls.HasQuorum()
			if proposal.Blockhash() == hash {
				if cm.found != nil {
					log.Debug("cm.found is not nil")
					select {
					case cm.found <- proposal.Block:
						log.Debug("store precommit lockset")
						cm.storePrecommitLockset(hash, pls)
						cm.disable()
					default:
						log.Debug("no chan")
					}
				} else {
					log.Debug("cm.found is nil")
				}
				return
			}
		}
	} else {
		if pls != nil {
			result, hash := pls.HasQuorum()
			if result {
				log.Debug("store precommit lockset")
				cm.storePrecommitLockset(hash, pls)
			}
		}
	}
}

func (cm *ConsensusManager) verifyVotes(header *types.Header) error {
	number := header.Number.Uint64()
	blockhash := header.Hash()

	if pls := cm.loadPrecommitLockset(blockhash); pls != nil {
		_, hash := pls.HasQuorum()
		if blockhash == hash {
			return nil
		} else {
			log.Error("verify Votes Error Occur")
			return errors.New("store PrecommitLockset hash is not the same")
		}
	} else {
		log.Debug("verify Votes Failed, sync with others")
		cm.synchronizer.request(number)
		time.Sleep(500 * 1000 * 1000) // wait for request from others
		return cm.verifyVotes(header)
	}
}

func (cm *ConsensusManager) cleanup() {
	// log.Debug("in cleanup,current Head Number is ", "number", cm.Head().Header().Number.Uint64())
	cm.writeMapMu.Lock()
	for hash, p := range cm.blockCandidates {
		if cm.Head().Header().Number.Uint64() >= p.GetHeight() {
			delete(cm.blockCandidates, hash)
		}
	}
	cm.writeMapMu.Unlock()
	cm.getHeightMu.Lock()
	for i, _ := range cm.heights {
		if cm.getHeightManager(i).height < cm.Head().Header().Number.Uint64() {
			////DEBUG
			log.Debug("Delete BlockCandidte", i)
			delete(cm.heights, i)
		}
	}
	cm.getHeightMu.Unlock()
}

func (cm *ConsensusManager) Sign(s interface{}) {
	log.Debug("CM Sign")
	switch t := s.(type) {
	case *btypes.BlockProposal:
		t.Sign(cm.privkey)
	case *btypes.Vote:
		t.Sign(cm.privkey)
	case *btypes.PrecommitVote:
		t.Sign(cm.privkey)
	case *btypes.LockSet:
		t.Sign(cm.privkey)
	case *btypes.PrecommitLockSet:
		t.Sign(cm.privkey)
	case *btypes.VotingInstruction:
		t.Sign(cm.privkey)
	case *btypes.Ready:
		t.Sign(cm.privkey)
	default:
		log.Debug("consensus mangaer sign error")
	}
}

func (cm *ConsensusManager) setProposalLock(block *types.Block) {
	// TODO: update this
	cm.proposalLock = block
}

func (cm *ConsensusManager) broadcast(msg interface{}) {
	cm.pm.BroadcastBFTMsg(msg)
}

func (cm *ConsensusManager) isReady() bool {
	return float32(len(cm.readyValidators)) > float32(len(cm.contract.validators))*2.0/3.0
}

func (cm *ConsensusManager) SendReady(force bool) {

	if cm.isReady() && !force {
		return
	}
	ls := cm.activeRound().lockset
	r := btypes.NewReady(cm.readyNonce, ls)
	cm.Sign(r)
	r.From()
	cm.broadcast(r)
	cm.readyNonce += 1
}

func (cm *ConsensusManager) AddReady(ready *btypes.Ready) {
	cc := cm.contract
	addr, err := ready.From()
	if err != nil {
		log.Error("AddReady err ", "err", err)
		return
	}
	if !cc.isValidators(addr) {
		log.Debug(addr.Hex())
		log.Debug("receive ready from invalid sender")
		return
	}
	if _, ok := cm.readyValidators[addr]; !ok {
		cm.writeMapMu.Lock()
		cm.readyValidators[addr] = struct{}{}
		cm.writeMapMu.Unlock()
	}
}

func (cm *ConsensusManager) AddVote(v *btypes.Vote, peer *peer) bool {
	if v == nil {
		log.Debug("cm addvote error")
		return false
	}
	addr, _ := v.From()
	if _, ok := cm.readyValidators[addr]; !ok {
		cm.writeMapMu.Lock()
		cm.readyValidators[addr] = struct{}{}
		cm.writeMapMu.Unlock()
	}
	cm.getHeightMu.Lock()
	h := cm.getHeightManager(v.Height)
	success := h.addVote(v, true)
	log.Debug("addVote to ", "height", v.Height, "round", v.Round, "from", addr, "success", success)

	cm.getHeightMu.Unlock()
	return success
}

func (cm *ConsensusManager) AddPrecommitVote(v *btypes.PrecommitVote, peer *peer) bool {
	if v == nil {
		log.Debug("cm AddPrecommitVote fail")
		return false
	}
	// log.Debug("addVote", v.From())
	cm.getHeightMu.Lock()
	h := cm.getHeightManager(v.Height)
	success := h.addPrecommitVote(v, true)
	cm.getHeightMu.Unlock()
	return success
}

func (cm *ConsensusManager) AddProposal(p btypes.Proposal, peer *peer) bool {
	if p == nil {
		panic("nil peer in cm AddProposal")
	}

	if p.GetHeight() < cm.Height() {
		log.Debug("proposal from past")
		return false
	}
	addr, err := p.From()
	if err != nil {
		log.Debug("proposal sender error ", "err", err)
		return false
	}
	if !cm.contract.isValidators(addr) || !cm.contract.isProposer(p) {
		log.Debug("proposal sender invalid", "validator?", cm.contract.isValidators(addr), "proposer?", cm.contract.isProposer(p))
		return false
	}
	if _, ok := cm.readyValidators[addr]; !ok {
		cm.writeMapMu.Lock()
		cm.readyValidators[addr] = struct{}{}
		cm.writeMapMu.Unlock()
	}
	// if proposal is valid
	ls := p.LockSet()
	if !ls.IsValid() && ls.EligibleVotesNum != 0 {
		log.Debug("proposal invalid")
		return false
	} else {
		if p.GetRound() != 0 {
			if ls.Height() != p.GetHeight() {
				log.Debug("proposal invalid, height not the same or not the first round")
				return false
			}
			if p.GetRound()-ls.Round() != 1 {
				log.Debug("proposal invalid, ")
				return false
			}
		}
	}

	switch proposal := p.(type) {
	case *btypes.BlockProposal:
		// log.Debug("adding bp in :", proposal.Height, proposal.Round, proposal.Blockhash())
		if err := cm.pm.validateBlock(proposal.Block); err != nil {
			log.Error("Received proposal block is invalid")
		}

		if peer != nil {
			cm.synchronizer.onProposal(p, peer)
		}

		if proposal.Block.Number().Uint64() != proposal.Height {
			log.Debug("proposal different height")
			return false
		}
		if proposal.Round != 0 && !ls.NoQuorum() {
			log.Debug("proposal invalid: round !=0 & not noquorum")
			return false
		}
		if quorum, _ := proposal.SigningLockset.HasQuorum(); !quorum {
			log.Debug("signing lockset error")
			return false
		}
		// if proposal.Height > cm.Height() {
		// 	log.Debug("proposal from the future")
		// 	return false
		// }
		cm.addBlockProposal(proposal)
	case *btypes.VotingInstruction:
		if !(proposal.LockSet().Round() == proposal.Round-1 && proposal.Height == proposal.LockSet().Height()) {
			log.Debug("Invalid VotingInstruction")
			return false
		} else if proposal.Round == 0 {
			log.Debug("Invalid VotingInstruction")
			return false
		} else if result, _ := proposal.LockSet().HasQuorum(); !result {
			log.Debug("Invalid VotingInstruction")
			return false
		}
	}
	cm.getHeightMu.Lock()
	isValid := cm.getHeightManager(p.GetHeight()).addProposal(p)
	cm.getHeightMu.Unlock()
	return isValid
}

func (cm *ConsensusManager) addBlockProposal(bp *btypes.BlockProposal) bool {
	log.Debug("cm add BlockProposal", "h", bp.Height, "r", bp.Round)

	result, _ := bp.SigningLockset.HasQuorum()
	slH := bp.SigningLockset.Height()
	if !result || slH != bp.Height-1 {
		log.Debug("Error: proposal error")
		return false
	}
	cm.getHeightMu.Lock()
	h := cm.getHeightManager(slH)
	for _, v := range bp.SigningLockset.PrecommitVotes {
		h.addPrecommitVote(v, false)
	}
	cm.getHeightMu.Unlock()
	cm.addBlockCandidates(bp)
	return true
}

func (cm *ConsensusManager) addBlockCandidates(bp *btypes.BlockProposal) {
	cm.writeMapMu.Lock()
	cm.blockCandidates[bp.Blockhash()] = bp
	cm.writeMapMu.Unlock()
}

func (cm *ConsensusManager) lastCommittingLockset() *btypes.PrecommitLockSet {
	ls := cm.getHeightManager(cm.Height() - 1).lastQuorumPrecommitLockSet()
	if ls == nil {
		return nil
	}
	return ls
}

func (cm *ConsensusManager) HighestCommittingLockset() *btypes.PrecommitLockSet {
	var hcls *btypes.PrecommitLockSet
	hcls = nil
	for i, height := range cm.heights {
		ls := height.lastQuorumPrecommitLockSet()
		if ls != nil {
			if hcls == nil {
				hcls = ls
			} else if i > hcls.Height() {
				hcls = ls
			}
		}
	}
	return hcls
}

func (cm *ConsensusManager) lastValidLockset() *btypes.LockSet {
	// log.Debug("cm lastValidLockset ")

	ls := cm.getHeightManager(cm.Height()).lastValidLockset()
	return ls
}

func (cm *ConsensusManager) lastValidPrecommitLockset() *btypes.PrecommitLockSet {
	// log.Debug("cm lastValidPrecommitLockset ")
	ls := cm.getHeightManager(cm.Height()).lastValidPrecommitLockset()
	return ls
}

func (cm *ConsensusManager) lastLock() *btypes.Vote {
	return cm.getHeightManager(cm.Height()).LastVoteLock()
}

func (cm *ConsensusManager) mkLockSet(height uint64) *btypes.LockSet {
	return btypes.NewLockSet(cm.contract.numEligibleVotes(height), []*btypes.Vote{})
}

func (cm *ConsensusManager) mkPLockSet(height uint64) *btypes.PrecommitLockSet {
	return btypes.NewPrecommitLockSet(cm.contract.numEligibleVotes(height), []*btypes.PrecommitVote{})
}

type HeightManager struct {
	cm          *ConsensusManager
	height      uint64
	rounds      map[uint64]*RoundManager
	writeMapMu  sync.RWMutex
	activeRound uint64
}

func NewHeightManager(consensusmanager *ConsensusManager, height uint64) *HeightManager {
	return &HeightManager{
		cm:          consensusmanager,
		height:      height,
		rounds:      make(map[uint64]*RoundManager),
		writeMapMu:  sync.RWMutex{},
		activeRound: 0,
	}
}

func (hm *HeightManager) Round() uint64 {

	// l := hm.lastValidPrecommitLockset()
	// if l != nil {
	// 	if l.IsValid() {
	// 		// log.Debug("hm Round()", l.Round()+1)
	// 		return l.Round() + 1
	// 	}
	// }
	return hm.activeRound
}

func (hm *HeightManager) getRoundManager(r uint64) *RoundManager {
	hm.writeMapMu.Lock()
	defer hm.writeMapMu.Unlock()
	if _, ok := hm.rounds[r]; !ok {
		hm.rounds[r] = NewRoundManager(hm, r)
	}
	return hm.rounds[r]
}

func (hm *HeightManager) LastVoteLock() *btypes.Vote {
	// highest lock
	for i := len(hm.rounds) - 1; i >= 0; i-- {
		index := uint64(i)
		if hm.getRoundManager(index).voteLock != nil {
			return hm.getRoundManager(index).voteLock
		}
	}
	return nil
}

func (hm *HeightManager) LastPrecommitVoteLock() *btypes.PrecommitVote {
	// highest lock
	for i := len(hm.rounds) - 1; i >= 0; i-- {
		index := uint64(i)
		if hm.getRoundManager(index).voteLock != nil {
			return hm.getRoundManager(index).precommitVoteLock
		}
	}
	return nil
}

func (hm *HeightManager) LastVotedBlockProposal() *btypes.BlockProposal {
	// the last block proposal node voted on
	for i := len(hm.rounds) - 1; i >= 0; i-- {
		index := uint64(i)
		switch p := hm.getRoundManager(index).proposal.(type) {
		case *btypes.BlockProposal:
			v := hm.getRoundManager(index).voteLock
			if p.Blockhash() == v.Blockhash {
				return p
			}
		default:
			return nil
		}
	}
	return nil
}

func (hm *HeightManager) lastValidLockset() *btypes.LockSet {
	// highest valid lockset on height
	for i := len(hm.rounds) - 1; i >= 0; i-- {
		index := uint64(i)
		// log.Debug("lastvalidlockset i", i)
		if hm.getRoundManager(index).lockset.IsValid() {
			return hm.getRoundManager(index).lockset
		}
	}
	return nil
}

func (hm *HeightManager) lastValidPrecommitLockset() *btypes.PrecommitLockSet {
	// highest valid lockset on height
	for i := len(hm.rounds) - 1; i >= 0; i-- {
		index := uint64(i)
		if hm.getRoundManager(index).precommitLockset.IsValid() {
			return hm.getRoundManager(index).precommitLockset
		}
	}
	return nil
}

// PoLC_Lockset
func (hm *HeightManager) lastQuorumLockset() *btypes.LockSet {
	var found *btypes.LockSet
	for i := 0; i < len(hm.rounds); i++ {
		index := uint64(i)
		ls := hm.getRoundManager(index).lockset
		if ls.IsValid() {
			result, hash := ls.HasQuorum()
			if result {
				if found != nil {
					log.Info("height: ", hm.height, index)
					if _, h := found.HasQuorum(); h != hash {
						log.Info("multiple valid lockset")
					}
				}
				found = ls
			}
		}
	}
	return found
}

func (hm *HeightManager) lastQuorumPrecommitLockSet() *btypes.PrecommitLockSet {
	var found *btypes.PrecommitLockSet
	for i := 0; i < len(hm.rounds); i++ {
		index := uint64(i)
		ls := hm.getRoundManager(index).precommitLockset
		if ls.IsValid() {
			result, hash := ls.HasQuorum()
			if result {
				if found != nil {
					log.Info("multiple valid lockset on precommit lockset")
					if _, h := found.HasQuorum(); h != hash {
						log.Info("multiple valid lockset")
						panic("multiple valid locksets on different proposals")
					}
				}
				found = ls
			}
		}
	}
	return found
}

func (hm *HeightManager) HasQuorum() (bool, common.Hash) {
	ls := hm.lastQuorumPrecommitLockSet()
	if ls != nil {
		return ls.HasQuorum()
	} else {
		return false, common.Hash{}
	}
}

func (hm *HeightManager) addVote(v *btypes.Vote, process bool) bool {
	addr, _ := v.From()
	if !hm.cm.contract.isValidators(addr) {
		log.Debug("non-validator vote")
		return false
	}
	isOwnVote := (addr == hm.cm.contract.coinbase)
	r := v.Round
	return hm.getRoundManager(r).addVote(v, isOwnVote, process)
}

func (hm *HeightManager) addPrecommitVote(v *btypes.PrecommitVote, process bool) bool {
	addr, _ := v.From()
	if !hm.cm.contract.isValidators(addr) {
		log.Debug("non-validator vote")
		return false
	}
	isOwnVote := (addr == hm.cm.contract.coinbase)
	r := v.Round
	return hm.getRoundManager(r).addPrecommitVote(v, isOwnVote, process)
}

func (hm *HeightManager) addProposal(p btypes.Proposal) bool {
	return hm.getRoundManager(p.GetRound()).addProposal(p)
}

func (hm *HeightManager) process() {
	////DEBUG
	r := hm.Round()

	hm.getRoundManager(r).process()
	////DEBUG
}

type RoundManager struct {
	hm                *HeightManager
	cm                *ConsensusManager
	round             uint64
	height            uint64
	lockset           *btypes.LockSet
	precommitLockset  *btypes.PrecommitLockSet
	proposal          btypes.Proposal
	voteLock          *btypes.Vote
	precommitVoteLock *btypes.PrecommitVote
	timeoutTime       float64
	timeoutPrecommit  float64
	roundProcessMu    sync.Mutex
}

func NewRoundManager(heightmanager *HeightManager, round uint64) *RoundManager {
	lockset := heightmanager.cm.mkLockSet(heightmanager.height)
	pLockset := heightmanager.cm.mkPLockSet(heightmanager.height)
	return &RoundManager{
		hm:                heightmanager,
		cm:                heightmanager.cm,
		round:             round,
		height:            heightmanager.height,
		lockset:           lockset,
		precommitLockset:  pLockset,
		timeoutTime:       0,
		timeoutPrecommit:  0,
		proposal:          nil,
		voteLock:          nil,
		precommitVoteLock: nil,
	}
}

func (rm *RoundManager) getTimeout() float64 {
	if rm.timeoutTime != 0 {
		return 0
	}
	now := rm.cm.Now()
	roundTimeout := rm.cm.roundTimeout
	roundTimeoutFactor := rm.cm.roundTimeoutFactor
	delay := float64(roundTimeout) * math.Pow(roundTimeoutFactor, float64(rm.round))
	rm.timeoutTime = float64(now) + delay
	log.Debug("RM gettimout", "height", rm.height, "round", rm.round)
	return delay
}

func (rm *RoundManager) setTimeoutPrecommit() {
	if rm.timeoutPrecommit != 0 {
		return
	}
	now := rm.cm.Now()
	timeout := 2
	timeoutFactor := 1.5
	delay := float64(timeout) * math.Pow(timeoutFactor, float64(rm.round))
	rm.timeoutPrecommit = float64(now) + delay
	log.Debug("RM get timeoutPrecommit", "height", rm.height, "round", rm.round)
}

func (rm *RoundManager) addVote(vote *btypes.Vote, force_replace bool, process bool) bool {
	// log.Debug("In RM addvote", "round", rm.round)
	if !rm.lockset.Contain(vote) {
		err := rm.lockset.Add(vote, force_replace)
		if err != nil {
			log.Error("err: ", "Add vote to lockset error", err)
			return false
		}
		return true
	}
	// log.Debug("vote already in lockset")
	return false
}

func (rm *RoundManager) addPrecommitVote(vote *btypes.PrecommitVote, force_replace bool, process bool) bool {
	if !rm.precommitLockset.Contain(vote) {
		addr, _ := vote.From()
		log.Debug("addPrecommitVote to ", "h", vote.Height, "r", vote.Round, "from", addr)
		err := rm.precommitLockset.Add(vote, force_replace)
		if err != nil {
			log.Debug("Add precommit vote to lockset error", err)
			return false
		}
		if result, hash := rm.precommitLockset.HasQuorum(); result {
			log.Debug("There is a quorum ", "height", rm.height, "round", rm.round)
			rm.cm.commitPrecommitLockset(hash, rm.precommitLockset)
		}
		return true
	}
	// log.Debug("precommitVote already in lockset")
	return false
}

func (rm *RoundManager) addProposal(p btypes.Proposal) bool {
	rm.roundProcessMu.Lock()
	defer rm.roundProcessMu.Unlock()

	// log.Debug("addProposal in ", rm.round, p)
	if rm.proposal == nil {
		rm.proposal = p
		return true
	} else if rm.proposal.Blockhash() == p.Blockhash() {
		return true
	} else {
		log.Debug("addProposal Error:", rm.proposal, p)
		return false
	}
}

func (rm *RoundManager) process() {
	rm.roundProcessMu.Lock()
	defer rm.roundProcessMu.Unlock()
	////DEBUG
	log.Debug("In RM Process", "height", rm.height, "round", rm.round)
	if rm.hm.Round() != rm.round {
		return
	}
	if rm.cm.Height() != rm.height {
		return
	}

	p := rm.propose()
	switch proposal := p.(type) {
	case *btypes.BlockProposal:
		if proposal != nil {
			rm.cm.addBlockCandidates(proposal)
			rm.cm.broadcast(proposal)
		}
	case *btypes.VotingInstruction:
		rm.cm.broadcast(proposal)
	default:
		log.Debug("propose nothing")
	}
	if rm.cm.Config.AlwaysAgree {
		if rm.voteLock == nil {
			log.Info("Vote byzantine votes")
			blockhash := rm.proposal.Blockhash()
			vote := btypes.NewVote(rm.height, rm.round, blockhash, 1)
			precommitVote := btypes.NewPrecommitVote(rm.height, rm.round, blockhash, 1)

			rm.cm.Sign(vote)
			rm.cm.Sign(precommitVote)

			rm.voteLock = vote
			rm.precommitVoteLock = precommitVote

			rm.addVote(vote, false, true)
			rm.addPrecommitVote(precommitVote, false, true)
		}
	}
	if rm.voteLock != nil {
		log.Debug("voteLock is not nil", "height", rm.height, "roound", rm.round)
	} else {
		v := rm.vote()
		if v != nil {
			rm.cm.broadcast(v)
		}
	}

	if rm.lockset.IsValid() {
		if rm.precommitVoteLock == nil {
			pv := rm.votePrecommit()
			if pv != nil {
				rm.cm.broadcast(pv)
			}
		} else {
			log.Debug("precommitVoteLock is not nil in ", "height", rm.height, "round", rm.round)
		}
	} else {
		log.Debug("rm lockset is not valid yet")
	}

	// wait no more precommit vote if timeout reached
	if rm.timeoutPrecommit != 0 && float64(rm.cm.Now()) >= rm.timeoutPrecommit && rm.precommitLockset.IsValid() {
		rm.hm.activeRound += 1
	}
}

func (rm *RoundManager) propose() btypes.Proposal {
	if !rm.cm.isWaitingForProposal() {
		log.Debug("proposing is not waiting for proposal")
		return nil
	}
	proposer := rm.cm.contract.proposer(rm.height, rm.round)
	if proposer != rm.cm.coinbase {
		log.Debug("I am not proposer in", "height", rm.height, "round", rm.round)
		return nil
	}
	log.Debug("I am a proposer in ", "height", rm.height, "round", rm.round)
	if rm.proposal != nil {
		addr, err := rm.proposal.From()
		if err != nil {
			log.Error("error occur %v", err)
			return nil
		}
		if addr != rm.cm.coinbase {
			addr, _ := rm.proposal.From()
			log.Error(addr.Hex(), rm.cm.coinbase.Hex())
			return nil
		}
		if rm.voteLock == nil {
			log.Error("Propose Error: voteLock nil")
			return nil
		}
		log.Debug("already propose in this HR", rm.height, rm.round)
		return rm.proposal
	}

	roundLockset := rm.cm.lastValidLockset()
	var proposal btypes.Proposal
	if rm.cm.Config.DifferentProposal == true {
		log.Debug("send two proposals")
		if bp := rm.mkProposal(); bp != nil {
			header := bp.Block.Header()
			header.Extra = []byte("Byzantine block")
			block := bp.Block.WithSeal(header)
			if bp2, err := btypes.NewBlockProposal(bp.Height, bp.Round, block, bp.SigningLockset, bp.RoundLockset); err != nil {
				rm.cm.Sign(bp2)
				rm.cm.pm.BroadcastTwoBlockProposal(bp, bp2)
			} else {
				log.Error("create bp2 occur error")
			}
			return nil
		} else {
			return nil
		}
	}

	if roundLockset == nil && rm.round == 0 {
		log.Debug("make proposal")
		if bp := rm.mkProposal(); bp != nil {
			proposal = bp
		} else {
			return nil
		}
	} else if roundLockset == nil {
		log.Error("no valid round lockset for height", "height", rm.height, "round", rm.round)
		return nil
	} else {
		quorum, _ := roundLockset.HasQuorum()
		if !quorum {
			if bp := rm.mkProposal(); bp != nil {
				proposal = bp
			} else {
				return nil
			}
		} else {
			if p, err := btypes.NewVotingInstruction(rm.height, rm.round, roundLockset); err != nil {
				log.Error("error occur %v", err)
				return nil
			} else {
				proposal = p
				rm.cm.Sign(proposal)
			}
		}
	}
	rm.proposal = proposal

	return proposal
}

func (rm *RoundManager) mkProposal() *btypes.BlockProposal {
	var roundLockset *btypes.LockSet
	signingLockset := rm.cm.lastCommittingLockset()
	if signingLockset == nil {
		// log.Error("error occur: no last committing lockset")
		return nil
	}
	if rm.round > 0 {
		lastPrecommitVoteLock := rm.hm.LastPrecommitVoteLock()
		if lastPrecommitVoteLock != nil {
			log.Error("error occur: MkProposal error, there is precommit votelock")
			return nil
		}
		roundLockset = rm.cm.lastValidLockset().Copy()
	} else {
		roundLockset = nil
	}
	isQuorum, _ := signingLockset.HasQuorum()
	if !isQuorum {
		log.Error("error occur: MkProposal error, no quorum ")
		return nil
	}
	if !(roundLockset != nil || rm.round == 0) {
		log.Error("error occur: MkProposal error ")
		return nil
	}

	// Try to wait more Tx per block
	// time.Sleep(1000 * 1000 * 1000 * 0.2)
	var block *types.Block
	if rm.cm.currentBlock != nil {
		log.Debug("block is prepared")
		block = rm.cm.currentBlock
	} else {
		log.Debug("block is not prepared")
		return nil
	}
	blockProposal, err := btypes.NewBlockProposal(rm.height, rm.round, block, signingLockset, roundLockset)
	if err != nil {
		log.Error("error occur %v", err)
		return nil
	}
	rm.cm.Sign(blockProposal)
	rm.cm.setProposalLock(block)
	log.Debug("Create block blockhash : ", blockProposal.Blockhash())
	return blockProposal
}

func (rm *RoundManager) vote() *btypes.Vote {

	if rm.voteLock != nil {
		//DEBUG
		log.Debug("voted")
		return nil
	}
	// DEBUG
	// log.Debug("in vote in RM", "height", rm.height, "round", rm.round)
	lastPrecommitVoteLock := rm.hm.LastPrecommitVoteLock()

	var vote *btypes.Vote
	if lastPrecommitVoteLock != nil && lastPrecommitVoteLock.VoteType == 1 {
		// vote previous PrecommitVote.
		vote = btypes.NewVote(rm.height, rm.round, lastPrecommitVoteLock.Blockhash, 1)
	} else if rm.proposal != nil {
		// There is a proposal, Intercept the proposal.
		switch bp := rm.proposal.(type) {
		case *btypes.VotingInstruction: // vote for votinginstruction
			quorum, _ := bp.LockSet().HasQuorum()
			// quorumPossible, _ := bp.LockSet().QuorumPossible()
			if quorum && bp.LockSet().Round() > lastPrecommitVoteLock.Round {
				log.Debug("vote votinginstruction quorum	")
				vote = btypes.NewVote(rm.height, rm.round, bp.Blockhash(), 1)
			} else {
				if lastPrecommitVoteLock == nil {
					vote = btypes.NewVote(rm.height, rm.round, common.StringToHash(""), 2)
				}
			}
		case *btypes.BlockProposal:
			log.Debug("voting on new proporsal")
			vote = btypes.NewVote(rm.height, rm.round, rm.proposal.Blockhash(), 1)
		}
	} else if rm.timeoutTime != 0 && float64(rm.cm.Now()) >= rm.timeoutTime {
		vote = btypes.NewVote(rm.height, rm.round, common.StringToHash(""), 2)
	} else {
		log.Debug("Timeout time not reach, curr vs timeout:", "curr", float64(rm.cm.Now()), "timeout", rm.timeoutTime)
		return nil
	}
	if vote == nil {
		return nil
	}
	rm.cm.Sign(vote)
	rm.voteLock = vote

	log.Debug("vote success in", "height", rm.height, "round", rm.round)
	rm.addVote(vote, false, true)
	rm.setTimeoutPrecommit()
	return vote
}

func (rm *RoundManager) votePrecommit() *btypes.PrecommitVote {
	if rm.precommitVoteLock != nil {
		log.Debug("precommit voted")
		return nil
	}
	var vote *btypes.PrecommitVote
	if rm.lockset.IsValid() {
		if quorum, blockhash := rm.lockset.HasQuorum(); quorum {
			log.Debug("prevote quorum. vote precommit on block")
			vote = btypes.NewPrecommitVote(rm.height, rm.round, blockhash, 1)
		} else if rm.timeoutTime != 0 && float64(rm.cm.Now()) >= rm.timeoutTime {
			log.Debug("prevote no quorum. vote precommit nil")
			vote = btypes.NewPrecommitVote(rm.height, rm.round, common.StringToHash(""), 2)
		} else {
			log.Debug("wait timeoutTime")
		}
	} else {
		log.Debug("prevote invalid")
	}
	if vote != nil {
		rm.cm.Sign(vote)
		if vote.VoteType == 1 {
			rm.precommitVoteLock = vote
		}
		// log.Debug("precommit vote success in H:", "height", rm.height)
		rm.addPrecommitVote(vote, false, true)
	}
	return vote
}
