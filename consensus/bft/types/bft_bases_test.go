package types

import (
	"bytes"
	"crypto/ecdsa"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestVote(t *testing.T) {
	key1 := crypto.MakePrivatekey("1")

	height := uint64(2)
	round := uint64(3)
	bh := common.HexToHash("00000000000000000000000000000000")

	sender := crypto.PubkeyToAddress(key1.PublicKey)
	v1 := NewVote(height, round, common.Hash{}, 2)
	v2 := NewVote(height, round, bh, 1)

	if err := v1.Sign(key1); err != nil {
		t.Error(err)
	}
	if addr, err := v1.From(); err == nil {
		if addr != sender {
			t.Error("derived address doesn't match")
		}
	} else {
		t.Error(err)
	}

	v2.Sign(key1)
	if addr, err := v2.From(); err == nil {
		if addr != sender {
			t.Error("derived address doesn't match")
		}
	} else {
		t.Error(err)
	}
	// encodes

	v1s, _ := rlp.EncodeToBytes(v1)
	var v1d *Vote
	if err := rlp.Decode(bytes.NewReader(v1s), &v1d); err != nil {
		t.Error("decodeVote failed")
	}
	emptyHash := common.Hash{}
	if v1d.Blockhash != emptyHash {
		t.Error("vote blockhash doesn't match")
	}

	v2s, _ := rlp.EncodeToBytes(v2)
	var v2d *Vote
	if err := rlp.Decode(bytes.NewReader(v2s), &v2d); err != nil {
		t.Error("decodeVote failed")
	}

	if v2d.Blockhash != bh {
		t.Error("vote blockhash doesn't match")
	}
}

func TestReady(t *testing.T) {
	key1 := crypto.MakePrivatekey("1")

	ls := NewLockSet(10, nil)
	s := NewReady(0, ls)
	if s.CurrentLockSet != ls {
		t.Error("ready's lockset doesn't match")
	}
	s.Sign(key1)

	s0 := NewReady(0, ls)
	s0.Sign(key1)

	s1 := NewReady(1, ls)
	s1.Sign(key1)

	s.From()
	s0.From()
	if s.CurrentLockSet != s0.CurrentLockSet || s.Nonce != s0.Nonce || *s.sender != *s0.sender {
		t.Error("readys doesn't match")
	}
	if *s == *s1 {
		t.Error("readys should not match")
	}
}
func TestLockSet(t *testing.T) {
	var keys []*ecdsa.PrivateKey
	var validators []common.Address
	for i := 0; i < 10; i++ {
		s := strconv.Itoa(i + 1)
		keys = append(keys, crypto.MakePrivatekey(s))
	}
	for _, key := range keys {
		validators = append(validators, crypto.PubkeyToAddress(key.PublicKey))
	}
	//
	ls := NewLockSet(uint64(len(keys)), nil)
	if len(ls.Votes) != 0 {
		t.Error("lockset votes number doesn't match")
	}
	height := uint64(2)
	round := uint64(3)
	bh := common.HexToHash("00000000000000000000000000000000")

	// add not signed vote
	v1 := NewVote(height, round, bh, 1)

	if err := ls.Add(v1, false); err == nil {
		t.Error("there should be an error")
	}
	// add not signed vote
	v1.Sign(keys[0])
	if err := ls.Add(v1, false); err != nil {
		t.Error("error occured")
	}
	if len(ls.Votes) != 1 {
		t.Error("votes number doesn't match")
	}
	lsh := ls.Hash()
	ls.Add(v1, false)
	if lsh != ls.Hash() {
		t.Error("lockset hash doesn't match")
	}
	if len(ls.Votes) != 1 {
		t.Error("votes number doesn't match")
	}

	// second vote same sender
	v2 := NewVote(height, round, bh, 1)
	v2.Sign(keys[0])
	ls.Add(v1, false)
	ls.Add(v2, false)
	if lsh != ls.Hash() {
		t.Error("lockset hash doesn't match")
	}
	if len(ls.Votes) != 1 {
		t.Error("votes number doesn't match")
	}

	// third vote
	v3 := NewVote(height, round, bh, 1)
	v3.Sign(keys[1])
	ls.Add(v3, false)
	if lsh == ls.Hash() {
		t.Error("lockset hash doesn't match")
	}
	if len(ls.Votes) != 2 {
		t.Error("votes number doesn't match")
	}
	if !ls.Contain(v3) {
		t.Error("vote should be in lockset")
	}
	lsh = ls.Hash()

	// vote wrong round
	v4 := NewVote(height, round+1, bh, 1)
	v4.Sign(keys[2])

	if lsh != ls.Hash() {
		t.Error("lockset hash doesn't match")
	}
	if len(ls.Votes) != 2 {
		t.Error("votes number doesn't match")
	}
	if ls.Contain(v4) {
		t.Error("vote should not be in lockset")
	}

	// vote twice
	bh2 := common.HexToHash("11111111111111111111111111111111")
	v3_2 := NewVote(height, round, bh2, 1)
	v3_2.Sign(keys[1])
	if err := ls.Add(v3_2, false); err == nil {
		t.Error("there should be an error")
	}
	if lsh != ls.Hash() {
		t.Error("lockset hash doesn't match")
	}
	if len(ls.Votes) != 2 {
		t.Error("votes number doesn't match")
	}
	if ls.Contain(v3_2) {
		t.Error("vote should not be in lockset")
	}
}
func TestoneVoteLockset(t *testing.T) {
	ls := NewLockSet(1, nil)
	if len(ls.Votes) != 0 {
		t.Error("lockset votes number doesn't match")
	}
	height := uint64(2)
	round := uint64(3)
	bh := common.HexToHash("00000000000000000000000000000000")
	v := NewVote(height, round, bh, 1)
	ls.Add(v, false)
	has, quorum := ls.HasQuorum()
	if has {
		t.Error("there should be a quorum")
	}
	if quorum != bh {
		t.Error("blockhash doesn't match")
	}
}
func TestLockSetIsvalid(t *testing.T) {
	var keys []*ecdsa.PrivateKey
	var validators []common.Address
	for i := 0; i < 10; i++ {
		s := strconv.Itoa(i + 1)
		keys = append(keys, crypto.MakePrivatekey(s))
	}
	for _, key := range keys {
		validators = append(validators, crypto.PubkeyToAddress(key.PublicKey))
	}
	//
	ls := NewLockSet(uint64(len(keys)), nil)
	if len(ls.Votes) != 0 {
		t.Error("lockset votes number doesn't match")
	}
	height := uint64(2)
	round := uint64(3)
	bh := common.HexToHash("00000000000000000000000000000000")
	var votes Votes
	for _, key := range keys {
		v := NewVote(height, round, bh, 1)
		v.Sign(key)
		votes = append(votes, v)
	}
	for i, v := range votes {
		v.Sign(keys[i])
		ls.Add(v, false)
		if len(ls.Votes) != i+1 {
			t.Error("lockset votes number doesn't match")
		}
		if float64(len(ls.Votes)) < 2/3.*float64(ls.EligibleVotesNum) {
			if ls.IsValid() {
				t.Error("lockset should be invalid")
			}
		} else {
			if !ls.IsValid() {
				t.Error("lockset should be valid")
			}
		}
	}

}
func TestLockSetWithQuorum(t *testing.T) {
	var keys []*ecdsa.PrivateKey
	var validators []common.Address
	for i := 0; i < 10; i++ {
		s := strconv.Itoa(i + 1)
		keys = append(keys, crypto.MakePrivatekey(s))
	}
	for _, key := range keys {
		validators = append(validators, crypto.PubkeyToAddress(key.PublicKey))
	}
	ls := NewLockSet(3, nil)
	bh := common.HexToHash("00000000000000000000000000000000")
	v1 := NewVote(0, 0, bh, 1)
	v2 := NewVote(0, 0, common.Hash{}, 2)
	v1.Sign(keys[0])
	v2.Sign(keys[1])
	ls.Add(v1, false)
	ls.Add(v2, false)
	if len(ls.Votes) != 2 {
		t.Error("lockset votes number doesn't match")
	}
	if ls.IsValid() {
		t.Error("lockset should be invalid")
	}
	v3 := NewVote(0, 0, common.Hash{}, 2)
	v3.Sign(keys[2])
	ls.Add(v3, false)
	if !ls.IsValid() {
		t.Error("lockset should be valid")
	}
	if !ls.NoQuorum() {
		t.Error("lockset should be no quorum")
	}
	if has, _ := ls.HasQuorum(); has {
		t.Error("lockset should be no quorum")
	}
	if has, _ := ls.QuorumPossible(); has {
		t.Error("lockset should be no quorum")
	}
}
func TestLockSetQuorums(t *testing.T) {
	var keys []*ecdsa.PrivateKey
	var validators []common.Address
	for i := 0; i < 10; i++ {
		s := strconv.Itoa(i + 1)
		keys = append(keys, crypto.MakePrivatekey(s))
	}
	for _, key := range keys {
		validators = append(validators, crypto.PubkeyToAddress(key.PublicKey))
	}
	height := uint64(2)
	round := uint64(3)
	bh1 := common.HexToHash("11111111111111111111111111111111")
	bh2 := common.HexToHash("22222222222222222222222222222222")
	bh3 := common.HexToHash("33333333333333333333333333333333")
	var ls *LockSet

	// hasQuorum
	q1 := []int{1, 1, 1, 1, 1, 1, 1}
	q2 := []int{1, 1, 1, 1, 1, 1, 1, 0, 0, 0}
	q3 := []int{1, 1, 1, 1, 1, 1, 1, 2, 2, 2}

	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q1 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}

	if has, _ := ls.HasQuorum(); !has {
		t.Error("there should be a quorum")
	}
	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q2 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if has, _ := ls.HasQuorum(); !has {
		t.Error("there should be a quorum")
	}
	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q3 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if has, _ := ls.HasQuorum(); !has {
		t.Error("there should be a quorum")
	}
	// no quorum
	q4 := []int{1, 1, 1, 2, 2, 2, 0}
	q5 := []int{0, 0, 0, 0, 0, 0, 0}
	q6 := []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q4 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if !ls.NoQuorum() {
		t.Error("there should be no quorum")
	}

	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q5 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}

	if !ls.NoQuorum() {
		t.Error("there should be no quorum")
	}

	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q6 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if !ls.NoQuorum() {
		t.Error("there should be no quorum")
	}
	q7 := []int{1, 1, 1, 1, 0, 0, 0}
	q8 := []int{1, 1, 1, 1, 2, 2, 2, 2}
	q9 := []int{1, 1, 1, 1, 2, 2, 2, 3, 3, 3}
	q10 := []int{1, 1, 1, 1, 1, 1, 2}

	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q7 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if has, _ := ls.QuorumPossible(); !has {
		t.Error("there should be a quorum")
	}
	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q8 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if has, _ := ls.QuorumPossible(); !has {
		t.Error("there should be a quorum")
	}
	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q9 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if has, _ := ls.QuorumPossible(); !has {
		t.Error("there should be a quorum")
	}
	ls = NewLockSet(uint64(len(keys)), nil)
	for i, j := range q10 {
		var v *Vote
		if j != 0 {
			if j == 1 {
				v = NewVote(height, round, bh1, 1)
			} else if j == 2 {
				v = NewVote(height, round, bh2, 1)
			} else if j == 3 {
				v = NewVote(height, round, bh3, 1)
			}
		} else if j == 0 {
			v = NewVote(height, round, common.Hash{}, 2)
		}
		v.Sign(keys[i])
		ls.Add(v, false)
	}
	if has, _ := ls.QuorumPossible(); !has {
		t.Error("there should be a quorum")
	}

}
