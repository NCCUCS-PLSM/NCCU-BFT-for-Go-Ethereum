package bft

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/bft/types"
)

const (
	MaxGetproposalsCount = 10
)

// eth protocol message codes
const (
	// Protocol messages belonging to bft
	StatusMsg               = 0x01
	ReadyMsg                = 0x08
	GetPrecommitLocksetsMsg = 0x09
	PrecommitLocksetMsg     = 0x0a
	NewBlockProposalMsg     = 0x0b
	VotingInstructionMsg    = 0x0c
	VoteMsg                 = 0x11
	PrecommitVoteMsg        = 0x12
)

type HDCStatusData struct {
	ProtocolVersion uint32
	NetworkId       uint32
	TD              *big.Int
	CurrentLockset  *types.LockSet
	GenesisBlock    common.Hash
}

// Requests a BlockProposals message detailing a number of blocks to be sent, each referred to
// by block number. Note: Don't expect that the peer necessarily give you all these blocks
// in a single message - you might have to re-request them.

type blockProposalsData struct {
	PrecommitLockset []*types.PrecommitLockSet
}
type newBlockProposals struct {
	BlockProposal *types.BlockProposal
}
type votingInstructionData struct {
	VotingInstruction *types.VotingInstruction
}
type voteData struct {
	Vote *types.Vote
}
type precommitVoteData struct {
	PrecommitVote *types.PrecommitVote
}
type readyData struct {
	Ready *types.Ready
}
