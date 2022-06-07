# Project2 RaftKV

## Part A

RawNode 相当于负责向 Raft 输入数据，Raft 得到输入数据之后会进行处理，同时会将一些日志数据存放到 RaftLog 中。Raft 处理完成之后，会将需要发送出去的数据放到 msgs 消息队列中，RawNode 会从中取走消息封装好，然后传递给上层应用，上层应用会发送给别的节点

- 调用 `Step` 驱动 Raft 解决接收到的消息；
- 调用 `Tick` 驱动 Raft 的计时；
- 调用 `Propose` 使 Leader 接收到来的 Entry，并存放至 RaftLog 向其他成员同步；
- 通过 `RaftReady` 封装需要上层应用处理的数据，包括更新的 HardState 和 SoftState、需要 Applied 的日志、Unstabled 日志、快照

![TinyKV_Raft.drawio](/Users/li/Desktop/draw.io/TinyKV_Raft.drawio.png)

PartA 包含三部分：Leader Election、Log Replication、RawNode Interface，基本按照 Raft Paper 上面实现就可以了，注意任何时候只要收到消息的任期大于本节点的任期，本节点就需要变为 follower

**Leader Election**

1. 选举的时候需要保证一个节点在一次选举期间只能投票一次，但是在选出了 leader 之后可以通过 HeartBeat 和 AppendEntry 重置 Vote，以便下次选举遇到任期相同的 Candidate 可以给它投票
2. 可以投票给 Candidate 的条件是：1⃣️ 任期大于自己并且日志足够新；2⃣️ 任期和自己相等，并且自己没有投过票或者已经投票给 Candidate 了，并且日志足够新。日志足够新的定义是最后一条日志的任期大于等于本节点最后一条日志的任期，或者如果想等的话索引要大于等于本节点的索引

**Log Replication**

上层应用在收到客户端提交的请求的时候会生成一个 Entry，通过 RawNode 封装的 Propose 接口发送消息给 Raft Leader，Leader 需要将新日志添加到 RaftLog 并广播给所有的节点

还需要注意的是，不同于 6.824，在 6.824 中由于 AppendEntry 包含了心跳和日志同步因此 leader 会时刻和 follower 同步日志。但是在 TinyKV 里面周期性发送的 HeartBeat 并不包含日志，因此一旦一个被网络隔离的节点恢复通信的时候我们需要及时的向它同步日志，此时需要在 heartbeat response 中检查节点的日志是不是和自己一样新了，如果没有的话也需要生成 AppendEntry Message

1. 根据 leader 保存的每个节点的 Next 数据生成需要同步的日志
2. 节点收到 AppendEntry Request 的时候
   - 首先需要检查 prevLogIndex 是否冲突，这里冲突的定义是：follower 的日志中不存在该索引值的日志，或者存在但是任期不同。当 prevLogIndex 有冲突的时候找到 follower 日志中第一条 Term 等于「prevLogIndex 这条日志 Term」的日志索引，用来提示 leader 加快冲突处理，并 Reject 这条日志同步消息
   - 如果 prevLogIndex 没有冲突的话，找到 new log entry 第一次出现冲突的位置，截断之后的数据，然后追加新的数据。注意如果已经持久化的日志出现了冲突，需要将 stabled 往前移动
3. leader 收到冲突的时候需要继续对该节点发送同步消息，直到同步成功为止。当大部分节点同步了这条日志，并且这条日志的任期是 leader 的任期，此时就更新 commitIndex，后续 RawNode 在生成 Ready 数据的时候会向上层提交

**RawNode Interface**

RawNode 中需要保存 SoftState 和 HardState，这里保存的都是 prev 的数据，用来检查是否有更新。上层应用会通过 hasReady() 判断是否有新的数据更新，如果有的话会使用 Ready() 接口封装这些数据，新数据更新包含下面这些：

1. SoftState 有更新
2. HardState 有更新，TinyKV 里面 HardState 除了 Term、Vote、Log 之外还存储了 commitIndex。当然也可以不存储，这里应该是为了加快处理，在不存储的情况下 leader 会通过和 follower 的同步更新到 commitIndex
3. 有新的日志需要 Applied
4. 需要写入到 stabled storage 的日志有变化
5. 快照变化

Part A 的代码处于 Basic Raft 层面，保证一个 Raft Group 内的数据一致性

**其他**

关于 Vote 字段的更新时间，raft 需要保证在一个任期节点只会投票一次，可以在 `becomeFollower(term, leader)` 函数中判断 `term > currentTerm`，条件满足的时候重置 Vote

收到 `term > currentTerm` 的时候先执行 becomeFollower 重置 Vote，再判断 candidate 的日志是否足够新，满足条件的时候再对 candidate 进行投票

## Part B

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220502215721410.png" alt="image-20220502215721410" style="zoom:50%;" />

PartB 需要我们实现 RaftKV，其中需要阅读了解 raftstore, region, peer_storage, peer 的概念。简单来说，一个节点就是一个 raftstore，里面包含了多个 peer，这些 peer 共享 raftstore 的底层存储（实验中是 badgerDB）。每个 peer 都属于一个 region，region 就是一个 raft group，同一个 raftstore 中的 peer 不会出现属于同一个 region。每个 region 会负责一个范围的数据，例如 region1 处理 `[key1, key2)` 的数据，region2 处理 `[key2, max_key]` 的数据

**storeMeta**

RaftStore 上使用 `storeMeta` 维护节点的所有 meta data，几个重要的数据是：regionID 和 key 的关系（用来定位某个 key 是由那个 region 管理的），regionID -> Region 的映射（用来读取 Region 的元数据）

**启动 RaftStore**

RaftStore 在被启动后，会加载或创建 peer，并将 peers 注册到 route 中，route 中保存了 regionID->peer 的映射，当收到消息的时候通过 regionID 找到 peer 并交给 peer 处理。加载 peer 的时候如果底层有保存着 peer 的数据，此时会读取这些数据创建 peer，否则就会创建一个新的 peer。随后会启动一些列的 worker，我们可以将 raftstore 的工作看作是接受来自其他节点的 msg，然后根据 regionID 将其转发到指定的 peer 执行，以及发送 peer 需要发送给其他 raftstore 上的 msg

**PeerStorage**

PeerStorage 负责 raftstore 上某个 peer 的存储，peer_storage 中包含了两个 DB 实例：raftDB 和 kvDB。其中 raftDB 中保存了 stabled entries 以及 RaftLocalState（HardState + last stabled Log Index），kvDB 中保存了 RaftApplyState（last applied entry index + truncated log information）和 RegionLocalState

**关于 worker**

```go
func (rw *raftWorker) run(closeCh <-chan struct{}, wg *sync.WaitGroup) {
  defer wg.Done()
  var msgs []message.Msg
  for {
    msgs = msgs[:0]
    select {
    case <-closeCh:
      return
    case msg := <-rw.raftCh:
      msgs = append(msgs, msg)
    }
    pending := len(rw.raftCh)
    for i := 0; i < pending; i++ {
      msgs = append(msgs, <-rw.raftCh)
    }
    peerStateMap := make(map[uint64]*peerState)
    for _, msg := range msgs {
      peerState := rw.getPeerState(peerStateMap, msg.RegionID)
      if peerState == nil {
        continue
      }
      newPeerMsgHandler(peerState.peer, rw.ctx).HandleMsg(msg)
    }
    for _, peerState := range peerStateMap {
      newPeerMsgHandler(peerState.peer, rw.ctx).HandleRaftReady()
    }
  }
}
```

worker 的主要功能就是接受 msg，然后通过 HandleMsg 处理从 raftCh 接收到的消息，包括：

1. MsgTypeTick：执行 peer 内部 RawNode 的 Tick
2. MsgTypeRaftCmd：包装来自客户端的 requests
3. MsgTypeRaftMessage：处理来自其它 peers 发送过来的消息，最后会通过 RawNode 的 Step 方法处理

之后通过 HandleRaftReady 处理每个 peer 的 Ready 消息，包括：写入 raft persist data 到底层存储（HardState、Log Entries、Snapshot），执行 committed entries 中的 command，发送 raft msgs 给别的节点

### 实验思路

框架很复杂，但是最终实现只落在三个函数上

`1. SaveReadyState()`：写入 raft persist data 到底层存储（HardState、Log Entries、Snapshot）。几个**注意事项**

1. 写入的时候需要使用 WriteBatch 一次性原子的写入；
2. 由于已经持久化的日志可能会因为冲突而被 leader 覆盖掉，对于这部分数据也需要在存储引擎中删除。例如 `......stabled -> ......conflict......currLastIndex......stabled`，对于 [conflict, stabled] 中的日志本应该全都删除，但是 [conflict, currLastIndex] 中的数据只需要修改就可以了

`2. proposeRaftCommand()`：用来向 Raft 模块 propose 客户端请求(RaftCmdRequest)，并创建 proposal 保存 request 执行结果。每个 proposal 通过 entryIndex 和 entryTerm 唯一确定，创建的时候需要获取 RaftLog 的 lastIndex 并将 lastIndex+1 作为 proposal 的 index。必须要先创建 proposal 再向 Raft propose，因为这里同一个节点上的执行是串行的，如果先执行 propose 则会导致 proposal 中的 Index 与 Log Entry 对应不上（proposal 会大一个 index）

`3. HandleRaftReady`：worker 在执行完 HandleMsg 之后 peer raft 的状态会有变化，此时 worker 会对每个 peer 执行 HandleRaftReady 处理 Ready 消息。首先需要 `SaveReadyState` 保存 persist data，然后发送给别的节点的 raft msgs。最后最关键的是处理 committed entries

leader 每次会收集多个 requests 封装在 **RaftCmdRequest** 中，因此在处理 committed entry 的时候需要遍历其中的每一个 request。处理完所有 requests 之后，我们需要找到该 entry 对应的 proposal，并执行 proposal 的 Done 函数保存执行结果，**proposal 与 committed entry 的关系**会有三种情况：

**StaleProposal**

日志被覆盖的时候会出现，例如 Peer1 在 Term1 的时候成功复制了 1 号日志，然后在 Term1 又接受了来自客户端的 (2, 1) 和 (3, 1)，但是并没有 commit 的时候就掉线了，此时集群的日志和 proposal 状态如下：

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220503094432314.png" alt="image-20220503094432314" style="zoom:50%;" />

此后 Peer2 成为了 Term2 的 Leader（节点 3 给它投票），然后 Peer2 接受了请求 (2, 2) 和 (3, 2) 并且成功 commit，并且 Peer1 重启之后日志被覆盖，此时集群的日志和 proposal 状态如下：

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220503094601615.png" alt="image-20220503094601615" style="zoom:50%;" />

当 Peer1 处理 Ready 消息中的 commit entry 的时候，会导致 LogIndex2 和 LogIndex3 的 Term 是对应不上的，这个时候需要回复 StableCommand

**CorrectProposal**

日志和 proposal 完全一致

**FurtherProposal**

如果上述中 Peer2 在复制了日志给 Peer1 之后掉线了，并且此时 Peer1 还没有 apply (2, 2) 和 (3, 2)，然后 Peer1 成为了新的 Leader，接受了来自客户端的请求 (4, 3)，并且复制给了 Peer3，然后开始 apply Index2-4 的日志。此时集群的日志和 proposal 的状态如下：

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220503095125225.png" alt="image-20220503095125225" style="zoom:50%;" />

在 apply Index2 和 Index3 的时候，P6 就是 FutherProposal

**Proposal 的处理**

- 遇到 StaleProposal 的时候需要回复 Stale，否则的话会导致 request timeout，并删除这条 proposal
- 遇到 CorrectProposal 的时候回复完毕之后，需要删除 proposal
- 遇到 FurtherProposal 的时候直接返回

处理的时候我们需要一次处理一批 Proposal 直到遇到正确的 Proposal 或者遇到 FurtherProposal。最后执行完所有的 committed entry 之后需要更新 RaftApplyState，并通过 Advance 推进 RawNode

## Part C

PartC 基于 PartA 和 PartB 实现快照的生成、发送以及安装，整个 raft_storage 层的处理逻辑如下

<img src="/Users/li/Desktop/draw.io/project2b.drawio.png" alt="project2b.drawio" style="zoom:4.5%;" />

**快照生成**

在 GCTick 的时候如果判断当前 applied entries 的数量相较于上一次已经达到了 GCLogLimit，此时会发送一个 AdminRequest，peer 会将这个 request 提交到 raft，等待 raft 同步、提交

当 raft 提交的时候，通过 ready 获取到这条日志并应用它，需要修改 TruncateState 并发送一个日志截断任务给 raftlog-gc worker，worker 会在后台异步的将日志删除

**快照的发送**

这里需要搞清楚什么时候会发送快照，简单来说就是当 leader 发现 follower 返回给自己的冲突日志并没有在内存中，这时候这条日志就存在于快照中，所以需要将快照发送给 follower 进行同步

通过 `peer_storage.Snapshot()` 会给 region_worker 发出产生快照的任务，产生快照的任务是异步执行的，会立即返回一个 ErrSnapshotTemporarilyUnavailable 错误表示正在生成快照。等待下一次 leader 在心跳的时候发现 follower 的日志不同步，就会再次尝试产生并发送快照，这个时候快照就有可能准备好了，然后会发送给 follower

TinyKV 中 region_worker 在生成快照的时候会一并将 leader 当前最近的日志也打包进去，举个例子，leader 的快照是到 1000，但是 leader 当前 commit 已经到了 1010，这个时候产生快照的时候会生成一个 1010 的快照

**快照的安装**

follower 在收到快照之后需要判断快照的最后一条日志是否大于自己的 committedIndex，因为有可能会由于网络原因收到过期的快照，这个时候应该避免安装快照

否则的话需要更新自己当前的 Raftlog.committed, stabled, applied 都设置为快照的最后一条日志，dummyIndex 设置为最后一条日志的下一条日志。TinyKV 的快照中还包含了 region 的配置信息，因此还需要更新 region 配置

这个时候实际上快照都还没有安装到底层的 DB，我们需要将快照存放到 pendingSnapshot 中，等待状态机执行 hasReady 的时候发现快照，并交给 region_worker 安装快照，到这里为止快照才是真正的被 install 到 DB

[TinyKV Project2C 关于 Snapshot 调用流程的问题 - 学习与认证 - TiDB 的问答社区 (asktug.com)](https://asktug.com/t/topic/273859)

## 错误记录

1. leader 在处理 AppendEntry Response 的时候，尝试给 follower 发送新的 AppendEntry Request，但是 follower 回复的 Index 超出了 leader 的最后一条日志，导致 **panic: runtime error: index out of range [103] with length 63**

原因分析：查看日志发现这个时候出现了两个 leader(4, 5)，并且它们的任期都是 34，leader(5) 和 follower(2) 同步了自己的最近日志(2554)，然后 leader(4) 尝试发送给 follower(2) 日志(2510)，由于出现日志冲突，follower(2) 回复 leader(4) 自己的最后一条日志(2554)。但是 leader(4) 并没有这条日志

关于为什么可以直接在内存中的 entries 中找到第一条 conflictTerm 的日志返回，原因是 snapshot 中的日志必定是已经被 commit 的，而对于 commit 的日志是不可能也不应该出现冲突的

解决方法：

2. client 查询结果总是缺少一条日志

原因分析：如果来自同一个 client 的 Put 和 Get/Snap 请求被 leader 一起处理，原先的逻辑是一起处理的这批 commitEntries 全都被放到 WriteBatch 之后才执行 MustWriteToDB。但是 proposal 是在 MustWriteToDB 之前被唤醒的，这就导致了 Get/Snap 请求不能及时看到 Put 的结果

解决方法：遇到 Get/Snap 请求的时候，先执行一次 MustWriteToDB，然后新建一个 WriteBatch 赋值给 kvWB

3. want:x 0 0 yx 0 1 yx 0 2 y; got: :x 0 0 yx 0 1 yx 0 2 yx 0 2 y

原因分析：在 apply committed entries 的时候对 kvWB 所做的修改都是修改了 `handleRaftReady` 里面 kvWB 这块内存（原先的时候这个 kvWB 不是指针）。然后在遇到 Snap/Get 消息的时候会执行一次 kvWB 的写入，但是这部分写入仅仅是清空了 `processRequest::kvWB`，而对于 `handleRaftReady::kvWB` 依旧存有这些写入的内容，导致在 `handleRaftReady` 里面会再次执行这些写入

解决方法：将 `handleRaftReady::kvWB` 修改为指针，每次 `process` 的时候都返回 `process...::kvWB` 重新对 `handleRaftReady::kvWB` 进行赋值，这样就可以避免重复的写入以及 `processRequest::kvWB` 后续的新修改

4. too many open files

原因分析：shell 限制了文件打开的数量，这个问题困扰了我很久很久，后面才发现是 zsh 的限制，怀疑过内核的限制和 launchd 对进程的限制，就是没有想到 shell 的限制

解决方法：`ulimit -S -n 4096` 修改 shell 打开文件上限

[macOS 解决 too many open files – 孑枵 Abreto](https://blog.abreto.net/archives/2020/02/macos-too-many-open-files.html)

5. slice bounds out of range [12:11]

前面的问题都解决之后才出现的，因此大概率不是 RaftLog 代码设计的问题。检查日志发现，follower 在收到 leader 的 CompactLogRequest 的时候出现 `CompactIndex = 573, curr_truncatedIndex = 574` 的情况，这个时候无条件的截断了 574 这条日志，导致：

- RaftLog.dummyIndex 从 575 变为 574
- 内存中的日志并没有改变，依旧是 [575, 585]
- Raft.stabled 也没有改变，依旧是 585

然后执行了 `unstabledEntries` 尝试获取所有未提交的日志，由于 `dummyIndex = 574`，所以计算出来的最后一条日志是 `dummyIndex - 1 + len(entries) = 574 - 1 + 11 = 584`，因此这个时候会获取日志数组的下标范围是 `[12:11]`，而如果 dummyIndex = 585 的时候是 `[12:12]`。所以就出现了 slice bounds out of range [12:11]

为什么会出现 `CompactIndex < curr_truncatedIndex`：假如某个时刻 leader 快照的最后一条日志是 300，而这个时候 raft 中的最后一条日志是 574，这个时候如果 follower 与 leader 出现日志冲突，leader 会给 follower 发送最后一条日志是 574 的快照，所以 follower 在安装快照之后 truncatedIndex 就变为了 575；然后紧接着，leader 收到 admin request 需要创建快照，快照的最后一条日志是 573，这个时候同步到 follower 并且 follower 提交之后就出现了 CompactIndex < truncatedIndex 的情况

解决方法：增加判断只有 `CompactIndex > curr_truncatedIndex` 的时候才可以执行快照压缩

err：[Pingcap TinyKV 2022 lab2思路及文档 - 知乎 (zhihu.com)](https://zhuanlan.zhihu.com/p/489197367)

## Raft 扩展

### leader 为什么不能通过计算副本的方式提交之前任期的日志

可以参考下面这条链接，简单来说就是，如果允许 leader 通过计算副本的方式提交之前任期的日志，那么就会出现一条已经被同步到大多数节点并且被提交的日志被覆盖掉，这是违反协议的

而如果 leader 只能通过计算副本的方式提交当前任期的日志，就不会出现这种情况，因为这个时候别的日志不够新的节点不可能成为 leader，也就不会覆盖掉已经被提交的日志了

[为什么Raft协议不能提交之前任期的日志？ - codedump的网络日志](https://www.codedump.info/post/20211011-raft-propose-prev-term/)

[raft算法中，5.4.2节一疑问？ - 知乎 (zhihu.com)](https://www.zhihu.com/question/68287713)

### TiKV Raft 优化

1. leader 一次收集多个 requests发送给 follower
2. leader 发送了一批 log 之后可以直接更新 nextIndex，这样就可以不需要等待 follower 返回就继续发送后续的 log
3. 并行的执行将 log 发送给 followers 和 append，因为 appned 涉及到落盘有开销。即便 leader append 出现 panic 也不会影响，因为已经被 append 到大多数节点的日志后续会被新的 leader 提交
4. 异步 apply，已经被 commit 的日志一定会被 apply，可以在后台异步的执行 apply 操作

[TiKV 功能介绍 - Raft 的优化 | PingCAP](https://pingcap.com/zh/blog/optimizing-raft-in-tikv)