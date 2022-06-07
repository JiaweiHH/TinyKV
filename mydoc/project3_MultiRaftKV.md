# Project3 MultiRaftKV

## PartA

### 领导权禅让

这一部分比较简单，leader 在收到 transferLeader 请求的时候会判断 transferee 的日志是否是最新的。如果不是最新的，则 leader 会发送日志同步给 transferee。直到 transferee 的日志是最新的时候，leader 会发送 TimeoutNow 消息给 transferee，这个时候 transferee 会无视选举超时，直接发起选举，由于此时拥有最新的日志和更大的任期，因此 transferee 大概率会赢的选举

leader 在领导权禅让期间不能响应客户端的请求，避免由于客户端请求的到来，导致 leader 和 transferee 一直在同步日志

另外还需要注意的是，论文中提到如果在选举超时时间内还没有完成领导权禅让，则 leader 需要终止并恢复接收客户端请求

#### 实验细节

1. becomeFollower 的时候重置 transferee
2. leader tick 的时候如果处于领导权禅让过程，需要更新 transferElapsed，在超过选举超时时间的时候结束领导权禅让
2. follower 或者 candidate 收到 transferLeader 消息需要转发给 leader
2. 集群外的节点收到消息需要忽略这条消息

### 集群成员变更

阅读 raft 博士论文可以了解到如果一次直接(condition 1)变更多个节点(condition 2)就会破坏 raft 的安全性。由于不同节点切换到新配置的时间不同，可能会导致两个不同的 leader 可以在同一个任期内被选举出来，一个拥有旧配置，另一个拥有新配置

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220510160716699.png" alt="image-20220510160716699" style="zoom: 33%;" />

例如上图中，在某个时间节点新配置包含节点 {3, 4, 5}，旧配置包含节点 {1, 2}。如果 server-1 短暂的断开连接然后发起投票（恢复连接），然后 server-2 判断其日志和自己一样新、并且任期大于自己，所以 server-1 获得了 server-2 的投票，在 server-1 看来这个时候已经获得了大多数节点的认可，因此 server-1 转变为 leader。同样的如果在同一时刻，server-5 也短暂的断开连接并发起投票，它会获得 {3, 4} 的投票，这个时候 server-5 也会变为 leader

这时候就出现了在同一个任期内选举出了两个 leader，破坏了 raft 的安全性。下面两种算法可以解决脑裂问题

#### 单步变更算法

出现上图问题的其中一个原因是由于一次变更多个节点，导致出现了 “新配置” 和 “旧配置” 的 quorum 不相交。所以一个简单的出发点就是 **禁止会导致多数成员不相交的成员更改**，对应的算法就是单步变更算法：一次只能从集群中添加或者删除一个服务器

单步变更算法可以保证，新旧配置的 quorum 必定是有重叠的、且重叠区域大小是 1，这样的话一个处于重叠范围的节点只能给 $C_{old}$ 或者 $C_{new}$ 的其中一个节点进行投票。而一旦重叠节点给其中一边投票了，那么处于另一边的节点必定不会成为 leader

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220510170621851.png" alt="image-20220510170621851" style="zoom: 33%;" />

#### Joint Consensus

joint consensus 算法可以一次执行多个成员变更，不同于一次变更一个配置，变更多个配置的时候新旧配置的 quorum 可能会出现不重叠。这时候引入了一个中间状态 $C_{old, new}$($C_{old} \cup C_{new}$)，集群会首先变更到过渡状态，处于中间状态的时候 quorum 中的节点需要包含 $C_{old}$ 中的大多数和 $C_{new}$ (已经引入的)中的大多数（$quorum_{old} \cup quorum_{new}$），这条规则保证了不会出现两个 leader。然后再变更到 $C_{new}$ 状态

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220510221431581.png" alt="image-20220510221431581" style="zoom: 50%;" />

上面这张图中展示了 Joint Consensus 算法过程中（例如 $C_{old} = \{1, 2, 3\}$，$C_{new} = \{2, 3, 4, 5\}$，$C_{old, new} = \{1, 2, 3, 4, 5\}$），集群可能会处于的状态：

1. $C_{old, new}$ 提交之前。这个时候所有的节点可能处于 $C_{old}$ 配置下，也可能处于 $C_{old, new}$ 配置下。此时如果 leader 宕机了，无论发起新一轮投票的节点是处于 $C_{old}$ 配置下的节点，还是处于 $C_{old, new}$ 配置下的节点，都需要 $C_{old}$ 集合中的大多数节点同意，因此不会出现两个 leader
2. $C_{old, new}$ 提交之后，$C_{new}$ 下发之前。此时集群中的大多数节点的配置都处于 $C_{old, new}$，由于处于 $C_{old}$ 配置的节点没有最新的日志，因此集群中的大多数节点不会给它投票，因此只有处于 $C_{old, new}$ 配置的节点会成为 leader，同样不会出现两个 leader
3. $C_{new}$ 下发之后，提交之前。此时集群中的节点有三种状态可能会处于三种状态：处于 $C_{old}$ 配置的节点、处于 $C_{old, new}$ 配置的节点，处于 $C_{new}$ 配置的节点。同样由于处于 $C_{old}$ 配置的节点没有最新的日志因此不会成为 leader，剩下的只有处于 $C_{old, new}$ 和 $C_{new}$ 的节点，无论是谁发起选举，都需要 $C_{new}$ 集合中的大多数同意，因此也不会出现两个 leader
4. $C_{new}$ 提交之后。这个时候集群处于 $C_{new}$ 配置下了，只有 $C_{new}$ 配置的节点可以成为 leader，因此不会出现两个 leader

[Raft 算法之集群成员变更 - SegmentFault 思否](https://segmentfault.com/a/1190000022796386)

#### 配置生效时间（以单步变更算法为例）

**(1) 论文中提到：只有当旧集群的大多数成员都在 $C_{new}$ 的规则下运行时，才可以安全地开始另一次成员更改**。在谈配置生效时间之前，我们先来看这条规则，假如 leader 不等待 prev_conf 生效就直接发送 next_conf，这个时候其实回到了上图中的场景。例如一个集群 {1, 2, 3}：

1. leader 1 发送 {addNode 4} 日志给 followers，然后 {2, 3} commit 了这条日志，但是还没有 apply
2. server-1 apply {addNode 4}，所以这个时候集群变为了 {1, 2, 3, 4}，但是 server-2 和 server-3 还不知道 server-4 的存在
3. 之后 server-1 发送 {addNode 5} 给所有的节点，并且 {1, 4} applied，此时集群变为了 {1, 2, 3, 4, 5}，但是 {1, 2} 依旧没有 apply，因此不知道 {4, 5} 的存在
4. 然后在某时刻 {2} 发起选举，并且 {3} 给 {2} 投票，在 {2} 看来已经获得了大多数节点的投票，因此 {2} 在这个任期成为 leader；同样在这个时刻 {5} 发起选举，并且 {1, 4, 5} 投票，所以 {5} 也变为了 leader
5. 上述过程就回到了上面图片中的场景，相当于单步变更算法失去了 <u>新旧配置 quorum 重叠</u> 的意义

**(2) 再来看配置变更在什么时候应该生效**。配置变更也是一条日志，普通日志在被状态机 apply 的时候会生效，如果服务器只在了解到 $C_{new}$ 已经提交时才采用 $C_{new}$，那么 leader 将很难知道旧集群中的大部分已经应用了它。在 raft paper 中提到，对于 `Conf_Change` 类型的日志，<u>在收到之后就可以生效</u>（虽然 leader 宕机之后这条日志可能会被截断，导致配置回退，但这不是问题，只要保证在同一个任期只能选出一个 leader 就可以了）

**(3) 不需要等待所有节点都应用了配置变更之后才开始下一次配置变更**。因为对于这些少数没有应用配置变更的旧集群中的节点，它们只能是因为没有收到日志同步，<u>由于日志不够新因此它们就不可能成为 leader</u>

[周刊（第13期）：重读Raft论文中的集群成员变更算法（一）：理论篇 (qq.com)](https://mp.weixin.qq.com/s/HGdJF_cN4yybmn3orERRPw)

#### 实验思路

论文中要求节点在收到 ConfChange 日志的时候就生效新的配置，但是 TinyKV 实际上是在 apply 的时候生效的，不过在 TinyKV 的框架下这样做并不会有问题，我们可以分析一下。假如集群初始节点是 {1, ,2, 3} 并且 server-1 是 leader，初始的 commitIndex = 0

1. server-1 发送 ConfChange{addNode 4} 日志给 {2, 3}，{2, 3} 同步之后 server-1 提交这条日志(commitIndex = 1)，此时 {2, 3} 还没有收到下一次 server-1 的 AppendEntry Request，因此 commitIndex 没有更新
2. server-1 apply，然后配置生效，这个时候在 server-1 看来集群的配置是 {1, 2, 3, 4}

然后我们知道配置变更的过程中出现两个 leader 的本质原因是一次性增加了多个节点，并且没有限制 quorum。也就是如果想要出现两个 leader，则需要 {2, 3} 看来集群只有 {1, 2, 3}，在 {1, 4, 5} 看来集群有 {1, 2, 3, 4, 5}。下面接着分析，可以发现这种情况是不可能的

3. 接下来 server-1 开始发送 ConfChange{addNode 5} 日志给 {2, 3, 4}
4. {2, 3} 一旦收到了 ConfChange{addNode 5}，就会更新 commitIndex = 1
5. {2, 3, 4} 返回日志同步结果给 leader，leader 更新 commitIndex = 2，并且 apply，在 server-1 看来这个时候集群变为了 {1, 2, 3, 4, 5}
6. 同样的 server-4 也 apply 这条日志，这个时候在 {1, 4, 5} 看来集群变为 {1, 2, 3, 4, 5}

如果想要发生最初提到的两个 leader 的情况，需要 {2, 3} 中发起选举的那个节点看来它的配置和最新的配置的 quorum 没有交集。然而假如说 server-2 需要发起选举，这个时候会在 `handleRafyReady` 的时候将 RequestVote message 发送出去，在发送这个消息的时候也会将 commitIndex = 1 这条日志 apply，而日志一旦 apply，server-2 的配置就会变为 {1, 2, 3, 4}，此时它的 quorum 必定会和 {1, 4, 5} 的配置（即 {1, 2, 3, 4, 5}）的 quorum，所以重叠的这些节点保证了不会选出两个 leader

**注意点**

1. raft 重启初始化的时候需要对 PendingConfIndex 进行初始化
2. leader 执行 Propose，如果新日志是 ConfChange 需要更新 PendingConfIndex
3. candidate 成为 leader 的时候也需要检查并更新 PendingConfIndex

如果在 raft 重启的时候不更新 PendingConfIndex，假如 leader 在关闭之间还有一个没有同步到任何节点的 ConfChange{addNode 4}。这个时候重启之后如果又添加了新的 ConfChange{addNode 5}，然后 leader 会一次性的向 follower 同步 {addNode4, addNode5}，这时候就破坏了单步变更算法，此时新旧配置就会出现 quorum 不重叠的现象

candidate 成为 leader 如果不更新也是同样的道理，会导致新的 leader 同时将 {addNode4, addNode 5} 同步给 followers，造成 quorum 不重叠

**new peer 的创建**

leader 会根据 peercache 中保存的 new peer 的 storeId 发送 heartbeat 给这个 peer。然后 raft_storage 收到消息的时候会尝试通过 router 转发这条消息到本节点上的 peer，但是由于 new peer 一开始不存在于这个物理节点，这个时候就会创建一个新的 peer

storage 层面的消息是由 store worker 处理的

[TinyKV | lab3+lab4 - 知乎 (zhihu.com)](https://zhuanlan.zhihu.com/p/466627300)

#### 单步变更算法存在的问题

### 成员变更参考资料

[raft-thesis-zh_cn/raft-thesis-zh_cn.md at master · OneSizeFitsQuorum/raft-thesis-zh_cn (github.com)](https://github.com/OneSizeFitsQuorum/raft-thesis-zh_cn/blob/master/raft-thesis-zh_cn.md#4-集群成员更改)

[周刊（第13期）：重读Raft论文中的集群成员变更算法（一）：理论篇 (qq.com)](https://mp.weixin.qq.com/s/HGdJF_cN4yybmn3orERRPw)

[Raft 算法之集群成员变更 - SegmentFault 思否](https://segmentfault.com/a/1190000022796386)

[分布式-Raft算法(三)如何解决成员变更问题 - HonorJoey的博客 | HonorJoey Blog](https://honorjoey.top/2020/07/04/分布式-Raft算法(三)-如何解决成员变更问题/)

下面两个难度较大

[TiDB 在 Raft 成员变更上踩的坑 - 知乎 (zhihu.com)](https://zhuanlan.zhihu.com/p/342319702)

[TiDB 在 Raft 成员变更上踩的坑 - OpenACID Blog](https://blog.openacid.com/distributed/raft-bug/)

### 错误记录

1. [error] [region 1] 1 handle raft message error raft: cannot step as peer not found

最后发现原因是因为，通过 removeNode 的 peerId 在 peer_storage::region::Peers 查找的时候，使用的是数组下标来匹配 peerId，正确的方法应该是使用 d.peerStorage.region.Peers[i].Id 来匹配

解决方法：改为通过 peer 对象的编号匹配

2. panic: [region 1] 2 unexpected raft log index: lastIndex 0 < appliedIndex 8

错误原因：在 storage{2} 删除 region{2} 的时候会清除 peer_storage DB 上的所有数据。然后发现当 processCimmittedEntry 结束的时候，继续顺着执行流程更新了 applyState，因此导致了 raftState 的数据为空，但是 applyState 的数据不为空

解决方法：在 processCommittedEntry 处理完一条 committedEntry 之后，判断是否 peer 是否停止了，如果 peer 停止了，则直接从 handleRaftReady 中结束

3. panic: get value 7631 for key 6b31

说明：首先删除 {2, 3, 4, 5}；然后 {1} 处理新的请求；然后重新将 {2} 添加回来，集群继续处理新的请求；然后添加 {3}，然后删除 {2}。这个时候需要 {2} 中的数据必须是空的，但是 bug 指出查询 {2} 中的数据的时候并不是空的

调试之后发现，原因在于当 {2} 提交了第二次删除 {2} 的 entry 的时候，因为 RegionEpoch 不是最新的所以忽略了这次的请求

解决方法：在 {1}->{2} 发送 snapshot 并处理完成之后，更新 {2} 的 region 信息

**有小概率会出现只有两个 peer 的时候，leader 执行了 remove 自己的操作，并且当 leader remove 了之后并没有将 commitIndex 同步给 follower。这个时候 follower 无法及时更新 region 信息，导致发起选举也无法成为 leader，最后测试用例 request tomeout。解决方法是当 leader 准备 remove 自己的时候，在 propose 前执行 transferLeader（目前并没有实现，有时间可以改进）**

## PartB

触发 Split 的流程：

1. `peer_msg_handler.go` 中的 `onTick()` 定时检查，调用 `onSplitRegionCheckTick()` 函数，生成一个 `SplitCheckTask` 任务发送到 split_checker
2. 检查时如果发现满足 split 要求，则生成一个 `MsgTypeSplitRegion` 请求（包含了 SplitKey），通过  RaftRouter 发送到指定的 region 上
3. `HandleMsg` 方法中调用 `onPrepareSplitRegion()`，发送 `SchedulerAskSplitTask` 请求到 scheduler，为新分配的 Region 申请新的 region id 和 peer id。申请成功之后，会发起一个 `AdminCmdType_Split` 的 AdminRequest 到 Region 中
4. 之后就和接收普通 AdminRequest 一样，propose 等待 apply

Region 分裂之后，两个 Region 的数据都还是在同一个 store 上，不需要数据迁移。可能会觉得这样的分裂有什么意义，这样的好处在于可以使用两个 RaftGroup 进行管理，提升访问性能

执行 Split 过程：

1. 检查 regionId 是否相同、检查 regionEpoch 是否是过期的 split 请求、检查 Key 是否在 Region 中
2. 更新 oldRegion 的 Version
3. 创建新的 region，newRegion 的 startKey 设置为 splitKey，endKey 设置为 oldRegion 的 endKey
4. 修改 storeMeta 的 regionRanges 和 regions 信息
5. 持久化 oldRegion 和 newRegion
6. 在当前 store 上创建 newRegion 的 peer，注册到 router 并启动
7. 处理 proposal
8. 发送 heartbeat 给 scheduler

### 错误记录

1. proposal.cb == nil

测试程序在 propose 的时候传入的 cb 就是 nil，导致后面再设置 cb.Txn 的时候出现错误

解决方法：只有当 cb 不是 nil 的时候才对其进行设置 Txn

2. assert.NotNil(t, resp.GetHeader().GetError()) 错误

Region 分裂之后向 oldRegion 请求某个存在于 newRegion 中的数据，本应该得到 KeyNotInRegion 的 Error，但是现在依旧找到了这个 Key

原因是，Region 分裂之后 oldRegion 指示更改了元数据，底层数据并没有修改，因为 newRegion 和 oldRegion 属于同一个 store

解决方法：对于 Get/Put/Delete 请求来说，首先判断 Key 是否在对应的 Region 中

3. key [49 32 48 48 48 48 48 48 48 48] is not in region id:24 start_key:"0 00000011" end_key:"0 00000022" region_epoch:<conf_ver:1 version:2 > peers:<id:25 store_id:1 > peers:<id:26 store_id:2 > peers:<id:27 store_id:3 > peers:<id:28 store_id:4 > peers:<id:29 store_id:5 >

找到问题是因为 Snap Request 的时候没有判断 request 中的 Version 是不是最新的 Version。因为请求可能是在 Region 变化之前就已经发出来了，然后如果 Region 变化之后，依旧返回这个 Region 的话就会导致 Snap Response 之后找不到 Region 中的数据

4. panic: start key > end key

测试用例会出现通过了 CheckRegionEpoch 之后 splitKey 不在 region 中的情况，尚不清楚这是如何发生的

解决方法是，process Split 的时候，检查了 Epoch 之后再检查 splitKey

5. panic: key [57 32 48 48 48 48 48 48 48 48] is not in region id:12 start_key:"1 00000005" end_key:"1 00000016" region_epoch:<conf_ver:1 version:2 > peers:<id:13 store_id:1 > peers:<id:14 store_id:2 > peers:<id:15 store_id:3 > peers:<id:16 store_id:4 > peers:<id:17 store_id:5 >

原因在于 requests.Header.RegionId != d.regionId，增加了这个错误判断之后不会出现这个问题。

**最后依旧有概率会出现这个问题，以及一个新的问题（“多读的问题”），放着先不管了**

![image-20220522143455717](/Users/li/Library/Application Support/typora-user-images/image-20220522143455717.png)

## Part C

这一部分实现 Scheduler 的逻辑，具体就只有两个函数 `processRegionHeartbeat` 和 `Schedule`。Scheduler 会根据 heartbeat 更新 local region information，然后检查针对 region 是否有 command 需要处理，如果有的话就会去处理

这部分比较简单，代码量也比较少

**processRegionHeartbeat**

`processRegionHeartbeat` 用来更新 local region information。需要检查 RegionEpoch 是否是最新的

1. 检查是否有两个 region 的 regionId 一样，对于一样的 id，Version 或者 ConVer 较小的是过期的 region
2. 如果没有两个 region 的 id 一样，那么检查是否有重叠的 region，对于有重叠的 region，Version 或者 ConVer 较小的 region 是过期的 region

如果当前 heartbeat 中的 region 是最新的 region，调用 `c.putRegion` 更新 region tree，调用 `c.updateStoreStatusLocked` 更新 store status

**Schedule**

TinyKV 集群中存在多个 scheduler，每个 scheduler 实现了 Scheduler 的接口，例如 banlance-region scheduler, balance-leader scheduler 等。scheduler 会周期性的执行 `Schedule` 函数，并根据该函数的返回结果执行特性的操作。本实验只需要关注 balance-region scheduler 的 `Schedule` 函数实现，balance-region scheduler 会检查 store 的平衡性，如果出现了不平衡会选择一个 region 进行移动（例如某个 store 上面包含了太多的 region，则认为这个 store 出现了不平衡性）

`Schedule` 函数的处理逻辑如下：

1. 选出 suitableStores，并按照 region size 排序。suitableStores 是指那些满足 `DownTime` 小于 `MaxStoreDownTime` 的 store
2. 从 region size 最大的 suitableStores 开始遍历，依次调用 `GetPendingRegionsWithLock()`、`GetFollowersWithLock()`、`GetLeadersWithLock()` 获取 store 上不同类型的 regions。如果找不到 regions 该函数直接返回 nil
3. 判断目标 region 的 store 数量，如果小于 `cluster.GetMaxReplicas()` 直接返回 nil
4. 从小到大遍历 suitableStores，找到一个满足目标 region 不在的 store
5. 检查 fromStore 和 toStore 的 region size 差值是否小于两倍的目标 region size，如果是的话直接放弃操作，因为当差值大雨两倍的 region size 的时候，移动了 region 之后还能够保证 fromStore 的大小大于 toStore 的大小，避免重复移动
6. 创建 createMovePeerOperator 操作并返回
