// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package raft

import (
	"errors"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// ErrStepLocalMsg is returned when try to step a local raft message
var ErrStepLocalMsg = errors.New("raft: cannot step raft local message")

// ErrStepPeerNotFound is returned when try to step a response message
// but there is no peer found in raft.Prs for that node.
var ErrStepPeerNotFound = errors.New("raft: cannot step as peer not found")

// SoftState provides state that is volatile and does not need to be persisted to the WAL.
type SoftState struct {
	Lead      uint64
	RaftState StateType
}

// Ready encapsulates the entries and messages that are ready to read,
// be saved to stable storage, committed or sent to other peers.
// All fields in Ready are read-only.
// Ready 结构体用于保存已经处于 ready 状态的日志和消息
// 这些都是准备保存到持久化存储、提交或者发送给其他节点的
type Ready struct {
	// The current volatile state of a Node.
	// SoftState will be nil if there is no update.
	// It is not required to consume or store SoftState.
	*SoftState

	// The current state of a Node to be saved to stable storage BEFORE
	// Messages are sent.（发送 Message 之前保存到稳定存储的硬状态）
	// HardState will be equal to empty state if there is no update.（没有更新的时候 HardState 是空的）
	pb.HardState

	// Entries specifies entries to be saved to stable storage BEFORE
	// Messages are sent.
	// 需要在发送消息之前被写入到 Storage 的日志
	Entries []pb.Entry

	// Snapshot specifies the snapshot to be saved to stable storage.
	Snapshot pb.Snapshot

	// CommittedEntries specifies entries to be committed to a
	// store/state-machine. These have previously been committed to stable
	// store.
	// 需要被输入到状态机中的日志，这些日志之前已经被保存到 Storage 中了
	CommittedEntries []pb.Entry

	// Messages specifies outbound messages to be sent AFTER Entries are
	// committed to stable storage.
	// If it contains a MessageType_MsgSnapshot message, the application MUST report back to raft
	// when the snapshot has been received or has failed by calling ReportSnapshot.
	// 在日志被写入到 Storage 之后需要发送的消息
	Messages []pb.Message
}

// RawNode is a wrapper of Raft.（RawNode 仅仅是对 Raft 的封装）
type RawNode struct {
	Raft *Raft
	// Your Data Here (2A).
	prevSoftSt *SoftState   // 上一阶段的 Soft State
	prevHardSt pb.HardState // 上一阶段的 Hard State
}

// NewRawNode returns a new RawNode given configuration and a list of raft peers.
func NewRawNode(config *Config) (*RawNode, error) {
	// Your Code Here (2A).
	rn := &RawNode{
		Raft: newRaft(config), // 创建底层 Raft 节点
	}
	rn.prevHardSt, _, _ = config.Storage.InitialState()
	return rn, nil
}

// Tick advances the internal logical clock by a single tick.
func (rn *RawNode) Tick() {
	rn.Raft.tick()
}

// Campaign causes this RawNode to transition to candidate state.
func (rn *RawNode) Campaign() error {
	// 强制发起选举
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgHup,
	})
}

// Propose proposes data be appended to the raft log.
func (rn *RawNode) Propose(data []byte) error {
	ent := pb.Entry{Data: data}
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgPropose,
		From:    rn.Raft.id,
		Entries: []*pb.Entry{&ent}})
}

// ProposeConfChange proposes a config change.
func (rn *RawNode) ProposeConfChange(cc pb.ConfChange) error {
	data, err := cc.Marshal()
	if err != nil {
		return err
	}
	ent := pb.Entry{EntryType: pb.EntryType_EntryConfChange, Data: data}
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgPropose,
		Entries: []*pb.Entry{&ent},
	})
}

// ApplyConfChange applies a config change to the local node.
func (rn *RawNode) ApplyConfChange(cc pb.ConfChange) *pb.ConfState {
	if cc.NodeId == None {
		return &pb.ConfState{Nodes: nodes(rn.Raft)}
	}
	switch cc.ChangeType {
	case pb.ConfChangeType_AddNode:
		rn.Raft.addNode(cc.NodeId)
	case pb.ConfChangeType_RemoveNode:
		rn.Raft.removeNode(cc.NodeId)
	default:
		panic("unexpected conf type")
	}
	return &pb.ConfState{Nodes: nodes(rn.Raft)}
}

// Step advances the state machine using the given message.
func (rn *RawNode) Step(m pb.Message) error {
	// ignore unexpected local messages receiving over network
	if IsLocalMsg(m.MsgType) {
		return ErrStepLocalMsg
	}
	if pr := rn.Raft.Prs[m.From]; pr != nil || !IsResponseMsg(m.MsgType) {
		return rn.Raft.Step(m)
	}
	return ErrStepPeerNotFound
}

// softStateUpdate 检查 SoftState 是否有更新
func (rn *RawNode) isSoftStateUpdate() bool {
	return rn.prevSoftSt != nil && (rn.Raft.Lead != rn.prevSoftSt.Lead || rn.Raft.State != rn.prevSoftSt.RaftState)
}

// hardStateUpdate 检查 HardState 是否有更新
func (rn *RawNode) isHardStateUpdate() bool {
	return rn.Raft.Term != rn.prevHardSt.Term || rn.Raft.Vote != rn.prevHardSt.Vote ||
		rn.Raft.RaftLog.committed != rn.prevHardSt.Commit
}

// Ready returns the current point-in-time state of this RawNode.
func (rn *RawNode) Ready() Ready {
	// Your Code Here (2A).
	rd := Ready{
		Messages:         rn.Raft.msgs,
		Entries:          rn.Raft.RaftLog.unstableEntries(),
		CommittedEntries: rn.Raft.RaftLog.nextEnts(),
	}
	if rn.isSoftStateUpdate() {
		rd.SoftState = &SoftState{Lead: rn.Raft.Lead, RaftState: rn.Raft.State}
	}
	if rn.isHardStateUpdate() {
		rd.HardState = pb.HardState{
			Term:   rn.Raft.Term,
			Vote:   rn.Raft.Vote,
			Commit: rn.Raft.RaftLog.committed,
		}
	}
	rn.Raft.msgs = nil // 将消息传递到上层应用之后需要清空节点的消息队列
	return rd
}

// HasReady called when RawNode user need to check if any Ready pending.
func (rn *RawNode) HasReady() bool {
	// Your Code Here (2A).
	return len(rn.Raft.msgs) > 0 || rn.isSoftStateUpdate() || rn.isHardStateUpdate() ||
		len(rn.Raft.RaftLog.unstableEntries()) > 0 || len(rn.Raft.RaftLog.nextEnts()) > 0
}

// Advance notifies the RawNode that the application has applied and saved progress in the
// last Ready results.
func (rn *RawNode) Advance(rd Ready) {
	// Your Code Here (2A).
	if rd.SoftState != nil {
		// 不等于 nil 说明上次执行 Ready 更新了 softState
		rn.prevSoftSt = rd.SoftState
	}
	if rd.HardState.Term != None || rd.HardState.Vote != None || rd.HardState.Commit != None {
		// 检查 HardState 是否是默认值，默认值说明没有更新，此时不应该更新 prevHardSt
		rn.prevHardSt = rd.HardState
	}
	rn.Raft.RaftLog.stabled += uint64(len(rd.Entries))
	rn.Raft.RaftLog.applied += uint64(len(rd.CommittedEntries))
}

// GetProgress return the Progress of this node and its peers, if this
// node is leader.
func (rn *RawNode) GetProgress() map[uint64]Progress {
	prs := make(map[uint64]Progress)
	if rn.Raft.State == StateLeader {
		for id, p := range rn.Raft.Prs {
			prs[id] = *p
		}
	}
	return prs
}

// TransferLeader tries to transfer leadership to the given transferee.
func (rn *RawNode) TransferLeader(transferee uint64) {
	_ = rn.Raft.Step(pb.Message{MsgType: pb.MessageType_MsgTransferLeader, From: transferee})
}
