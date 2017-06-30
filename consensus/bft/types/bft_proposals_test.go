package types

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestBlockProposal(t *testing.T) {
	var keys []*ecdsa.PrivateKey
	var validators []common.Address
	for i := 0; i < 10; i++ {
		s := strconv.Itoa(i + 1)
		keys = append(keys, crypto.MakePrivatekey(s))
	}
	for _, key := range keys {
		validators = append(validators, crypto.PubkeyToAddress(key.PublicKey))
	}
	bh := common.HexToHash("00000000000000000000000000000000")

	params.MinGasLimit = big.NewInt(125000)      // Minimum the gas limit may ever be.
	params.GenesisGasLimit = big.NewInt(3141592) // Gas limit of the Genesis block.

	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		key3, _ = crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		addr3   = crypto.PubkeyToAddress(key3.PublicKey)
		db, _   = ethdb.NewMemDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesis := WriteGenesisBlockForTesting(db, GenesisAccount{addr1, big.NewInt(1000000)})
	chain, _ := GenerateChain(nil, genesis, db, 5, func(i int, gen *BlockGen) {
		switch i {
		case 0:
			// In block 1, addr1 sends addr2 some ether.
			gen.SetCoinbase(addr1)
			tx, _ := types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), params.TxGas, nil, nil).SignECDSA(key1)
			gen.AddTx(tx)
		case 1:
			// In block 2, addr1 sends some more ether to addr2.
			// addr2 passes it on to addr3.
			gen.SetCoinbase(addr1)
			tx1, _ := types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(key1)
			tx2, _ := types.NewTransaction(gen.TxNonce(addr2), addr3, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(key2)
			gen.AddTx(tx1)
			gen.AddTx(tx2)
		case 2:
			// Block 3 is empty but was mined by addr3.
			gen.SetCoinbase(addr3)
			gen.SetExtra([]byte("yeehaw"))
		case 3:
			// Block 4 includes blocks 2 and 3 as uncle headers (with modified extra data).
			b2 := gen.PrevBlock(1).Header()
			b2.Extra = []byte("foo")
			gen.AddUncle(b2)
			b3 := gen.PrevBlock(2).Header()
			b3.Extra = []byte("foo")
			gen.AddUncle(b3)
		}
	})

	// Import the chain. This runs all block validation rules.
	evmux := &event.TypeMux{}
	blockchain, _ := NewBlockChain(db, MakeChainConfig(), FakePow{}, evmux)
	if i, err := blockchain.InsertChain(chain); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}

	// block 1
	blk1 := blockchain.GetBlockByNumber(1)
	gls := types.GenesisSigningLockset(genesis, keys[0])
	bp1, _ := types.NewBlockProposal(1, 0, blk1, gls, nil)
	if bp1.LockSet() != gls {
		t.Error("locksets does not match")
	}
	bp1.Sign(key1)
	if _, err := types.NewBlockProposal(1, 1, blk1, gls, nil); err == nil {
		t.Error("There should be an error")
	}
	bp1.ValidateVotes(validators, validators[:1])

	// block 2
	blk2 := blockchain.GetBlockByNumber(2)

	ls := types.NewLockSet(uint64(len(validators)), nil)
	for _, key := range keys {
		v := types.NewVote(1, 0, blk1.Hash(), 1)
		v.Sign(key)
		ls.Add(v, false)
	}
	bp2, _ := types.NewBlockProposal(2, 0, blk2, ls, nil)
	if bp2.LockSet() != ls {
		t.Error("locksets does not match")
	}
	if err := bp2.ValidateVotes(validators, validators); err == nil {
		t.Error("There should be an error") // missing signature
	}

	if err := bp2.Sign(keys[0]); err == nil {
		t.Error("There should be an error") // private key not match
	}

	if err := bp2.Sign(key1); err == nil {
		t.Error("There should be an error") // already signed
	}
	bp2, _ = types.NewBlockProposal(2, 0, blk2, ls, nil) // reset signature

	if err := bp2.Sign(key1); err != nil {
		fmt.Println(err)
		t.Error("error occur")
	}
	if err := bp2.ValidateVotes(validators, validators); err != nil {
		fmt.Println(err)
		t.Error("error occur")
	}

	bps1, _ := rlp.EncodeToBytes(bp2)
	var dbp1 *types.BlockProposal
	if err := rlp.Decode(bytes.NewReader(bps1), &dbp1); err != nil {
		fmt.Println(bp2, err)
		t.Error("decode blockproposal failed")
	}

	// block 2 round 1 , timeout in round 0
	rls := types.NewLockSet(uint64(len(validators)), nil)
	for _, key := range keys {
		v := types.NewVote(2, 0, common.Hash{}, 2)
		v.Sign(key)
		rls.Add(v, false)
	}
	bp2_1, err1 := types.NewBlockProposal(2, 1, blk2, ls, rls)
	if err1 != nil {
		t.Error(err1)
	}
	if err := bp2_1.Sign(key1); err != nil {
		t.Error("error occur")
	}
	if err := bp2_1.ValidateVotes(validators, validators); err != nil {
		t.Error("error occur")
	}
	// serilizable
	bps, _ := rlp.EncodeToBytes(bp2_1)
	var dbp *types.BlockProposal
	if err := rlp.Decode(bytes.NewReader(bps), &dbp); err != nil {
		t.Error("decode blockproposal failed")
	}
	if err := bp2_1.ValidateVotes(validators, validators); err != nil {
		t.Error("error occur")
	}

	// check quorumpossible lockset failure
	rls = types.NewLockSet(uint64(len(validators)), nil)
	for i, key := range keys {
		var v *types.Vote
		if i < 4 {
			v = types.NewVote(2, 0, bh, 1)
		} else {
			v = types.NewVote(2, 0, common.Hash{}, 2)
		}
		v.Sign(key)
		rls.Add(v, false)
	}
	if has, _ := rls.QuorumPossible(); !has {
		t.Error("there should be quorumpossible")
	}

	if _, err := types.NewBlockProposal(2, 1, blk2, ls, rls); err == nil { // NoQuorum necessary R0
		t.Error("there should be an error")
	}

}
func TestVotingInstruction(t *testing.T) {
	var keys []*ecdsa.PrivateKey
	var validators []common.Address
	for i := 0; i < 10; i++ {
		s := strconv.Itoa(i + 1)
		keys = append(keys, crypto.MakePrivatekey(s))
	}
	for _, key := range keys {
		validators = append(validators, crypto.PubkeyToAddress(key.PublicKey))
	}
	bh := common.HexToHash("11111111111111111111111111111111")

	rls := types.NewLockSet(uint64(len(validators)), nil)
	for i, key := range keys {
		var v *types.Vote
		if i < 4 {
			v = types.NewVote(2, 0, bh, 1)
		} else {
			v = types.NewVote(2, 0, common.Hash{}, 2)
		}
		v.Sign(key)
		rls.Add(v, false)
	}
	if has, _ := rls.QuorumPossible(); !has {
		t.Error("there should be quorumpossible")
	}
	vi, _ := types.NewVotingInstruction(2, 1, rls)
	if bh != vi.Blockhash() {
		t.Error("block hash does not match")
	}

	// no quorum
	rls = types.NewLockSet(uint64(len(validators)), nil)
	for i, key := range keys {
		var v *types.Vote
		if i < 3 {
			v = types.NewVote(2, 0, bh, 1)
		} else {
			v = types.NewVote(2, 0, common.Hash{}, 2)
		}
		v.Sign(key)
		rls.Add(v, false)
	}

	if has, _ := rls.QuorumPossible(); has {
		t.Error("there should be no quorum")
	}
	if !rls.NoQuorum() {
		t.Error("there should be no quorum")
	}

	if _, err := types.NewVotingInstruction(2, 1, rls); err == nil {
		t.Error("there should be an error")
	}
}
