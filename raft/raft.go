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
	"github.com/pingcap-incubator/tinykv/log"
	rand2 "math/rand"
	"sort"
	"time"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// None is a placeholder node ID used when there is no leader.
const None uint64 = 0

// StateType represents the role of a node in a cluster.
type StateType uint64

const (
	StateFollower StateType = iota
	StateCandidate
	StateLeader
)

var stmap = [...]string{
	"StateFollower",
	"StateCandidate",
	"StateLeader",
}

func (st StateType) String() string {
	return stmap[uint64(st)]
}

// ErrProposalDropped is returned when the proposal is ignored by some cases,
// so that the proposer can be notified and fail fast.
var ErrProposalDropped = errors.New("raft proposal dropped")

// Config contains the parameters to start a raft.
type Config struct {
	// ID is the identity of the local raft. ID cannot be 0.
	ID uint64

	// peers contains the IDs of all nodes (including self) in the raft cluster. It
	// should only be set when starting a new raft cluster. Restarting raft from
	// previous configuration will panic if peers is set. peer is private and only
	// used for testing right now.
	peers []uint64

	// ElectionTick is the number of Node.Tick invocations that must pass between
	// elections. That is, if a follower does not receive any message from the
	// leader of current term before ElectionTick has elapsed, it will become
	// candidate and start an election. ElectionTick must be greater than
	// HeartbeatTick. We suggest ElectionTick = 10 * HeartbeatTick to avoid
	// unnecessary leader switching.
	ElectionTick int
	// HeartbeatTick is the number of Node.Tick invocations that must pass between
	// heartbeats. That is, a leader sends heartbeat messages to maintain its
	// leadership every HeartbeatTick ticks.
	HeartbeatTick int

	// Storage is the storage for raft. raft generates entries and states to be
	// stored in storage. raft reads the persisted entries and states test_result of
	// Storage when it needs. raft reads test_result the previous state and configuration
	// test_result of storage when restarting.
	Storage Storage
	// Applied is the last applied index. It should only be set when restarting
	// raft. raft will not return entries to the application smaller or equal to
	// Applied. If Applied is unset when restarting, raft might return previous
	// applied entries. This is a very application dependent configuration.
	Applied uint64
}

func (c *Config) validate() error {
	if c.ID == None {
		return errors.New("cannot use none as id")
	}

	if c.HeartbeatTick <= 0 {
		return errors.New("heartbeat tick must be greater than 0")
	}

	if c.ElectionTick <= c.HeartbeatTick {
		return errors.New("election tick must be greater than heartbeat tick")
	}

	if c.Storage == nil {
		return errors.New("storage cannot be nil")
	}

	return nil
}

// Progress represents a follower’s progress in the view of the leader. Leader maintains
// progresses of all followers, and sends entries to the follower based on its progress.
type Progress struct {
	// 分别用来记录：
	// 每个 follower 当前和 leader 同步到什么位置了
	// 每个 follower 下一个日志同步的位置，
	Match, Next uint64
}

type Raft struct {
	id uint64

	Term uint64
	// 关于 Vote 字段的更新时机：每个节点在一个选举期间只能投票一次
	// 一旦收到了 leader 的心跳或者日志同步消息就需要重置 Vote
	Vote uint64

	// the log
	RaftLog *RaftLog

	// log replication progress of each peers
	Prs map[uint64]*Progress

	// this peer's role
	State StateType

	// votes records（存放哪些节点投票给了本节点）
	votes map[uint64]bool

	// 统计有多少个节点给自己投票了
	granted int

	// msgs need to send
	msgs []pb.Message

	// the leader id
	Lead uint64

	// heartbeat interval, should send
	heartbeatTimeout int
	// baseline of election interval
	electionTimeout int
	// 随机超时选举，[electionTimeout, 2*electionTimeout)
	randomElectionTimeout int
	// number of ticks since it reached last heartbeatTimeout.
	// only leader keeps heartbeatElapsed.
	heartbeatElapsed int
	// Ticks since it reached last electionTimeout when it is leader or candidate.
	// Number of ticks since it reached last electionTimeout or received a
	// valid message from current leader when it is a follower.
	electionElapsed int

	transferElapsed int // 用于计时 transfer 的时间

	// leadTransferee is id of the leader transfer target when its value is not zero.
	// Follow the procedure defined in section 3.10 of Raft phd thesis.
	// (https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf)
	// (Used in 3A leader transfer)
	leadTransferee uint64

	// Only one conf change may be pending (in the log, but not yet
	// applied) at a time. This is enforced via PendingConfIndex, which
	// is set to a value >= the log index of the latest pending
	// configuration change (if any). Config changes are only allowed to
	// be proposed if the leader's applied index is greater than this
	// value.
	//（PendingConfIndex 表示当前还没有生效的 ConfChange，只有在日志被提交并应用之后才会生效）
	// (Used in 3A conf change)
	PendingConfIndex uint64
}

// newRaft return a raft peer with the given config
func newRaft(c *Config) *Raft {
	if err := c.validate(); err != nil {
		panic(err.Error())
	}
	// Your Code Here (2A).
	rf := &Raft{
		id:               c.ID,
		Prs:              make(map[uint64]*Progress),
		RaftLog:          newLog(c.Storage), // Log 也从持久化存储中读取
		State:            StateFollower,
		heartbeatTimeout: c.HeartbeatTick, electionTimeout: c.ElectionTick,
		heartbeatElapsed: 0, electionElapsed: 0,
	}
	hardState, conState, _ := c.Storage.InitialState()
	if c.peers == nil {
		c.peers = conState.Nodes
	}
	rf.Term, rf.Vote = hardState.Term, hardState.Vote // Term 和 Vote 从持久化存储中读取
	rf.resetRandomizedElectionTimeout()
	for _, id := range c.peers {
		rf.Prs[id] = &Progress{}
	}
	rf.PendingConfIndex = rf.initPendingConfIndex()
	return rf
}

// initPendingConfIndex 初始化 pendingConfIndex
// 查找 [appliedIndex + 1, lastIndex] 之间是否存在还没有 Apply 的 ConfChange Entry
func (r *Raft) initPendingConfIndex() uint64 {
	for i := r.RaftLog.applied + 1; i <= r.RaftLog.LastIndex(); i++ {
		if r.RaftLog.entries[i-r.RaftLog.FirstIndex()].EntryType == pb.EntryType_EntryConfChange {
			return i
		}
	}
	return None
}

// quorum 返回 majority 节点的数量
func (r *Raft) quorum() int {
	return len(r.Prs)/2 + 1
}

// maybeCommit 判断是否有新的日志需要提交
func (r *Raft) maybeCommit() bool {
	matchArray := make(uint64Slice, 0)
	for _, progress := range r.Prs {
		matchArray = append(matchArray, progress.Match)
	}
	// 获取所有节点 match 的中位数，就是被大多数节点复制的日志索引
	sort.Sort(sort.Reverse(matchArray))
	toCommitIndex := matchArray[r.quorum()-1]
	// 检查是否可以提交 toCommitIndex
	return r.RaftLog.maybeCommit(toCommitIndex, r.Term)
}

// maybeUpdate 检查日志同步是不是一个过期的回复
func (pr *Progress) maybeUpdate(n uint64) bool {
	var updated bool
	// 判断是否是过期的消息回复
	if pr.Match < n {
		pr.Match = n
		pr.Next = pr.Match + 1
		updated = true
	}
	return updated
}

// resetRandomizedElectionTimeout 生成随机选举超时时间，范围在 [r.electionTimeout, 2*r.electionTimeout]
func (r *Raft) resetRandomizedElectionTimeout() {
	rand := rand2.New(rand2.NewSource(time.Now().UnixNano()))
	r.randomElectionTimeout = r.electionTimeout + rand.Intn(r.electionTimeout)
}

func (r *Raft) appendEntry(entries []*pb.Entry) {
	lastIndex := r.RaftLog.LastIndex() // leader 最后一条日志的索引
	for i := range entries {
		// 设置新日志的索引和任期
		entries[i].Index = lastIndex + uint64(i) + 1
		entries[i].Term = r.Term
		if entries[i].EntryType == pb.EntryType_EntryConfChange {
			r.PendingConfIndex = entries[i].Index
		}
	}
	r.RaftLog.appendNewEntry(entries)
}

func (r *Raft) sendTimeoutNow(to uint64) {
	r.msgs = append(r.msgs, pb.Message{MsgType: pb.MessageType_MsgTimeoutNow, From: r.id, To: to})
}

// sendSnapshot 发送快照给别的节点
func (r *Raft) sendSnapshot(to uint64) {
	snapshot, err := r.RaftLog.storage.Snapshot()
	if err != nil {
		// 生成 Snapshot 的工作是由 region worker 异步执行的，如果 Snapshot 还没有准备好
		// 此时会返回 ErrSnapshotTemporarilyUnavailable 错误，此时 leader 应该放弃本次 Snapshot Request
		// 等待下一次再请求 storage 获取 snapshot（通常来说会在下一次 heartbeat response 的时候发送 snapshot）
		return
	}
	r.msgs = append(r.msgs, pb.Message{
		MsgType:  pb.MessageType_MsgSnapshot,
		From:     r.id,
		To:       to,
		Term:     r.Term,
		Snapshot: &snapshot,
	})
	r.Prs[to].Next = snapshot.Metadata.Index + 1
}

// sendAppend sends an append RPC with new entries (if any) and the
// current commit index to the given peer. Returns true if a message was sent.
func (r *Raft) sendAppend(to uint64) bool {
	// Your Code Here (2A).
	// prevLogIndex 和 prevLogTerm 用来判断 leader 和 follower 的日志是否冲突
	// 如果没有冲突的话就会将 Entries 追加或者覆盖到 follower 的日志中
	prevLogIndex := r.Prs[to].Next - 1
	prevLogTerm, err := r.RaftLog.Term(prevLogIndex)
	if err == nil {
		// 没有错误发生，说明 prevLogIndex 的下一条日志（nextIndex）位于内存日志数组中
		// 此时可以发送 Entries 数组给 followers
		appendMsg := pb.Message{
			MsgType: pb.MessageType_MsgAppend,
			From:    r.id,
			To:      to,
			Term:    r.Term,
			LogTerm: prevLogTerm,
			Index:   prevLogIndex,
			Entries: make([]*pb.Entry, 0),
			Commit:  r.RaftLog.committed,
		}
		nextEntries := r.RaftLog.getEntries(prevLogIndex+1, 0) // 期望覆盖或者追加到 follower 上的日志集合
		for i := range nextEntries {
			appendMsg.Entries = append(appendMsg.Entries, &nextEntries[i])
		}
		r.msgs = append(r.msgs, appendMsg)
		log.Infof("[AppendEntry Request]%d to %d, prevLogIndex = %d, len(Entries) = %d, lastIndex %v", r.id, to, prevLogIndex, len(nextEntries), r.RaftLog.LastIndex())
		return true
	}
	// 有错误，说明 nextIndex 存在于快照中，此时需要发送快照给 followers
	r.sendSnapshot(to)
	log.Infof("[Snapshot Request]%d to %d, prevLogIndex %v, dummyIndex %v", r.id, to, prevLogIndex, r.RaftLog.dummyIndex)
	return false
}

// sendHeartbeat sends a heartbeat RPC to the given peer.
func (r *Raft) sendHeartbeat(to uint64) {
	// Your Code Here (2A).
	r.msgs = append(r.msgs, pb.Message{
		MsgType: pb.MessageType_MsgHeartbeat,
		From:    r.id,
		To:      to,
		Term:    r.Term,
	})
}

// startElection 开始选举：1. 修改自身的状态 2. 填充消息
func (r *Raft) startElection() {
	r.becomeCandidate() // 1. 2. 3. （Term、Vote、election timeout）
	if len(r.Prs) == 1 {
		// 集群只有一个节点的时候，直接变成 Leader
		r.becomeLeader()
		return
	}
	// 4. 发起 RequestVote 请求，消息中的数据按照 Figure4 中进行填充。设置所有需要发送的消息
	for id := range r.Prs {
		if id == r.id {
			continue
		}
		r.msgs = append(r.msgs, pb.Message{
			MsgType: pb.MessageType_MsgRequestVote, // 消息类型
			To:      id,                            // 节点 ID
			From:    r.id,                          // 候选者编号
			Term:    r.Term,                        // 候选者任期
			LogTerm: r.RaftLog.LastTerm(),          // 候选者最后一条日志的索引
			Index:   r.RaftLog.LastIndex(),         // 候选者最后一条日志的任期
		})
	}
	r.granted = 1                   // 开始的时候只有自己投票给自己
	r.votes = make(map[uint64]bool) // 重置 votes
	r.votes[r.id] = true
	// 后续 RawNode 在 Ready 的时候会取走消息，并发送给别的节点
}

// broadcastHeartBeat 广播心跳消息
func (r *Raft) broadcastHeartBeat() {
	for id := range r.Prs {
		if id == r.id {
			continue
		}
		r.sendHeartbeat(id)
		log.Infof("[HeartBeat] %v to %v, committedIndex %v, lastIndex %v", r.id, id, r.RaftLog.committed, r.RaftLog.LastIndex())
	}
	r.heartbeatElapsed = 0
	// 等待 RawNode 取走 r.msgs 中的消息，发送心跳给别的节点
}

func (r *Raft) broadcastAppendEntry() {
	for id := range r.Prs {
		if id == r.id {
			continue
		}
		r.sendAppend(id)
	}
}

// tick advances the internal logical clock by a single tick.
func (r *Raft) tick() {
	// Your Code Here (2A).
	switch r.State {
	case StateLeader: // Leader 需要更新心跳时钟
		r.heartbeatElapsed++
		if r.heartbeatElapsed >= r.heartbeatTimeout {
			// MessageType_MsgBeat 属于内部消息，不需要经过 RawNode 处理
			r.Step(pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgBeat})
		}
		if r.leadTransferee != None {
			// 在选举超时后领导权禅让仍然未完成，则 leader 应该终止领导权禅让，这样可以恢复客户端请求
			r.transferElapsed++
			if r.transferElapsed >= r.electionTimeout {
				r.leadTransferee = None
			}
		}
	default: // Follower 和 Candidate 需要更新超时选举时钟
		r.electionElapsed++
		if r.electionElapsed >= r.randomElectionTimeout {
			// MessageType_MsgHup 属于内部消息，也不需要经过 RawNode 处理
			r.Step(pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgHup})
		}
	}
}

// becomeFollower transform this peer's state to Follower
func (r *Raft) becomeFollower(term uint64, lead uint64) {
	// Your Code Here (2A).
	if term > r.Term {
		// 只有 Term > currentTerm 的时候才需要对 Vote 进行重置
		// 这样可以保证在一个任期内只会进行一次投票
		r.Term = term
		r.Vote = None
	}
	r.State = StateFollower
	r.Lead = lead
	r.electionElapsed = 0
	r.leadTransferee = None
	r.resetRandomizedElectionTimeout()
}

// becomeCandidate transform this peer's state to candidate
func (r *Raft) becomeCandidate() {
	// Your Code Here (2A).
	r.State = StateCandidate // 0. 更改自己的状态
	r.Term++                 // 1. 增加自己的任期
	r.Vote = r.id            // 2. 投票给自己
	r.electionElapsed = 0    // 3. 重置超时选举计时器
	r.resetRandomizedElectionTimeout()
}

// becomeLeader transform this peer's state to leader
func (r *Raft) becomeLeader() {
	// Your Code Here (2A).
	// NOTE: Leader should propose a noop entry on its term
	r.State = StateLeader
	r.Lead = r.id

	// 初始化 nextIndex 和 matchIndex
	for id := range r.Prs {
		r.Prs[id].Next = r.RaftLog.LastIndex() + 1 // 初始化为 leader 的最后一条日志索引（后续出现冲突会往前移动）
		r.Prs[id].Match = 0                        // 初始化为 0 就可以了
	}
	r.PendingConfIndex = r.initPendingConfIndex()
	// 成为 Leader 之后立马在日志中追加一条 noop 日志，这是因为
	// 在 Raft 论文中提交 Leader 永远不会通过计算副本的方式提交一个之前任期、并且已经被复制到大多数节点的日志
	// 通过追加一条当前任期的 noop 日志，可以快速的提交之前任期内所有被复制到大多数节点的日志
	r.Step(pb.Message{MsgType: pb.MessageType_MsgPropose, Entries: []*pb.Entry{{}}})
}

func (r *Raft) poll(id uint64, voted bool) int {
	if _, ok := r.votes[id]; !ok {
		// 如果 id 没有投过票，那么就更新 id 的投票情况
		r.votes[id] = voted
		if voted {
			r.granted++
		}
	}
	return r.granted
}

// Step the entrance of handle message, see `MessageType`
// on `eraftpb.proto` for what msgs should be handled
func (r *Raft) Step(m pb.Message) error {
	// Your Code Here (2A).
	if _, ok := r.Prs[r.id]; !ok && m.MsgType == pb.MessageType_MsgTimeoutNow {
		return nil
	}
	switch r.State {
	case StateFollower: // Follower 可以接收到的消息：MsgHup、MsgRequestVote、MsgHeartBeat、MsgAppendEntry
		r.stepFollower(m)
	case StateCandidate: // Candidate 可以接收到的消息：MsgHup、MsgRequestVote、MsgRequestVoteResponse、MsgHeartBeat
		r.stepCandidate(m)
	case StateLeader: // Leader 可以接收到的消息：MsgBeat、MsgHeartBeatResponse、MsgRequestVote
		r.stepLeader(m)
	}
	return nil
}

// stepFollower Follower 的状态机
func (r *Raft) stepFollower(m pb.Message) {
	switch m.MsgType {
	case pb.MessageType_MsgHup:
		r.startElection()
	case pb.MessageType_MsgRequestVote:
		r.handleRequestVote(m)
	case pb.MessageType_MsgHeartbeat:
		r.handleHeartbeat(m)
	case pb.MessageType_MsgAppend:
		r.handleAppendEntries(m)
	case pb.MessageType_MsgSnapshot:
		r.handleSnapshot(m)
	case pb.MessageType_MsgTimeoutNow:
		r.handleTimeoutNowRequest(m)
	case pb.MessageType_MsgTransferLeader:
		// 非 leader 收到领导权禅让消息，需要转发给 leader
		if r.Lead != None {
			m.To = r.Lead
			r.msgs = append(r.msgs, m)
		}
	}
}

// stepCandidate Candidate 的状态机
func (r *Raft) stepCandidate(m pb.Message) {
	switch m.MsgType {
	case pb.MessageType_MsgHup:
		r.startElection()
	case pb.MessageType_MsgRequestVote:
		r.handleRequestVote(m)
	case pb.MessageType_MsgRequestVoteResponse:
		r.handleRequestVoteResponse(m)
	case pb.MessageType_MsgHeartbeat:
		r.handleHeartbeat(m)
	case pb.MessageType_MsgAppend:
		r.handleAppendEntries(m)
	case pb.MessageType_MsgSnapshot:
		r.handleSnapshot(m)
	case pb.MessageType_MsgTransferLeader:
		// 非 leader 收到领导权禅让消息，需要转发给 leader
		if r.Lead != None {
			m.To = r.Lead
			r.msgs = append(r.msgs, m)
		}
	}
}

// stepLeader Leader 的状态机
func (r *Raft) stepLeader(m pb.Message) {
	switch m.MsgType {
	case pb.MessageType_MsgBeat: // 广播心跳
		r.broadcastHeartBeat()
	case pb.MessageType_MsgHeartbeatResponse: // 收到其他节点的心跳 response
		r.handleHeartbeatResponse(m)
	case pb.MessageType_MsgRequestVote: // 收到其他节点的 RequestVote 请求
		r.handleRequestVote(m)
	case pb.MessageType_MsgAppend: // 收到别的 leader 的 AppendEntry 请求
		r.handleAppendEntries(m)
	case pb.MessageType_MsgPropose:
		r.handlePropose(m)
	case pb.MessageType_MsgAppendResponse: // 收到 AppendEntry response
		r.handleAppendEntriesResponse(m)
	case pb.MessageType_MsgTransferLeader: // 领导权禅让消息
		r.handleTransferLeader(m)
	}
}

func (r *Raft) handleTimeoutNowRequest(m pb.Message) {
	// 直接发起选举
	if err := r.Step(pb.Message{MsgType: pb.MessageType_MsgHup}); err != nil {
		log.Panic(err)
	}
}

func (r *Raft) handleTransferLeader(m pb.Message) {
	// 判断 transferee 是否在集群中
	if _, ok := r.Prs[m.From]; !ok {
		return
	}
	// 如果 transferee 就是 leader 自身，则无事发生
	if m.From == r.id {
		return
	}
	// 判断是否有转让流程正在进行，如果是相同节点的转让流程就返回，否则的话终止上一个转让流程
	if r.leadTransferee != None {
		if r.leadTransferee == m.From {
			return
		}
		r.leadTransferee = None
	}
	r.leadTransferee = m.From
	r.transferElapsed = 0
	if r.Prs[m.From].Match == r.RaftLog.LastIndex() {
		// 日志是最新的，直接发送 TimeoutNow 消息
		r.sendTimeoutNow(m.From)
	} else {
		// 日志不是最新的，则帮助 leadTransferee 匹配最新的日志
		r.sendAppend(m.From)
	}
}

// handleRequestVote 节点收到 RequestVote 请求时候的处理
func (r *Raft) handleRequestVote(m pb.Message) {
	voteRep := pb.Message{MsgType: pb.MessageType_MsgRequestVoteResponse, From: r.id, To: m.From, Term: r.Term}
	if m.Term > r.Term {
		r.becomeFollower(m.Term, None)
	}
	if (m.Term > r.Term || (m.Term == r.Term && (r.Vote == None || r.Vote == m.From))) && r.RaftLog.isUpToDate(m.Index, m.LogTerm) {
		//【投票】1. Candidate 任期大于自己并且日志足够新
		// 2. Candidate 任期和自己相等并且自己在当前任期内没有投过票或者已经投给了 Candidate，并且 Candidate 的日志足够新
		r.becomeFollower(m.Term, None)
		r.Vote = m.From
	} else {
		// 【拒绝投票】1. Candidate 的任期小于自己（Condition 1 不满足）
		// 2. 自己在当前任期已经投过票了（Condition 1.2 不满足）
		// 3. Candidate 的日志不够新（Condition 2 不满足）
		voteRep.Reject = true
	}
	r.msgs = append(r.msgs, voteRep)
}

// handleRequestVoteResponse 节点收到 RequestVote Response 时候的处理
func (r *Raft) handleRequestVoteResponse(m pb.Message) {
	count := r.poll(m.From, !m.Reject) // 更新节点的投票信息
	if m.Reject {
		if m.Term > r.Term {
			// 如果某个节点拒绝了 RequestVote 请求，并且任期大于自身的任期
			// 那么 Candidate 回到 Follower 状态
			r.becomeFollower(m.Term, None)
		}
		if len(r.votes)-count >= r.quorum() {
			// 半数以上节点拒绝投票给自己，此时需要变回 follower
			r.becomeFollower(r.Term, None)
		}
	} else {
		if count >= r.quorum() {
			// 半数以上节点投票给自己，此时可以成为 leader
			r.becomeLeader()
		}
	}
}

// handlePropose 追加从上层应用接收到的新日志，并广播给 follower
func (r *Raft) handlePropose(m pb.Message) {
	r.appendEntry(m.Entries)
	// leader 处于领导权禅让，停止接收新的请求
	if r.leadTransferee != None {
		return
	}
	r.Prs[r.id].Match = r.RaftLog.LastIndex()
	r.Prs[r.id].Next = r.RaftLog.LastIndex() + 1
	if len(r.Prs) == 1 {
		r.RaftLog.commit(r.RaftLog.LastIndex())
	} else {
		r.broadcastAppendEntry()
	}
}

// handleAppendEntries handle AppendEntries RPC request
// 1. reply false if term < currentTerm
// 2. reply false if log doesn't contain an entry at prevLogIndex whose term matches prevLogTerm
// 3. if an existing entry conflict with a new one, delete the existing entry and all follow it
// 4. append any new entries not already in the log
// 5. if leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
func (r *Raft) handleAppendEntries(m pb.Message) {
	// Your Code Here (2A).
	appendEntryResp := pb.Message{MsgType: pb.MessageType_MsgAppendResponse, From: r.id, To: m.From, Term: r.Term}
	appendEntryResp.Reject = true
	if m.Term >= r.Term {
		r.becomeFollower(m.Term, m.From)

		//dummyIndex := r.RaftLog.dummyIndex
		if r.RaftLog.LastIndex() < m.Index || r.RaftLog.TermNoErr(m.Index) != m.LogTerm {
			// prevLogIndex 出现冲突（不存在或者任期冲突），这时不能直接将 leader 传递过来的 Entries 覆盖到 follower 日志上
			// 这里可以直接返回，以便让 leader 尝试 prevLogIndex - 1 这条日志，但是这样 follower 和 leader 之间的同步比较慢

			// 日志冲突优化：找到冲突任期的第一条日志，下次 leader 发送 AppendEntry 的时候会将 nextIndex 设置为 ConflictIndex
			// 如果找不到的话就设置为 prevLogIndex 的前一个
			appendEntryResp.Index = r.RaftLog.LastIndex() // 用于提示 leader prevLogIndex 的开始位置
			if r.RaftLog.LastIndex() >= m.Index {
				conflictTerm := r.RaftLog.TermNoErr(m.Index)
				for _, ent := range r.RaftLog.entries {
					if ent.Term == conflictTerm {
						appendEntryResp.Index = ent.Index - 1
						break
					}
				}
			}
			log.Infof("[handle AppendEntry]peer %d conflict with %d, reply Index %v", r.id, m.Index, appendEntryResp.Index)
		} else {
			// prevLogIndex 没有冲突

			if len(m.Entries) > 0 {
				idx, newLogIndex := m.Index+1, m.Index+1
				// 找到 follower 和 leader 在 new log 中出现冲突的位置
				for ; idx < r.RaftLog.LastIndex() && idx <= m.Entries[len(m.Entries)-1].Index; idx++ {
					term, _ := r.RaftLog.Term(idx)
					if term != m.Entries[idx-newLogIndex].Term {
						break
					}
				}
				if idx-newLogIndex != uint64(len(m.Entries)) {
					r.RaftLog.truncate(idx)                               // 截断冲突后面的所有日志
					r.RaftLog.appendNewEntry(m.Entries[idx-(m.Index+1):]) // 并追加新的的日志
					r.RaftLog.stabled = min(r.RaftLog.stabled, idx-1)     // 更新持久化的日志索引
					// 注意，如果 leader 传递过来的新日志 follower 都已经有了的话就不应该对 follower 的日志进行截断
					// 例如 ......prevLogIndex......m.Entries[len(m.Entries) - 1].Index......other entry......
					// 与 leader 进行日志同步之后依旧保持原状
					// 原因是，leader 可能对 follower 发送日志 1552(3)，然后此时 client 又发出了请求，leader 再次发送 1552(4)
					// 并且 1552(4) 先被处理，然后 1552(3) 到达了，此时 1552(3) 上面的日志 follower 都已经有了，但是 1556 是最新同步的
					// 这时候不应该删除 1556 这条日志，需要保留
				}
			}
			// 更新 commitIndex
			if m.Commit > r.RaftLog.committed {
				// 取当前节点「已经和 leader 同步的日志」和 leader 「已经提交日志」索引的最小值作为节点的 commitIndex
				r.RaftLog.commit(min(m.Commit, m.Index+uint64(len(m.Entries))))
			}
			appendEntryResp.Reject = false
			appendEntryResp.Index = m.Index + uint64(len(m.Entries))             // 用于 leader 更新 Next（存储的是下一次 AppendEntry 的 prevIndex）
			appendEntryResp.LogTerm = r.RaftLog.TermNoErr(appendEntryResp.Index) // 用于 leader 更新 committed
			log.Infof("[handle AppendEntry] peer %d prevLogIndex %d, len(m.Entry) %d, resp.Index %v, commitIndex %v", r.id, m.Index, len(m.Entries), appendEntryResp.Index, r.RaftLog.committed)
		}
	}
	r.msgs = append(r.msgs, appendEntryResp)
}

func (r *Raft) handleAppendEntriesResponse(m pb.Message) {
	log.Infof("[AppendEntry Response]from %d, Index = %d", m.From, m.Index)
	if m.Reject {
		if m.Term > r.Term {
			// 任期大于自己，那么就变为 Follower
			r.becomeFollower(m.Term, None)
		} else {
			// 否则就是因为 prevLog 日志冲突，继续尝试对 follower 同步日志
			r.Prs[m.From].Next = m.Index + 1
			r.sendAppend(m.From) // 再次发送 AppendEntry Request
		}
		return
	}
	if r.Prs[m.From].maybeUpdate(m.Index) {
		// 由于有新的日志被复制了，因此有可能有新的日志可以提交执行，所以判断一下
		if r.maybeCommit() {
			log.Infof("[commit] %v leader commit new entry, commitIndex %v", r.id, r.RaftLog.committed)
			r.broadcastAppendEntry() // 广播更新所有 follower 的 commitIndex
		}
	}
	if r.leadTransferee == m.From && r.Prs[m.From].Match == r.RaftLog.LastIndex() {
		// AppendEntryResponse 回复来自 leadTransferee，检查日志是否是最新的
		// 如果 leadTransferee 达到了最新的日志则立即发起领导权禅让
		r.sendTimeoutNow(m.From)
	}
}

func (r *Raft) handleHeartbeatResponse(m pb.Message) {
	if m.Reject {
		// heartbeat 被拒绝的原因只可能是对方节点的 Term 更大
		r.becomeFollower(m.Term, None)
	} else {
		// 心跳同步成功

		// 检查该节点的日志是不是和自己是同步的，由于有些节点断开连接并又恢复了链接
		// 因此 leader 需要及时向这些节点同步日志
		if r.Prs[m.From].Match < r.RaftLog.LastIndex() {
			r.sendAppend(m.From)
		}
	}
}

// handleHeartbeat handle Heartbeat RPC request
func (r *Raft) handleHeartbeat(m pb.Message) {
	// Your Code Here (2A).
	heartBeatResp := pb.Message{
		MsgType: pb.MessageType_MsgHeartbeatResponse,
		From:    r.id, To: m.From,
		Term: r.Term,
	}
	if m.Term < r.Term {
		heartBeatResp.Reject = true
	} else {
		r.becomeFollower(m.Term, m.From)
	}
	r.msgs = append(r.msgs, heartBeatResp)
}

// handleSnapshot handle Snapshot RPC request
//（从 SnapshotMetadata 中恢复 Raft 的内部状态，例如 term、commit、membership information）
func (r *Raft) handleSnapshot(m pb.Message) {
	// Your Code Here (2C).
	resp := pb.Message{MsgType: pb.MessageType_MsgAppendResponse, From: r.id, Term: r.Term}
	meta := m.Snapshot.Metadata
	if m.Term < r.Term {
		// 1. 如果 term 小于自身的 term 直接拒绝这次快照的数据
		resp.Reject = true
		log.Infof("[handle snapshot] %v reject because m.Term < r.Term", r.id)
	} else if r.RaftLog.committed >= meta.Index {
		// 2. 如果已经提交的日志大于等于快照中的日志，也需要拒绝这次快照
		// 因为 commit 的日志必定会被 apply，如果被快照中的日志覆盖的话就会破坏一致性
		resp.Index = r.RaftLog.committed
		log.Infof("[handle snapshot] %v reject snapshot", r.id)
	} else {
		// 3. 需要安装日志
		r.becomeFollower(m.Term, m.From)
		// 更新日志数据
		r.RaftLog.dummyIndex = meta.Index + 1
		r.RaftLog.committed = meta.Index
		r.RaftLog.applied = meta.Index
		r.RaftLog.stabled = meta.Index
		r.RaftLog.pendingSnapshot = m.Snapshot
		r.RaftLog.entries = make([]pb.Entry, 0)
		// 更新集群配置
		r.Prs = make(map[uint64]*Progress)
		for _, id := range meta.ConfState.Nodes {
			r.Prs[id] = &Progress{Next: r.RaftLog.LastIndex() + 1}
		}
		// 更新 response，提示 leader 更新 nextIndex
		resp.Index = meta.Index
		log.Infof("[handle snapshot] %v install snapshot, truncatedIndex %v, dummyIndex %v", r.id, meta.Index, r.RaftLog.dummyIndex)
	}
	r.msgs = append(r.msgs, resp)
}

// addNode add a new node to raft group
func (r *Raft) addNode(id uint64) {
	// Your Code Here (3A).
	if _, ok := r.Prs[id]; !ok {
		r.Prs[id] = &Progress{Next: r.RaftLog.LastIndex() + 1}
		r.PendingConfIndex = None // 清除 PendingConfIndex 表示当前没有未完成的配置更新
	}
}

// removeNode remove a node from raft group
func (r *Raft) removeNode(id uint64) {
	// Your Code Here (3A).
	if _, ok := r.Prs[id]; ok {
		delete(r.Prs, id)
		// 如果是删除节点，由于有节点被移除了，这个时候可能有新的日志可以提交
		// 这是必要的，因为 TinyKV 只有在 handleAppendRequestResponse 的时候才会判断是否有新的日志可以提交
		// 如果节点被移除了，则可能会因为缺少这个节点的回复，导致可以提交的日志无法在当前任期被提交
		if r.State == StateLeader && r.maybeCommit() {
			log.Infof("[removeNode commit] %v leader commit new entry, commitIndex %v", r.id, r.RaftLog.committed)
			r.broadcastAppendEntry() // 广播更新所有 follower 的 commitIndex
		}
	}
	r.PendingConfIndex = None // 清除 PendingConfIndex 表示当前没有未完成的配置更新
}
