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

import pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"

// RaftLog manage the log entries, its struct look like:
//
//  snapshot/first.....applied....committed....stabled.....last
//  --------|------------------------------------------------|
//                            log entries
//
// for simplify the RaftLog implement should manage all log entries
// that not truncated
type RaftLog struct {
	// storage contains all stable entries since the last snapshot.
	storage Storage

	// committed is the highest log position that is known to be in
	// stable storage on a quorum of nodes.
	//（committed 保存的是已经提交的 index）
	committed uint64

	// applied is the highest log position that the application has
	// been instructed to apply to its state machine.
	// Invariant: applied <= committed
	//（applied 保存的是传入到状态机中最高的 index，日志首先要提交成功才会被 applied 到状态机）
	applied uint64

	// log entries with index <= stabled are persisted to storage.
	// It is used to record the logs that are not persisted by storage yet.
	// Everytime handling `Ready`, the unstabled logs will be included.
	//（stabled 保存的是已经持久化到 storage 的 index）
	stabled uint64

	// all entries that have not yet compact.
	entries []pb.Entry

	// the incoming unstable snapshot, if any.
	// (Used in 2C)
	pendingSnapshot *pb.Snapshot

	// Your Data Here (2A).
	dummyIndex uint64
}

// newLog returns log using the given storage. It recovers the log
// to the state that it just commits and applies the latest snapshot.
func newLog(storage Storage) *RaftLog {
	// Your Code Here (2A).
	firstIndex, _ := storage.FirstIndex()
	lastIndex, _ := storage.LastIndex()
	log := &RaftLog{
		storage:    storage,
		committed:  firstIndex - 1,
		applied:    firstIndex - 1,
		stabled:    lastIndex,
		entries:    make([]pb.Entry, 0),
		dummyIndex: firstIndex,
	}
	// TinyKV 将 commitIndex 也作为持久化数据，尽管 Raft 论文中没有要求
	// commitIndex 不持久后续会通过 AppendEntry Request 慢慢更新回重新启动之前的值
	// 但是这么做的缺点就是上层应用需要去除重复的日志提交，例如使用递增序列号
	hardState, _, _ := storage.InitialState()
	log.committed = hardState.Commit
	entries, _ := storage.Entries(firstIndex, lastIndex+1)
	log.entries = append(log.entries, entries[0:]...)
	return log
}

// We need to compact the log entries in some point of time like
// storage compact stabled log entries prevent the log entries
// grow unlimitedly in memory
func (l *RaftLog) maybeCompact() {
	// Your Code Here (2C).
}

// unstableEntries return all the unstable entries（返回所有未持久化到 storage 的日志）
func (l *RaftLog) unstableEntries() []pb.Entry {
	// Your Code Here (2A).
	if l.LastIndex()-l.stabled == 0 {
		return make([]pb.Entry, 0)
	}
	return l.getEntries(l.stabled+1, 0)
}

// nextEnts returns all the committed but not applied entries
//（返回所有已经 commit 但是没有 applied 的日志，包括之前任期内的 commit 日志）
func (l *RaftLog) nextEnts() (ents []pb.Entry) {
	// Your Code Here (2A).
	appliedIndex := l.applied
	if l.committed > appliedIndex {
		return l.entries[appliedIndex:l.committed]
	}
	return make([]pb.Entry, 0)
}

// isUpToDate determines if the given (lastIndex,term) log is more up-to-date
// by comparing the index and term of the last entries in the existing logs.
// If the logs have last entries with different terms, then the log with the
// later term is more up-to-date. If the logs end with the same term, then
// whichever log has the larger lastIndex is more up-to-date. If the logs are
// the same, the given log is up-to-date.
func (l *RaftLog) isUpToDate(index, term uint64) bool {
	return term > l.LastTerm() || (term == l.LastTerm() && index >= l.LastIndex())
}

// LastIndex return the last index of the log entries
func (l *RaftLog) LastIndex() uint64 {
	// Your Code Here (2A).
	if len(l.entries) == 0 {
		return 0
	}
	return l.entries[len(l.entries)-1].Index
}

// Term return the term of the entry in the given index
func (l *RaftLog) Term(i uint64) (uint64, error) {
	// Your Code Here (2A).
	if i == 0 {
		return 0, nil
	}
	entriesIndex := i - l.dummyIndex
	return l.entries[entriesIndex].Term, nil
}

func (l *RaftLog) TermNoErr(i uint64) uint64 {
	// Your Code Here (2A).
	if i == 0 {
		return 0
	}
	entriesIndex := i - l.dummyIndex
	return l.entries[entriesIndex].Term
}

// LastTerm 返回最后一条日志的索引
func (l *RaftLog) LastTerm() uint64 {
	if len(l.entries) == 0 {
		return 0
	}
	lastIndex := l.LastIndex() - l.dummyIndex
	return l.entries[lastIndex].Term
}

// getEntries 返回 [start, end) 之间的所有日志，end = 0 表示返回 start 开始的所有日志
func (l *RaftLog) getEntries(start uint64, end uint64) []pb.Entry {
	if end == 0 {
		end = l.LastIndex() + 1
	}
	start, end = start-l.dummyIndex, end-l.dummyIndex
	return l.entries[start:end]
}

// appendEntry 添加新的日志，并返回最后一条日志的索引
func (l *RaftLog) appendNewEntry(ents []*pb.Entry) uint64 {
	for i := range ents {
		l.entries = append(l.entries, *ents[i])
	}
	return l.LastIndex()
}

func (l *RaftLog) getLength() uint64 {
	return uint64(len(l.entries))
}

func (l *RaftLog) truncate(startIndex uint64) {
	if l.getLength() > 0 {
		l.entries = l.entries[:startIndex-l.dummyIndex]
	}
}

func (l *RaftLog) commit(toCommit uint64) {
	l.committed = toCommit
}

// maybeCommit 检查一个被大多数节点复制的日志是否需要提交
func (l *RaftLog) maybeCommit(toCommit, term uint64) bool {
	commitTerm, _ := l.Term(toCommit)
	if toCommit > l.committed && commitTerm == term {
		// 只有当该日志被大多数节点复制（函数调用保证），并且日志索引大于当前的commitIndex（Condition 1）
		// 并且该日志是当前任期内创建的日志（Condition 2），才可以提交这条日志
		// 【注】为了一致性，Raft 永远不会通过计算副本的方式提交之前任期的日志，只能通过提交当前任期的日志一并提交之前所有的日志
		l.commit(toCommit)
		return true
	}
	return false
}
