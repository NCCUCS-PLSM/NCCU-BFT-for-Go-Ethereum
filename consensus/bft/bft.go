package bft

import (
	"errors"
	"math/big"
	"math/rand"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	errZeroBlockTime     = errors.New("timestamp equals parent's")
	errInvalidDifficulty = errors.New("invalid difficulty")

	fixDifficulty = big.NewInt(1)
)

type BFT struct {
	config     *params.ChainConfig // Consensus engine configuration parameters
	db         ethdb.Database      // Database to store and retrieve snapshot checkpoints
	blockchain *core.BlockChain

	pm *ProtocolManager

	signer common.Address // Ethereum address of the signing key
}

func New(config *params.ChainConfig, db ethdb.Database) *BFT {
	conf := *config

	bft := &BFT{
		config: &conf,
		db:     db,
	}
	return bft
}

func (b *BFT) SetupProtocolManager(chainConfig *params.ChainConfig, networkId uint64, mux *event.TypeMux, txpool *core.TxPool, blockchain *core.BlockChain, chainDb ethdb.Database, bftDb ethdb.Database, vmConfig vm.Config, validators []common.Address, privateKeyHex string, etherbase common.Address, allowEmpty bool) error {
	var err error
	privkey, _ := crypto.HexToECDSA(privateKeyHex)
	// addr := crypto.ToECDSAPub(crypto.FromECDSA(privkey))
	b.signer = crypto.PubkeyToAddress(privkey.PublicKey)
	b.blockchain = blockchain
	if b.pm, err = NewProtocolManager(chainConfig, networkId, mux, txpool, blockchain, chainDb, bftDb, vmConfig, validators, privateKeyHex, etherbase, allowEmpty); err != nil {
		return err
	}
	return nil
}

func (b *BFT) Start() {
	b.pm.Start()
}

func (b *BFT) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (b *BFT) VerifyHeader(chain consensus.ChainReader, header *types.Header, seal bool) error {
	// Short circuit if the header is known, or it's parent not
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	return b.verifyHeader(chain, header)
}

func (b *BFT) VerifyHeaders(chain consensus.ChainReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		for _, header := range headers {
			err := b.verifyHeader(chain, header)
			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

func (b *BFT) verifyHeader(chain consensus.ChainReader, header *types.Header) error {
	if header.Time.Cmp(big.NewInt(time.Now().Unix())) > 0 {
		return consensus.ErrFutureBlock
	}
	number := header.Number.Uint64()

	if number > 0 {
		if header.Difficulty == nil || header.Difficulty.Cmp(fixDifficulty) != 0 {
			return errInvalidDifficulty
		}
	}
	return b.verifySeal(chain, header)
}

func (b *BFT) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errors.New("uncles not allowed")
	}
	return nil
}

func (b *BFT) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	return b.verifySeal(chain, header)
}

func (b *BFT) verifySeal(chain consensus.ChainReader, header *types.Header) error {
	if err := b.pm.consensusManager.verifyVotes(header); err != nil {
		log.Error("verifySeal failed", "err", err)
		return err
	}

	return nil
}

func (b *BFT) Prepare(chain consensus.ChainReader, header *types.Header) error {
	header.Difficulty = fixDifficulty
	header.Coinbase = b.signer
	return nil
}

func (b *BFT) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {

	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
	header.UncleHash = types.CalcUncleHash(nil)

	return types.NewBlock(header, txs, nil, receipts), nil
}

func (b *BFT) Seal(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	// start voting mechanism
	log.Info("Sealing", "block n", block.Number())
	abort := make(chan struct{})
	found := make(chan *types.Block)

	go b.pm.consensusManager.Process(block, abort, found)
	var result *types.Block

	select {
	case <-stop:
		log.Info("stop by outside", "height", block.Number())
		close(abort)
		return nil, nil
	case result = <-found:
		log.Info("have a consensus on the block")
		close(abort)
	}
	if result.Header().Coinbase != b.signer {
		delay := time.Duration(rand.Intn(5)+6) * 500 * time.Millisecond
		select {
		case <-stop:
			return nil, nil
		case <-time.After(delay):
		}
	}
	return result, nil
}

func (b *BFT) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "bft",
		Version:   "1.0",
		Service:   &API{chain: chain, bft: b},
		Public:    false,
	}}
}

func (b *BFT) Protocols() []p2p.Protocol {
	return b.pm.SubProtocols
}
