package types

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/sha3"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	ErrInvalidSig = errors.New("invalid transaction v, r, s values")
	errNoSigner   = errors.New("missing signing methods")
)

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

type Vote struct {
	// signed signed
	sender    *common.Address
	V         *big.Int // signature
	R, S      *big.Int // signature
	Height    uint64
	Round     uint64
	Blockhash common.Hash
	VoteType  uint64 // 0: vote , 1: voteblock , 2: votenil
}
type Votes []*Vote

func NewVote(height uint64, round uint64, blockhash common.Hash, voteType uint64) *Vote {
	return &Vote{
		R:         new(big.Int),
		S:         new(big.Int),
		Height:    height,
		Round:     round,
		Blockhash: blockhash,
		VoteType:  voteType,
	}
}

func (v *Vote) Hash() common.Hash {
	return rlpHash([]interface{}{
		v.sender,
		v.Height,
		v.Round,
		v.VoteType,
		v.Blockhash,
	})
}
func (v *Vote) SigHash() common.Hash {
	return rlpHash([]interface{}{
		v.Height,
		v.Round,
		v.VoteType,
		v.Blockhash,
	})
}
func (v *Vote) Sign(prv *ecdsa.PrivateKey) error {
	if v.V != nil {
		return errors.New("already sign")
	}
	_, err := v.SignECDSA(prv, v.SigHash())
	if err != nil {
		return err
	}
	return nil
}
func (vote *Vote) hr() (uint64, uint64) {
	return vote.Height, vote.Round
}
func (v *Vote) From() (common.Address, error) {
	if v.sender != nil {
		return *v.sender, nil
	} else {
		if v.V == nil {
			return common.Address{}, errors.New("no signature")
		}
		addr, err := v.recoverSender(v.SigHash())
		if err != nil {
			log.Error("sender() error ", err)
			return common.Address{}, err
		}
		v.sender = &addr
		return addr, nil

	}
}
func (vote *Vote) recoverSender(hash common.Hash) (common.Address, error) {
	pubkey, err := vote.publicKey(hash)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pubkey[1:])[12:])
	vote.sender = &addr
	return addr, nil
}
func (vote *Vote) publicKey(hash common.Hash) ([]byte, error) {
	if vote.V.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(vote.V.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, vote.R, vote.S, true) {
		return nil, ErrInvalidSig
	}

	// encode the signature in uncompressed format
	r, s := vote.R.Bytes(), vote.S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	// hash := signed.SigHash()
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		log.Error("Could not get pubkey from signature: ", err)
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, nil
}

//sign
func (vote *Vote) WithSignature(sig []byte) (*Vote, error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	vote.R = new(big.Int).SetBytes(sig[:32])
	vote.S = new(big.Int).SetBytes(sig[32:64])
	vote.V = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return vote, nil
}

// Sign this with a privacy key
func (vote *Vote) SignECDSA(prv *ecdsa.PrivateKey, hash common.Hash) (*Vote, error) {
	sig, err := crypto.Sign(hash[:], prv)
	if err != nil {
		return nil, err
	}
	return vote.WithSignature(sig)
}

type PrecommitVote struct {
	// signed signed
	sender    *common.Address
	V         *big.Int // signature
	R, S      *big.Int // signature
	Height    uint64
	Round     uint64
	Blockhash common.Hash
	VoteType  uint64 // 1: voteblock , 2: votenil
}
type PrecommitVotes []*PrecommitVote

func NewPrecommitVote(height uint64, round uint64, blockhash common.Hash, voteType uint64) *PrecommitVote {
	return &PrecommitVote{
		R:         new(big.Int),
		S:         new(big.Int),
		Height:    height,
		Round:     round,
		Blockhash: blockhash,
		VoteType:  voteType,
	}
}

func (v *PrecommitVote) Hash() common.Hash {
	return rlpHash([]interface{}{
		v.sender,
		v.Height,
		v.Round,
		v.VoteType,
		v.Blockhash,
	})
}
func (v *PrecommitVote) SigHash() common.Hash {
	return rlpHash([]interface{}{
		v.Height,
		v.Round,
		v.VoteType,
		v.Blockhash,
	})
}
func (v *PrecommitVote) Sign(prv *ecdsa.PrivateKey) error {
	if v.V != nil {
		return errors.New("already sign")
	}
	_, err := v.SignECDSA(prv, v.SigHash())
	if err != nil {
		return err
	}
	return nil
}
func (vote *PrecommitVote) hr() (uint64, uint64) {
	return vote.Height, vote.Round
}
func (v *PrecommitVote) From() (common.Address, error) {
	if v.sender != nil {
		return *v.sender, nil
	} else {
		if v.V == nil {
			return common.Address{}, errors.New("no signature")
		}
		addr, err := v.recoverSender(v.SigHash())
		if err != nil {
			log.Error("sender() error ", err)
			return common.Address{}, err
		}
		v.sender = &addr
		return addr, nil

	}
}
func (vote *PrecommitVote) recoverSender(hash common.Hash) (common.Address, error) {
	pubkey, err := vote.publicKey(hash)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pubkey[1:])[12:])
	vote.sender = &addr
	return addr, nil
}
func (vote *PrecommitVote) publicKey(hash common.Hash) ([]byte, error) {
	if vote.V.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(vote.V.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, vote.R, vote.S, true) {
		return nil, ErrInvalidSig
	}

	// encode the signature in uncompressed format
	r, s := vote.R.Bytes(), vote.S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	// hash := signed.SigHash()
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		log.Error("Could not get pubkey from signature: ", err)
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, nil
}

//sign
func (vote *PrecommitVote) WithSignature(sig []byte) (*PrecommitVote, error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	vote.R = new(big.Int).SetBytes(sig[:32])
	vote.S = new(big.Int).SetBytes(sig[32:64])
	vote.V = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return vote, nil
}

// Sign this with a privacy key
func (vote *PrecommitVote) SignECDSA(prv *ecdsa.PrivateKey, hash common.Hash) (*PrecommitVote, error) {
	sig, err := crypto.Sign(hash[:], prv)
	if err != nil {
		return nil, err
	}
	return vote.WithSignature(sig)
}

type LockSet struct {
	// signed           signed
	sender           *common.Address
	V                *big.Int // signature
	R, S             *big.Int // signature
	EligibleVotesNum uint64
	Votes            Votes
	processed        bool
}

func NewLockSet(eligibleVotesNum uint64, vs Votes) *LockSet {

	ls := &LockSet{
		R:                new(big.Int),
		S:                new(big.Int),
		EligibleVotesNum: eligibleVotesNum,
		Votes:            []*Vote{},
		processed:        false,
	}
	for _, v := range vs {
		ls.Add(v, false)
	}
	return ls
}

// TODO FIXME
func (ls *LockSet) Copy() *LockSet {
	return NewLockSet(ls.EligibleVotesNum, ls.Votes)
}

type HashCount struct {
	blockhash common.Hash
	count     int
}
type HashCounts []HashCount

func (s HashCounts) Len() int           { return len(s) }
func (s HashCounts) Less(i, j int) bool { return s[i].count > s[j].count }
func (s HashCounts) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (lockset *LockSet) sortByBlockhash() HashCounts {
	// bhs := make(HashCount, 0, len(lockset.votes))
	bhs := make(map[common.Hash]int)
	for _, v := range lockset.Votes {
		if v.VoteType == 1 {
			bhs[v.Blockhash] += 1
		}
	}
	hs := make(HashCounts, 0)
	for bh, count := range bhs {
		hs = append(hs, HashCount{blockhash: bh, count: count})
	}
	sort.Sort(hs)
	return hs
}

func (lockset *LockSet) hr() (uint64, uint64) {
	if len(lockset.Votes) == 0 {
		panic("no vote for hr()")
	}
	hset := make(map[uint64]struct{})
	rset := make(map[uint64]struct{})

	for _, v := range lockset.Votes {
		hset[v.Height] = struct{}{}
		rset[v.Round] = struct{}{}
	}
	if len(hset) != 1 && len(rset) != 1 {
		log.Error("different hr in lockset")
	}
	return lockset.Votes[0].Height, lockset.Votes[0].Round
}
func (lockset *LockSet) From() (common.Address, error) {
	if lockset.sender != nil {
		return *lockset.sender, nil
	} else {
		if lockset.V == nil {
			return common.Address{}, errors.New("no signature")
		}
		addr, err := lockset.recoverSender(lockset.SigHash())
		if err != nil {
			log.Error("sender() error ", err)
			return common.Address{}, err
		}
		lockset.sender = &addr
		return addr, nil
	}
}
func (lockset *LockSet) Hash() common.Hash {
	return rlpHash([]interface{}{
		lockset.sender,
		lockset.EligibleVotesNum,
		lockset.Votes,
	})
}

func (lockset *LockSet) Height() uint64 {
	h, _ := lockset.hr()
	return h
}
func (lockset *LockSet) Round() uint64 {
	_, r := lockset.hr()
	return r
}
func (lockset *LockSet) SigHash() common.Hash {
	return rlpHash([]interface{}{
		lockset.EligibleVotesNum,
		lockset.Votes,
	})
}
func (lockset *LockSet) Sign(prv *ecdsa.PrivateKey) error {
	if lockset.V != nil {
		return errors.New("already sign")
	}
	_, err := lockset.SignECDSA(prv, lockset.SigHash())
	if err != nil {
		return err
	}
	return nil
}

var ErrInvalidVote = errors.New("inconsistent height, round")
var ErrInvalidVoteSig = errors.New("no signature")
var ErrDoubleVoting = errors.New("different votes on the same H,R")

func (lockset *LockSet) Add(vote *Vote, force bool) error {
	// log.Info(*vote.signed.sender)
	vote.From()
	if vote.sender == nil {
		log.Error("Could not get pubkey from signature: ", ErrInvalidVote)
		return ErrInvalidVote
	}

	if !lockset.Contain(vote) {

		if len(lockset.Votes) != 0 && (vote.Height != lockset.Height() || vote.Round != lockset.Round()) {
			fmt.Printf("votes len:%d, lockset.Height: %d, lockset.Round: %d \n", len(lockset.Votes), lockset.Height(), lockset.Round())
			fmt.Printf("vote.Height: %d, vote.Round: %d \n", vote.Height, vote.Round)
			return ErrInvalidVoteSig
		}
		if containsAddress(lockset.signee(), *vote.sender) {
			if !force {
				return ErrDoubleVoting
			}
			// find the previous vote and remove it
			lockset.removeVoteFrom(*vote.sender)
		}
		lockset.Votes = append(lockset.Votes, vote)

	}
	return nil
}

func (lockset *LockSet) removeVoteFrom(from common.Address) {
	for i, v := range lockset.Votes {
		if *v.sender == from {
			lockset.Votes = append(lockset.Votes[:i], lockset.Votes[i+1:]...)
			break
		}
	}
}

func (lockset *LockSet) signee() []common.Address {
	signee := []common.Address{}
	for _, v := range lockset.Votes {
		signee = append(signee, *v.sender)
	}
	return signee
}
func (lockset *LockSet) Contain(vote *Vote) bool {
	return containsVote(lockset.Votes, vote)
}

func containsVote(s []*Vote, e *Vote) bool {
	addr, _ := e.From()
	for _, a := range s {
		if a.Height == e.Height && a.Round == e.Round && a.Blockhash == e.Blockhash && a.VoteType == e.VoteType {
			addr2, _ := a.From()
			if addr == addr2 {
				return true
			}
		}
	}
	return false
}
func (lockset *LockSet) IsValid() bool {
	if float64(len(lockset.Votes)) > 2/3.*float64(lockset.EligibleVotesNum) {
		lockset.hr() // check votes' validation
		return true
	}
	return false
}

func (lockset *LockSet) HasQuorum() (bool, common.Hash) {
	if !lockset.IsValid() {
		return false, common.Hash{}
	}
	hs := lockset.sortByBlockhash()
	if len(hs) == 0 {
		return false, common.Hash{}
	}
	if float64(hs[0].count) > 2/3.0*float64(lockset.EligibleVotesNum) {
		return true, hs[0].blockhash
	} else {
		return false, common.Hash{}
	}
}

func (lockset *LockSet) NoQuorum() bool {
	if !lockset.IsValid() {
		return false
	}
	hs := lockset.sortByBlockhash()
	if len(hs) == 0 {
		return true
	}
	if float64(hs[0].count) <= 2/3.*float64(lockset.EligibleVotesNum) {
		return true
	} else {
		return false
	}
}

// func (lockset *LockSet) QuorumPossible() (bool, common.Hash) {

// 	if result, hs := lockset.HasQuorum(); result != false {
// 		return false, hs
// 	}
// 	if !lockset.IsValid() {
// 		return false, common.Hash{}
// 	}
// 	hs := lockset.sortByBlockhash()
// 	if len(hs) == 0 {
// 		return false, common.Hash{}
// 	}
// 	if float64(hs[0].count) > 1/3.*float64(lockset.EligibleVotesNum) {
// 		return true, hs[0].blockhash
// 	} else {
// 		return false, common.Hash{}
// 	}
// }
func checkVotes(lockset *LockSet, validators []common.Address) error {
	if int(lockset.EligibleVotesNum) != len(validators) {
		return errors.New("lockset EligibleVotesNum mismatch")
	}
	for _, v := range lockset.Votes {
		if containsAddress(validators, *v.sender) {
			errors.New("invalid signer")
		}
	}
	return nil
}
func (lockset *LockSet) recoverSender(hash common.Hash) (common.Address, error) {

	pubkey, err := lockset.publicKey(hash)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pubkey[1:])[12:])
	lockset.sender = &addr
	return addr, nil
}
func (lockset *LockSet) publicKey(hash common.Hash) ([]byte, error) {
	if lockset.V.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(lockset.V.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, lockset.R, lockset.S, true) {
		return nil, ErrInvalidSig
	}

	// encode the signature in uncompressed format
	r, s := lockset.R.Bytes(), lockset.S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	// hash := signed.SigHash()
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		log.Error("Could not get pubkey from signature: ", err)
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, nil
}

//sign
func (lockset *LockSet) WithSignature(sig []byte) (*LockSet, error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	lockset.R = new(big.Int).SetBytes(sig[:32])
	lockset.S = new(big.Int).SetBytes(sig[32:64])
	lockset.V = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return lockset, nil
}

// Sign this with a privacy key
func (lockset *LockSet) SignECDSA(prv *ecdsa.PrivateKey, hash common.Hash) (*LockSet, error) {
	sig, err := crypto.Sign(hash[:], prv)
	if err != nil {
		return nil, err
	}
	return lockset.WithSignature(sig)
}

// func (lockset *Lockset) check() {
// 'check either invalid or one of quorum, noquorum, quorumpossible'
// }

/////////////////////////////////////////////

func GenesisSigningLockset(genesis *types.Block, prv *ecdsa.PrivateKey) *LockSet {
	v := NewVote(0, 0, genesis.Hash(), 1)
	v.Sign(prv)
	ls := NewLockSet(1, nil)
	ls.Add(v, false)
	if result, _ := ls.HasQuorum(); result == false {
		panic("Genesis Signing Lockset error")
	}
	return ls
}

type PrecommitLockSet struct {
	// signed           signed
	sender           *common.Address
	V                *big.Int // signature
	R, S             *big.Int // signature
	EligibleVotesNum uint64
	PrecommitVotes   PrecommitVotes
	processed        bool
}

func NewPrecommitLockSet(eligibleVotesNum uint64, vs PrecommitVotes) *PrecommitLockSet {
	ls := &PrecommitLockSet{
		R:                new(big.Int),
		S:                new(big.Int),
		EligibleVotesNum: eligibleVotesNum,
		PrecommitVotes:   []*PrecommitVote{},
		processed:        false,
	}
	for _, v := range vs {
		ls.Add(v, false)
	}
	return ls
}

// TODO FIXME
func (ls *PrecommitLockSet) Copy() *PrecommitLockSet {
	return NewPrecommitLockSet(ls.EligibleVotesNum, ls.PrecommitVotes)
}
func (lockset *PrecommitLockSet) sortByBlockhash() HashCounts {
	// bhs := make(HashCount, 0, len(lockset.votes))
	bhs := make(map[common.Hash]int)
	for _, v := range lockset.PrecommitVotes {
		if v.VoteType == 1 {
			bhs[v.Blockhash] += 1
		}
	}
	hs := make(HashCounts, 0)
	for bh, count := range bhs {
		hs = append(hs, HashCount{blockhash: bh, count: count})
	}
	sort.Sort(hs)
	return hs
}

func (lockset *PrecommitLockSet) hr() (uint64, uint64) {
	if len(lockset.PrecommitVotes) == 0 {
		panic("no vote for hr()")
	}
	hset := make(map[uint64]struct{})
	rset := make(map[uint64]struct{})

	for _, v := range lockset.PrecommitVotes {
		hset[v.Height] = struct{}{}
		rset[v.Round] = struct{}{}
	}
	if len(hset) != 1 && len(rset) != 1 {
		log.Error("different hr in lockset")
	}
	return lockset.PrecommitVotes[0].Height, lockset.PrecommitVotes[0].Round
}
func (lockset *PrecommitLockSet) From() (common.Address, error) {
	if lockset.sender != nil {
		return *lockset.sender, nil
	} else {
		if lockset.V == nil {
			return common.Address{}, errors.New("no signature")
		}
		addr, err := lockset.recoverSender(lockset.SigHash())
		if err != nil {
			log.Error("sender() error ", err)
			return common.Address{}, err
		}
		lockset.sender = &addr
		return addr, nil
	}
}
func (lockset *PrecommitLockSet) Hash() common.Hash {
	return rlpHash([]interface{}{
		lockset.sender,
		lockset.EligibleVotesNum,
		lockset.PrecommitVotes,
	})
}
func (lockset *PrecommitLockSet) Height() uint64 {
	h, _ := lockset.hr()
	return h
}
func (lockset *PrecommitLockSet) Round() uint64 {
	_, r := lockset.hr()
	return r
}
func (lockset *PrecommitLockSet) SigHash() common.Hash {
	return rlpHash([]interface{}{
		lockset.EligibleVotesNum,
		lockset.PrecommitVotes,
	})
}
func (lockset *PrecommitLockSet) Sign(prv *ecdsa.PrivateKey) error {
	if lockset.V != nil {
		return errors.New("already sign")
	}
	_, err := lockset.SignECDSA(prv, lockset.SigHash())
	if err != nil {
		return err
	}
	return nil
}
func (lockset *PrecommitLockSet) Add(vote *PrecommitVote, force bool) error {
	// log.Info(*vote.signed.sender)
	vote.From()
	if vote.sender == nil {
		log.Error("Could not get pubkey from signature: ", ErrInvalidVote)
		return ErrInvalidVote
	}

	if !lockset.Contain(vote) {

		if len(lockset.PrecommitVotes) != 0 && (vote.Height != lockset.Height() || vote.Round != lockset.Round()) {
			fmt.Printf("votes len:%d, lockset.Height: %d, lockset.Round: %d \n", len(lockset.PrecommitVotes), lockset.Height(), lockset.Round())
			fmt.Printf("vote.Height: %d, vote.Round: %d \n", vote.Height, vote.Round)
			return ErrInvalidVoteSig
		}
		if containsAddress(lockset.signee(), *vote.sender) {
			if !force {
				return ErrDoubleVoting
			}
			// find the previous vote and remove it
			lockset.removeVoteFrom(*vote.sender)
		}
		lockset.PrecommitVotes = append(lockset.PrecommitVotes, vote)

	}
	return nil
}

func (lockset *PrecommitLockSet) removeVoteFrom(from common.Address) {
	for i, v := range lockset.PrecommitVotes {
		if *v.sender == from {
			lockset.PrecommitVotes = append(lockset.PrecommitVotes[:i], lockset.PrecommitVotes[i+1:]...)
			break
		}
	}
}

func (lockset *PrecommitLockSet) signee() []common.Address {
	signee := []common.Address{}
	for _, v := range lockset.PrecommitVotes {
		signee = append(signee, *v.sender)
	}
	return signee
}

func (lockset *PrecommitLockSet) Contain(vote *PrecommitVote) bool {
	return containsPrecommitVote(lockset.PrecommitVotes, vote)
}

func containsPrecommitVote(s []*PrecommitVote, e *PrecommitVote) bool {
	addr, _ := e.From()
	for _, a := range s {
		if a.Height == e.Height && a.Round == e.Round && a.Blockhash == e.Blockhash && a.VoteType == e.VoteType {
			addr2, _ := a.From()
			if addr == addr2 {
				return true
			}
		}
	}

	return false
}

func (lockset *PrecommitLockSet) IsValid() bool {
	if float64(len(lockset.PrecommitVotes)) > 2/3.*float64(lockset.EligibleVotesNum) {
		lockset.hr() // check votes' validation
		return true
	}
	return false
}

func (lockset *PrecommitLockSet) HasQuorum() (bool, common.Hash) {
	if !lockset.IsValid() {
		return false, common.Hash{}
	}
	hs := lockset.sortByBlockhash()
	if len(hs) == 0 {
		return false, common.Hash{}
	}
	if float64(hs[0].count) > 2/3.0*float64(lockset.EligibleVotesNum) {
		return true, hs[0].blockhash
	} else {
		return false, common.Hash{}
	}
}
func (lockset *PrecommitLockSet) NoQuorum() bool {
	if !lockset.IsValid() {
		return false
	}
	if q, _ := lockset.HasQuorum(); !q {
		return true
	} else {
		return false
	}

}
func checkPrecommitVotes(lockset *PrecommitLockSet, validators []common.Address) error {
	if int(lockset.EligibleVotesNum) != len(validators) {
		return errors.New("lockset EligibleVotesNum mismatch")
	}
	for _, v := range lockset.PrecommitVotes {
		if containsAddress(validators, *v.sender) {
			errors.New("invalid signer")
		}
	}
	return nil
}
func (lockset *PrecommitLockSet) recoverSender(hash common.Hash) (common.Address, error) {

	pubkey, err := lockset.publicKey(hash)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pubkey[1:])[12:])
	lockset.sender = &addr
	return addr, nil
}
func (lockset *PrecommitLockSet) publicKey(hash common.Hash) ([]byte, error) {
	if lockset.V.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(lockset.V.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, lockset.R, lockset.S, true) {
		return nil, ErrInvalidSig
	}

	// encode the signature in uncompressed format
	r, s := lockset.R.Bytes(), lockset.S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	// hash := signed.SigHash()
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		log.Error("Could not get pubkey from signature: ", err)
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, nil
}

//sign
func (lockset *PrecommitLockSet) WithSignature(sig []byte) (*PrecommitLockSet, error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	lockset.R = new(big.Int).SetBytes(sig[:32])
	lockset.S = new(big.Int).SetBytes(sig[32:64])
	lockset.V = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return lockset, nil
}

// Sign this with a privacy key
func (lockset *PrecommitLockSet) SignECDSA(prv *ecdsa.PrivateKey, hash common.Hash) (*PrecommitLockSet, error) {
	sig, err := crypto.Sign(hash[:], prv)
	if err != nil {
		return nil, err
	}
	return lockset.WithSignature(sig)
}

type Ready struct {
	// signed         signed
	sender         *common.Address
	V              *big.Int // signature
	R, S           *big.Int // signature
	Nonce          uint64
	CurrentLockSet *LockSet
}

func NewReady(nonce uint64, currentLockSet *LockSet) *Ready {
	return &Ready{
		R:              new(big.Int),
		S:              new(big.Int),
		Nonce:          nonce,
		CurrentLockSet: currentLockSet,
	}
}
func (r *Ready) Hash() common.Hash {
	return rlpHash([]interface{}{
		r.sender,
		r.Nonce,
		r.CurrentLockSet,
	})
}
func (r *Ready) SigHash() common.Hash {
	return rlpHash([]interface{}{
		r.Nonce,
		r.CurrentLockSet,
	})
}
func (r *Ready) From() (common.Address, error) {
	if r.sender != nil {
		return *r.sender, nil
	} else {
		if r.V == nil {
			return common.Address{}, errors.New("no signature")
		}
		addr, err := r.recoverSender(r.SigHash())
		if err != nil {
			return common.Address{}, err
		}
		r.sender = &addr
		return addr, nil
	}
}
func (r *Ready) Sign(prv *ecdsa.PrivateKey) error {
	if r.V != nil {
		return errors.New("already sign")
	}
	_, err := r.SignECDSA(prv, r.SigHash())
	if err != nil {
		return err
	}
	return nil
}
func (r *Ready) recoverSender(hash common.Hash) (common.Address, error) {

	pubkey, err := r.publicKey(hash)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pubkey[1:])[12:])
	r.sender = &addr
	return addr, nil
}
func (ready *Ready) publicKey(hash common.Hash) ([]byte, error) {
	if ready.V.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(ready.V.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, ready.R, ready.S, true) {
		return nil, ErrInvalidSig
	}

	// encode the signature in uncompressed format
	r, s := ready.R.Bytes(), ready.S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	// hash := signed.SigHash()
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		log.Error("Could not get pubkey from signature: ", err)
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, nil
}

//sign
func (r *Ready) WithSignature(sig []byte) (*Ready, error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	r.R = new(big.Int).SetBytes(sig[:32])
	r.S = new(big.Int).SetBytes(sig[32:64])
	r.V = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return r, nil
}

// Sign this with a privacy key
func (r *Ready) SignECDSA(prv *ecdsa.PrivateKey, hash common.Hash) (*Ready, error) {
	sig, err := crypto.Sign(hash[:], prv)
	if err != nil {
		return nil, err
	}
	return r.WithSignature(sig)
}

type Proposal interface {
	Sign(prv *ecdsa.PrivateKey) error
	From() (common.Address, error)
	GetHeight() uint64
	GetRound() uint64
	Blockhash() common.Hash
	LockSet() *LockSet
}
type BlockProposal struct {
	// signed signed
	sender         *common.Address
	V              *big.Int // signature
	R, S           *big.Int // signature
	Height         uint64
	Round          uint64
	Block          *types.Block
	RoundLockset   *LockSet
	SigningLockset *PrecommitLockSet
}

func NewBlockProposal(height uint64, round uint64, block *types.Block, signingLockset *PrecommitLockSet, roundLockset *LockSet) (*BlockProposal, error) {

	if round > 0 && roundLockset == nil {
		return nil, errors.New("R>0 needs a round lockset")
	}
	if round == 0 && roundLockset != nil {
		return nil, errors.New("R0 must not have a round lockset")
	}
	bp := &BlockProposal{
		R:              new(big.Int),
		S:              new(big.Int),
		Height:         height,
		Round:          round,
		Block:          block,
		SigningLockset: signingLockset,
		RoundLockset:   roundLockset,
	}
	if height != block.NumberU64() {
		return nil, errors.New("lockset.height / block.number mismatch")
	}
	if roundLockset != nil && height != roundLockset.Height() {
		return nil, errors.New("height mismatch")
	}
	if has, _ := signingLockset.HasQuorum(); !(round > 0 || has) {
		return nil, errors.New("R0 lockset == signing lockset needs quorum")
	}
	if round == 0 && roundLockset != nil {
		if roundLockset.Height() != block.NumberU64()-1 {
			return nil, errors.New("R0 round lockset must be from previous height")
		}
	}
	if !(round == 0) && !(round == roundLockset.Round()+1) {
		return nil, errors.New("Rn round lockset must be from previous round")
	}
	if has, _ := bp.SigningLockset.HasQuorum(); !has {
		return nil, errors.New("signing lockset needs quorum")
	}
	if !(bp.SigningLockset.Height() == bp.Height-1) {
		return nil, errors.New("signing lockset height mismatch")
	}
	if roundLockset != nil {
		if quorum, _ := roundLockset.HasQuorum(); quorum {
			return nil, errors.New("should be votinginstruction if there is quorum")
		}
	} else {
		bp.RoundLockset = NewLockSet(0, Votes{})
	}
	return bp, nil
}

func (bp *BlockProposal) GetHeight() uint64 { return bp.Height }
func (bp *BlockProposal) GetRound() uint64  { return bp.Round }
func (bp *BlockProposal) From() (common.Address, error) {
	if bp.sender != nil {
		if *bp.sender != bp.Block.Coinbase() {
			return common.Address{}, errors.New("signature does not match")

		}
		return *bp.sender, nil
	} else {
		if bp.V == nil {
			return common.Address{}, errors.New("no signature")
		}
		addr, err := bp.recoverSender(bp.SigHash())
		if err != nil {
			return common.Address{}, err
		}
		if *bp.sender != bp.Block.Coinbase() {
			return common.Address{}, errors.New("signature does not match")
		}
		bp.sender = &addr
		return addr, nil
	}
}
func (bp *BlockProposal) Hash() common.Hash {
	return rlpHash([]interface{}{
		bp.sender,
		bp.Height,
		bp.Round,
		bp.SigningLockset,
		bp.RoundLockset,
	})
}
func (bp *BlockProposal) SigHash() common.Hash {
	return rlpHash([]interface{}{
		bp.Height,
		bp.Round,
		bp.SigningLockset,
		bp.RoundLockset,
	})
}

// func (bp *BlockProposal) SigningLockset() *LockSet { return bp.SigningLockset }
func (bp *BlockProposal) Blockhash() common.Hash { return bp.Block.Hash() }
func (bp *BlockProposal) LockSet() *LockSet      { return bp.RoundLockset }

func (bp *BlockProposal) Sign(prv *ecdsa.PrivateKey) error {
	if bp.V != nil {
		return errors.New("already sign")
	}
	_, err := bp.SignECDSA(prv, bp.SigHash())
	if err != nil {
		return err
	}
	if _, err := bp.From(); err != nil {
		return err
	}
	return nil
}

func (bp *BlockProposal) ValidateVotes(validators_H []common.Address, validators_prevH []common.Address) error {
	if _, err := bp.From(); err != nil {
		return err
	}

	if bp.RoundLockset != nil && bp.RoundLockset.EligibleVotesNum != 0 {
		return checkVotes(bp.RoundLockset, validators_H)
	}
	return checkPrecommitVotes(bp.SigningLockset, validators_prevH)

}

func containsAddress(s []common.Address, e common.Address) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func (bp *BlockProposal) recoverSender(hash common.Hash) (common.Address, error) {

	pubkey, err := bp.publicKey(hash)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pubkey[1:])[12:])
	bp.sender = &addr
	return addr, nil
}

func (bp *BlockProposal) publicKey(hash common.Hash) ([]byte, error) {
	if bp.V.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(bp.V.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, bp.R, bp.S, true) {
		return nil, ErrInvalidSig
	}

	// encode the signature in uncompressed format
	r, s := bp.R.Bytes(), bp.S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	// hash := signed.SigHash()
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		log.Error("Could not get pubkey from signature: ", err)
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, nil
}

//sign
func (bp *BlockProposal) WithSignature(sig []byte) (*BlockProposal, error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	bp.R = new(big.Int).SetBytes(sig[:32])
	bp.S = new(big.Int).SetBytes(sig[32:64])
	bp.V = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return bp, nil
}

// Sign this with a privacy key
func (bp *BlockProposal) SignECDSA(prv *ecdsa.PrivateKey, hash common.Hash) (*BlockProposal, error) {
	sig, err := crypto.Sign(hash[:], prv)
	if err != nil {
		return nil, err
	}
	return bp.WithSignature(sig)
}

type VotingInstruction struct {
	// signed signed
	sender       *common.Address
	V            *big.Int // signature
	R, S         *big.Int // signature
	Height       uint64
	Round        uint64
	RoundLockset *LockSet
}

func NewVotingInstruction(height uint64, round uint64, roundLockset *LockSet) (*VotingInstruction, error) {
	if round <= 0 {
		return nil, errors.New("VotingInstructions must have R>0")
	}
	// if has, _ := roundLockset.QuorumPossible(); !has {
	// 	return nil, errors.New("VotingInstruction requires quorum possible")
	// }
	if round != roundLockset.Round()+1 {
		return nil, errors.New("Rn round lockset must be from previous round")
	}
	if height != roundLockset.Height() {
		return nil, errors.New("height mismatch")
	}
	if round != roundLockset.Round()+1 {
		return nil, errors.New("round mismatch")
	}
	return &VotingInstruction{
		R:            new(big.Int),
		S:            new(big.Int),
		Height:       height,
		Round:        round,
		RoundLockset: roundLockset,
	}, nil
}
func (vi *VotingInstruction) GetHeight() uint64 { return vi.Height }
func (vi *VotingInstruction) GetRound() uint64  { return vi.Round }

func (vi *VotingInstruction) From() (common.Address, error) {
	if vi.sender != nil {
		return *vi.sender, nil
	} else {
		if vi.V == nil {
			return common.Address{}, errors.New("no signature")
		}
		addr, err := vi.recoverSender(vi.SigHash())
		if err != nil {
			return common.Address{}, err
		}
		vi.sender = &addr
		return addr, nil
	}
}
func (vi *VotingInstruction) Hash() common.Hash {
	return rlpHash([]interface{}{
		vi.sender,
		vi.Height,
		vi.Round,
		vi.RoundLockset,
	})
}
func (vi *VotingInstruction) SigHash() common.Hash {
	return rlpHash([]interface{}{
		vi.Height,
		vi.Round,
		vi.RoundLockset,
	})
}
func (vi *VotingInstruction) Blockhash() common.Hash {
	_, hash := vi.RoundLockset.HasQuorum()
	return hash
}
func (vi *VotingInstruction) LockSet() *LockSet { return vi.RoundLockset }
func (vi *VotingInstruction) ValidateVotes(validators []common.Address) error {
	if _, err := vi.From(); err != nil {
		return err
	}

	if int(vi.RoundLockset.EligibleVotesNum) != len(validators) {
		return errors.New("roundLockset EligibleVotes mismatch")
	}
	for _, v := range vi.RoundLockset.Votes {
		if containsAddress(validators, *v.sender) {
			return errors.New("invalid signer")
		}
	}
	return nil
}
func (vi *VotingInstruction) Sign(prv *ecdsa.PrivateKey) error {
	if vi.V != nil {
		return errors.New("already sign")
	}
	_, err := vi.SignECDSA(prv, vi.SigHash())
	if err != nil {
		return err
	}
	if _, err := vi.From(); err != nil {
		return err
	}
	return nil
}
func (vi *VotingInstruction) recoverSender(hash common.Hash) (common.Address, error) {

	pubkey, err := vi.publicKey(hash)
	if err != nil {
		return common.Address{}, err
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pubkey[1:])[12:])
	vi.sender = &addr
	return addr, nil
}
func (vi *VotingInstruction) publicKey(hash common.Hash) ([]byte, error) {
	if vi.V.BitLen() > 8 {
		return nil, ErrInvalidSig
	}
	V := byte(vi.V.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, vi.R, vi.S, true) {
		return nil, ErrInvalidSig
	}

	// encode the signature in uncompressed format
	r, s := vi.R.Bytes(), vi.S.Bytes()
	sig := make([]byte, 65)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V

	// recover the public key from the signature
	// hash := signed.SigHash()
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		log.Error("Could not get pubkey from signature: ", err)
		return nil, err
	}
	if len(pub) == 0 || pub[0] != 4 {
		return nil, errors.New("invalid public key")
	}
	return pub, nil
}

//sign
func (vi *VotingInstruction) WithSignature(sig []byte) (*VotingInstruction, error) {
	if len(sig) != 65 {
		panic(fmt.Sprintf("wrong size for signature: got %d, want 65", len(sig)))
	}
	vi.R = new(big.Int).SetBytes(sig[:32])
	vi.S = new(big.Int).SetBytes(sig[32:64])
	vi.V = new(big.Int).SetBytes([]byte{sig[64] + 27})
	return vi, nil
}

// Sign this with a privacy key
func (vi *VotingInstruction) SignECDSA(prv *ecdsa.PrivateKey, hash common.Hash) (*VotingInstruction, error) {
	sig, err := crypto.Sign(hash[:], prv)
	if err != nil {
		return nil, err
	}
	return vi.WithSignature(sig)
}
