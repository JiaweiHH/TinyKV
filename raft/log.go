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
	"github.com/pingcap-incubator/tinykv/log"
	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

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
	// 收到 leader 的快照的时候，会将快照保存在此处，后续会把快照保存到 Ready 中去
	// 上层应用会应用 Ready 里面的快照
	pendingSnapshot *pb.Snapshot

	// Your Data Here (2A).
	dummyIndex uint64
}

// newLog returns log using the given storage. It recovers the log
// to the state that it just commits and applies the latest snapshot.
//（这里的 Storage 就是 PeerStorage）
func newLog(storage Storage) *RaftLog {
	// Your Code Here (2A).
	firstIndex, _ := storage.FirstIndex()
	lastIndex, _ := storage.LastIndex()
	l := &RaftLog{
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
	l.committed = hardState.Commit
	entries, _ := storage.Entries(firstIndex, lastIndex+1)
	l.entries = append(l.entries, entries[0:]...)
	log.Infof("{newLog} firstIndex %v, lastIndex %v, len(entries) %v, committed %v", firstIndex, lastIndex, len(l.entries), l.committed)
	return l
}

func (l *RaftLog) FirstIndex() uint64 {
	return l.dummyIndex
}

// We need to compact the log entries in some point of time like
// storage compact stabled log entries prevent the log entries
// grow unlimitedly in memory
func (l *RaftLog) maybeCompact() {
	// Your Code Here (2C).
	newFirst, _ := l.storage.FirstIndex()
	if newFirst > l.dummyIndex {
		entries := l.entries[newFirst-l.dummyIndex:]
		l.entries = make([]pb.Entry, 0)
		l.entries = append(l.entries, entries...)
	}
	l.dummyIndex = newFirst
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
	// applied = 5, committed = 5 (dummyIndex = 6) ----> applied = 5, committed = 10
	// log: [6, 7, 8, 9, 10, 11, 12], we want entries in [applied + 1, committed + 1]
	// applied + 1 = applied - dummyIndex + 1, committed + 1 = committed - dummyIndex + 1
	diff := l.dummyIndex - 1
	if l.committed > l.applied {
		return l.entries[l.applied-diff : l.committed-diff]
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
	// 下面注释的写法是错误的，因为主动创建的 storage 的 INIT_INDEX = 5
	// 此时如果调用 hasReady 会执行 unstableEntries()->lastIndex()
	// 如果按照下面的写法，就会去获取 [l.stabled = 5, lastIndex + 1 - dummyIndex = -4] 的数据
	//if len(l.entries) == 0 {
	//	return 0
	//}
	//return l.entries[len(l.entries)-1].Index

	// 另外一个原因是 dummyIndex 初始的时候也是 INIT_INDEX+1(6)，因此根据 dummyIndex 来计算 lastIndex 是合理的
	return l.dummyIndex - 1 + uint64(len(l.entries))
}

// Term return the term of the entry in the given index
func (l *RaftLog) Term(i uint64) (uint64, error) {
	// Your Code Here (2A).
	// 1. 如果内存日志中包含这条日志，直接返回查询结果
	if i >= l.dummyIndex {
		return l.entries[i-l.dummyIndex].Term, nil
	}
	// 2. 判断 i 是否等于当前正准备安装的快照的最后一条日志
	if !IsEmptySnap(l.pendingSnapshot) && i == l.pendingSnapshot.Metadata.Index {
		return l.pendingSnapshot.Metadata.Term, nil
	}
	// 3. 否则的话 i 只能是快照中的日志
	term, err := l.storage.Term(i)
	return term, err
}

func (l *RaftLog) TermNoErr(i uint64) uint64 {
	// Your Code Here (2A).
	//if i < l.dummyIndex {
	//	return 0
	//}
	//entriesIndex := i - l.dummyIndex
	//return l.entries[entriesIndex].Term
	if i >= l.dummyIndex {
		return l.entries[i-l.dummyIndex].Term
	}
	if !IsEmptySnap(l.pendingSnapshot) && i == l.pendingSnapshot.Metadata.Index {
		return l.pendingSnapshot.Metadata.Term
	}
	term, _ := l.storage.Term(i)
	return term
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
