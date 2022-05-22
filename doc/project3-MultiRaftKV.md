# Project3 MultiRaftKV

In project2, you have built a high available kv server based on Raft, good work! But not enough, such kv server is backed by a single raft group which is not unlimited scalable, and every write request will wait until committed and then write to badger one by one, which is a key requirement to ensure consistency, but also kill any concurrency.

![multiraft](imgs/multiraft.png)

In this project you will implement a multi raft-based kv server with balance scheduler, which consist of multiple raft groups, each raft group is responsible for a single key range which is named region here, the layout will be looked like the above diagram. Requests to a single region are handled just like before, yet multiple regions can handle requests concurrently which improves performance but also bring some new challenges like balancing the request to each region, etc.

This project has 3 part, including:

1. Implement membership change and leadership change to Raft algorithm
2. Implement conf change and region split on raftstore
3. Introduce scheduler

在本节中，您将使用 balance scheduler 实现一个 multi raft-based kv server，该服务器由多个 raft group 组成，每个 raft group 负责一个单独的 key 范围，这个范围在这里命名为 region。对单个区域的请求和以前一样，但是多个区域可以同时处理请求，这提高了性能，但也带来了一些新的挑战，如平衡对每个区域的请求等

该实验由三部分组成，包括：

1. 实现 raft 集群成员变更和领导层变更
2. 在 raftstore 上实现配置更改和 region split
3. 实现调度器

## Part A

In this part you will implement membership change and leadership change to the basic raft algorithm, these features are required by the next two parts. Membership change, namely conf change, is used to add or remove peers to the raft group, which can change the quorum of the raft group, so be careful. Leadership change, namely leader transfer, is used to transfer the leadership to another peer, which is very useful for balance.

在本部分中，您将对基本 raft 算法实现成员变更和领导变更，这些功能是接下来两部分所需的。成员变更 (membership change) 即 conf change，用于向 raft group 添加或者删除 peers，这可能会更改 raft group 的 quorum，因此请小心。领导层变更 (leadership change) 即 leader transfer，用于将 leader 转移到另一个 peer，这对 balance 来说非常有用

### The Code

The code you need to modify is all about `raft/raft.go` and `raft/rawnode.go`, also see `proto/proto/eraft.proto` for new messages you need to handle. And both conf change and leader transfer are triggered by the upper application, so you may want to start at `raft/rawnode.go`.

需要修改的文件：raft/raft.go, raft/rawnode.go。conf change 和 leader transfer 都由上层应用触发，因此你可以从 raft/rawnode.go 开始

### Implement leader transfer

To implement leader transfer, let’s introduce two new message types: `MsgTransferLeader` and `MsgTimeoutNow`. To transfer leadership you need to first call `raft.Raft.Step` with `MsgTransferLeader` message on the current leader, and to ensure the success of the transfer, the current leader should first check the qualification of the transferee (namely transfer target) like: is the transferee’s log up to date, etc. If the transferee is not qualified, the current leader can choose to abort the transfer or help the transferee, since abort is not helping, let’s choose to help the transferee. If the transferee’s log is not up to date, the current leader should send a `MsgAppend` message to the transferee and stop accepting new proposals in case we end up cycling. So if the transferee is qualified (or after the current leader’s help), the leader should send a `MsgTimeoutNow` message to the transferee immediately, and after receiving a `MsgTimeoutNow` message the transferee should start a new election immediately regardless of its election timeout, with a higher term and up to date log, the transferee has great chance to step down the current leader and become the new leader.

为了实现领导权禅让，leader 首先需要执行 `raft.Raft.Step(MsgTransferLeader)`。leader 需要检查 transferee 的资格，例如日志是否是最新的等。如果 transferee 的资格不合格，leader 应该帮助它，向其发送 `MsgAppend` 消息同步日志，此时 leader 需要停止接收新的 proposal（避免无限循环）。如果 transferee 符合条件（有可能是在 leader 的帮助下达到的条件），leader 应该立即发送一条 `MsgTimeoutNow` 消息，transferee 在收到消息之后可以无视 election timeout 直接发起选举，由于它有最新的日志和更高的任期 transferee 有很大的概率会成为新的 leader

### Implement conf change

Conf change algorithm you will implement here is not the joint consensus algorithm mentioned in the extended Raft paper that can add and/or remove arbitrary peers at once, instead, it can only add or remove peers one by one, which is more simple and easy to reason about. Moreover, conf change start at calling leader’s `raft.RawNode.ProposeConfChange` which will propose an entry with `pb.Entry.EntryType` set to `EntryConfChange` and `pb.Entry.Data` set to the input `pb.ConfChange`. When entries with type `EntryConfChange` are committed, you must apply it through `RawNode.ApplyConfChange` with the `pb.ConfChange` in the entry, only then you can add or remove peer to this raft node through `raft.Raft.addNode` and `raft.Raft.removeNode` according to the `pb.ConfChange`.

这里实现的 conf change 算法不是 raft 论文中提到的 joint consensus 算法，那个算法可以一次添加或删除任意数量的 peers，在这里只能一次添加或者删除一个 peer，这更加简单、也更容易推理。conf change 从调用 leader 的 `raft.RawNode.ProposeConChange` 开始，它会 propose 一个新的 entry，`EntryType = EntryConfChange`，`Data = pb.ConfChange`。当这条 entry 被提交的时候，你必须通过 `RawNode.ApplyConfChange` apply 这条 entry，只有这样才能根据 `pb.ConfChange` 通过 `addNode` 或者 `removeNode` 添加或者删除 peer

> Hints:
>
> - `MsgTransferLeader` message is local message that not come from network
> - You set the `Message.from` of the `MsgTransferLeader` message to the transferee (namely transfer target)
> - To start a new election immediately you can call `Raft.Step` with `MsgHup` message
> - Call `pb.ConfChange.Marshal` to get bytes represent of `pb.ConfChange` and put it to `pb.Entry.Data`
>
> 提示：
>
> - `MsgTransferLeader` 是一条 local message，并不是来自 network
> - `MsgTransferLeader` 消息的 from 字段设置为 transferee
> - 可以通过 `Raft.Step` 的 MsgHup 立即开始一次新的选举
> - `pb.ConfChange.Marshal` 可以获取 `pb.ConfChange` 的字节数据，并存放到 `pb.Entry.Data`

## Part B

As the Raft module supported membership change and leadership change now, in this part you need to make TinyKV support these admin commands based on part A. As you can see in `proto/proto/raft_cmdpb.proto`, there are four types of admin commands:

- CompactLog (Already implemented in project 2 part C)
- TransferLeader
- ChangePeer
- Split

`TransferLeader` and `ChangePeer` are the commands based on the Raft support of leadership change and membership change. These will be used as the basic operator steps for the balance scheduler. `Split` splits one Region into two Regions, that’s the base for multi raft. You will implement them step by step.

### The Code

All the changes are based on the implementation of the project2, so the code you need to modify is all about `kv/raftstore/peer_msg_handler.go` and `kv/raftstore/peer.go`.

### Propose transfer leader

This step is quite simple. As a raft command, `TransferLeader` will be proposed as a Raft entry. But `TransferLeader` actually is an action with no need to replicate to other peers, so you just need to call the `TransferLeader()` method of `RawNode` instead of `Propose()` for `TransferLeader` command.

### Implement conf change in raftstore

The conf change has two different types, `AddNode` and `RemoveNode`. Just as its name implies, it adds a Peer or removes a Peer from the Region. To implement conf change, you should learn the terminology of `RegionEpoch` first. `RegionEpoch` is a part of the meta-information of `metapb.Region`. When a Region adds or removes Peer or splits, the Region’s epoch has changed. RegionEpoch’s `conf_ver` increases during ConfChange while `version` increases during a split. It will be used to guarantee the latest region information under network isolation that two leaders in one Region.

You need to make raftstore support handling conf change commands. The process would be:

1. Propose conf change admin command by `ProposeConfChange`
2. After the log is committed, change the `RegionLocalState`, including `RegionEpoch` and `Peers` in `Region`
3. Call `ApplyConfChange()` of `raft.RawNode`

为了实现 conf change，首先需要了解 `RegionEpoch`。`RegionEpoch` 是 `metapb.Region` 的一部分，当 Region 添加或者删除 Peer 或者分裂时，`RegionEpoch` 会变化。`conf_ver` 会在 ConfChange 期间增加，`version` 会在 split 期间增加。它将用于保证在网络隔离的情况下，一个 Region 内的两个 leader 能够获得最新的 Region 信息

你需要使 raft_store 支持处理 ConfChange 的命令，这个过程将是：

1. 调用 `ProposeConfChange` Propose ConfChange admin command
2. 日志被提交之后，更新 `RegionLocalState`，其中包括 `RegionEpoch` 和 `Peers`
3. 调用 `ApplyConfChange`

> Hints:
>
> - For executing `AddNode`, the newly added Peer will be created by heartbeat from the leader, check `maybeCreatePeer()` of `storeWorker`. At that time, this Peer is uninitialized and any information of its Region is unknown to us, so we use 0 to initialize its `Log Term` and `Index`. The leader then will know this Follower has no data (there exists a Log gap from 0 to 5) and it will directly send a snapshot to this Follower.
> - For executing `RemoveNode`, you should call the `destroyPeer()` explicitly to stop the Raft module. The destroy logic is provided for you.
> - Do not forget to update the region state in `storeMeta` of `GlobalContext`
> - Test code schedules the command of one conf change multiple times until the conf change is applied, so you need to consider how to ignore the duplicate commands of the same conf change.
>
> 提示：
>
> - 为了执行 `AddNode`，新添加的 Peer 将由 leader 的 heartbeat 创建，请检查 `storeWorker` 中的 `maybeCreatePeer`。新创建的 Peer 是未初始化的，并且 Region 的信息是未知的，因此将它的 `LogTerm` 和 `Index` 初始化为 0。这样 leader 将会知道这个 follower 没有数据，并且会直接发送快照给这个 follower
> - 为了执行 `RemoveNode`，你需要调用 `destoryPeer` 显式的停止 raft module
> - 不要忘记更新 GlobalContext 中的 storeMeta 内的 region state
> - 测试代码会将一个 conf change 命令调用多次，直到应用 conf change 为止，因此需要考虑如何忽略同一个 conf change 的重复命令

### Implement split region in raftstore

![raft_group](imgs/keyspace.png)

To support multi-raft, the system performs data sharding and makes each Raft group store just a portion of data. Hash and Range are commonly used for data sharding. TinyKV uses Range and the main reason is that Range can better aggregate keys with the same prefix, which is convenient for operations like scan. Besides, Range outperforms in split than Hash. Usually, it only involves metadata modification and there is no need to move data around.

为了支持 multi-raft，系统执行数据分片，每个 RaftGroup 只存储一部分数据。Hash 和 Range 通常用于数据分片，TinyKV 使用 Range，主要原因是 Range 可以更好的聚合有相同前缀和 key，这便于扫描等操作。此外与 Hash 相比，Range 的效果更好。通常它只涉及元数据修改，不需要移动数据

``` protobuf
message Region {
 uint64 id = 1;
 // Region key range [start_key, end_key).
 bytes start_key = 2;
 bytes end_key = 3;
 RegionEpoch region_epoch = 4;
 repeated Peer peers = 5
}
```

Let’s take a relook at Region definition, it includes two fields `start_key` and `end_key` to indicate the range of data which the Region is responsible for. So split is the key step to support multi-raft. In the beginning, there is only one Region with range [“”, “”). You can regard the key space as a loop, so [“”, “”) stands for the whole space. With the data written, the split checker will checks the region size every `cfg.SplitRegionCheckTickInterval`, and generates a split key if possible to cut the Region into two parts, you can check the logic in
`kv/raftstore/runner/split_check.go`. The split key will be wrapped as a `MsgSplitRegion` handled by `onPrepareSplitRegion()`.

To make sure the ids of the newly created Region and Peers are unique, the ids are allocated by the scheduler. It’s also provided, so you don’t have to implement it. `onPrepareSplitRegion()` actually schedules a task for the pd worker to ask the scheduler for the ids. And make a split admin command after receiving the response from scheduler, see `onAskSplit()` in `kv/raftstore/runner/scheduler_task.go`.

So your task is to implement the process of handling split admin command, just like conf change does. The provided framework supports multiple raft, see `kv/raftstore/router.go`. When a Region splits into two Regions, one of the Regions will inherit the metadata before splitting and just modify its Range and RegionEpoch while the other will create relevant meta information.

Region 的 `start_key` 和 `end_key` 指示了它负责的数据范围，因此 split 是支撑 multi-raft 的关键步骤。开始的时候，只有一个 Region 范围是 ["", "")。写入数据后，split checker 将每隔 `cfg.SplitRegionCheckTickInterval` 检查一次 Region 的大小，如果可能将 Region 拆分的话会生成一个 split key，你可以在 `kv/raftstore/runner/split_check.go` 中检查逻辑。split key 将被包装为 `MsgSplitRegion`，并由 `onPrepareSplitRegion()` 处理

为了确保新创建的 Region 和 Peers 的 id 是唯一的，id 将由 scheduler 分配，这部分已经提供了，所以你不需要实现。`onPrepareSplitRegion()` 实际上调度了一个任务给 pd worker，它询问 scheduler 获得 ids。并在收到 scheduler 的响应之后发出 split admin command，具体的逻辑看 `kv/raftstore/runner/scheduler_task.go` 里面的 `onAskSplit()`

因此，您的任务是实现处理 split admin command 的过程，就想 conf change 一样。提供的框架支持 multi-raft，具体的看 `kv/raftstore/router.go`。当一个 Region 分裂为两个 Region 的时候，其中一个 Region 将继承拆分前的元数据并且只需要修改它的 Range 和 RegionEpoch，另一个 Region 将创建相关的元数据

> Hints:
>
> - The corresponding Peer of this newly-created Region should be created by
> `createPeer()` and registered to the router.regions. And the region’s info should be inserted into `regionRanges` in ctx.StoreMeta.
> - For the case region split with network isolation, the snapshot to be applied may have overlap with the existing region’s range. The check logic is in `checkSnapshot()` in `kv/raftstore/peer_msg_handler.go`. Please keep it in mind when implementing and take care of that case.
> - Use `engine_util.ExceedEndKey()` to compare with region’s end key. Because when the end key equals “”, any key will equal or greater than “”. > - There are more errors need to be considered: `ErrRegionNotFound`,`ErrKeyNotInRegion`, `ErrEpochNotMatch`.
>
> 提示：
>
> - 和新创建的 Region 相关联的 Peer 应该在 `createrPeer()` 创建，并注册到 router.regions。Region 的信息应该插入 ctx.StoreMeta 中的 `regionRanges`
> - 对于使用网络隔离拆分的测试程序，要应用的快照可能与现有 Region 的范围重叠。检查的逻辑在 `kv/raftstore/peer_msg_handler.go` 中的 `checkSnapshot()`。请在实现的时候牢记这一点
> - 使用 `engine_util.ExceedEndKey()` 比较 region 的 end key。当 end key 为 "" 的时候，任何 key 都将大于或等于 ""。需要考虑更多的 errors：`ErrRegionNotFound`,`ErrKeyNotInRegion`, `ErrEpochNotMatch`

## Part C

As we have instructed above, all data in our kv store is split into several regions, and every region contains multiple replicas. A problem emerged: where should we place every replica? and how can we find the best place for a replica? Who sends former AddPeer and RemovePeer commands? The Scheduler takes on this responsibility.

scheduler 的工作：放置副本、找到副本放置的最佳位置

To make informed decisions, the Scheduler should have some information about the whole cluster. It should know where every region is. It should know how many keys they have. It should know how big they are…  To get related information, the Scheduler requires that every region should send a heartbeat request to the Scheduler periodically. You can find the heartbeat request structure `RegionHeartbeatRequest` in `/proto/proto/schedulerpb.proto`. After receiving a heartbeat, the scheduler will update local region information.

Scheduler 需要知道整个集群的信息，他需要知道每个 region 在哪里，需要知道每个 region 有多少 key，需要知道每个 region 多大。为了获取这些信息，Scheduler 要求每个 region 定期的发送 heartbeat 请求给 scheduler（`RegionHeartbeatRequest`）。在收到 heartbeat 之后，scheduler 会更新 region 信息

Meanwhile, the Scheduler checks region information periodically to find whether there is an imbalance in our TinyKV cluster. For example, if any store contains too many regions, regions should be moved to other stores from it. These commands will be picked up as the response for corresponding regions’ heartbeat requests.

Scheduler 也会周期性的检查 region 信息，以此发现 region 是否是不平衡的。例如，如果某个 store 存储了太多的 regions，那么应该移动一部分 region 到别的 store。这些命令回作为相应 region 的 heartbeat 请求的响应

In this part, you will need to implement the above two functions for Scheduler. Follow our guide and framework, and it won’t be too difficult.

### The Code

The code you need to modify is all about `scheduler/server/cluster.go` and `scheduler/server/schedulers/balance_region.go`. As described above, when the Scheduler received a region heartbeat, it will update its local region information first. Then it will check whether there are pending commands for this region. If there is, it will be sent back as the response.

Scheduler 收到 region heartbeat 之后，首先会更新 local region 信息，然后检查是否有命令需要处理 region

You only need to implement `processRegionHeartbeat` function, in which the Scheduler updates local information; and `Schedule` function for the balance-region scheduler, in which the Scheduler scans stores and determines whether there is an imbalance and which region it should move.

`processRegionHeartbeat` 函数用来更新 local region 信息；`Schedule` 函数扫描 store 并决定是否出现了不平很，以及哪个 region 需要被移动

### Collect region heartbeat

As you can see, the only argument of `processRegionHeartbeat` function is a regionInfo. It contains information about the sender region of this heartbeat. What the Scheduler needs to do is just to update local region records. But <u>should it update these records for every heartbeat?</u>

Definitely not! There are two reasons. One is that updates could be skipped when no changes have been made for this region. The more important one is that the Scheduler cannot trust every heartbeat. Particularly speaking, if the cluster has partitions in a certain section, the information about some nodes might be wrong.

Scheduler 不需要每个 heatbeat 消息的时候都更新本地 region 信息，理由有两个：第一个是当 region 没有改变的时候不需要更新；第二个是如果集群出现了分区，那么心跳中包含的 region 信息可能是错误的，因此 Scheduler 无法相信每个 heatbeat

For example, some Regions re-initiate elections and splits after they are split, but another isolated batch of nodes still sends the obsolete information to Scheduler through heartbeats. So for one Region, either of the two nodes might say that it's the leader, which means the Scheduler cannot trust them both.

例如一些 rgeion 在分裂后重新启动选举和分裂，但是被隔离的节点仍然发送过时的信息给 Scheduler。对于某个 region来说，有两个节点都认为自己是 leader，因此 Scheduler 无法相信它们

Which one is more credible? The Scheduler should use `conf_ver` and `version` to determine it, namely `RegionEpoch`. The Scheduler should first compare the values of the Region version of two nodes. If the values are the same, the Scheduler compares the values of the configuration change version. The node with a larger configuration change version must have newer information.

通过 RegionEpoch 来决定相信哪个 heartbeat，首先比较 Ver，然后比较 ConVer

Simply speaking, you could organize the check routine in the below way:

1. Check whether there is a region with the same Id in local storage. If there is and at least one of the heartbeats’ `conf_ver` and `version` is less than its, this heartbeat region is stale（检查是否有两个 region 的 id 是一样的，conf_ver 和 version 小的那个是过时的 region）

2. If there isn’t, scan all regions that overlap with it. The heartbeats’ `conf_ver` and `version` should be greater or equal than all of them, or the region is stale.（如果没有编号相同的 region，扫描与它重叠的所有 region，conf_ver 和 version 应该大于等于这些重叠 region，否则的话当前 region 信息就是过时的）

Then how the Scheduler determines whether it could skip this update? We can list some simple conditions:

* If the new one’s `version` or `conf_ver` is greater than the original one, it cannot be skipped

* If the leader changed,  it cannot be skipped

* If the new one or original one has pending peer,  it cannot be skipped

* If the ApproximateSize changed, it cannot be skipped

* …

Don’t worry. You don’t need to find a strict sufficient and necessary condition. Redundant updates won’t affect correctness.

If the Scheduler determines to update local storage according to this heartbeat, there are two things it should update: region tree and store status. You could use `RaftCluster.core.PutRegion` to update the region tree and use `RaftCluster.core.UpdateStoreStatus` to update related store’s status (such as leader count, region count, pending peer count… ).

当 Scheduler 需要更新本地信息的时候，它需要更新两个数据：region tree 和 store status。`RaftCluster.core.PutRegion` 用来更新 region tree，`RaftCluster.core.UpdateStoreStatus` 用来更新 store status（leader count，region count，pending peer count）

### Implement region balance scheduler

There can be many different types of schedulers running in the Scheduler, for example, balance-region scheduler and balance-leader scheduler. This learning material will focus on the balance-region scheduler.

Scheduler 启动了很多个 schedulers，例如 banlacnce-region scheduler, balance-leader scheduler。在这里重点关注 balance-region scheduler

Every scheduler should have implemented the Scheduler interface, which you can find in `/scheduler/server/schedule/scheduler.go`. The Scheduler will use the return value of `GetMinInterval` as the default interval to run the `Schedule` method periodically. If it returns null (with several times retry), the Scheduler will use `GetNextInterval` to increase the interval. By defining `GetNextInterval` you can define how the interval increases. If it returns an operator, the Scheduler will dispatch these operators as the response of the next heartbeat of the related region.

Scheduler 模块会使用 `GetMinInterval` 的返回值作为时间间隔，以此来周期性的执行 `Schedule` 函数。如果它返回 null，就会使用 `GetNextInterval` 增加时间间隔，通过定义 `GetNextInterval` 你可以定义时间间隔是如何增加的。如果它返回 operator，Scheduler 将会调度这些 operators 作为相关 region 下一个心跳的响应

The core part of the Scheduler interface is `Schedule` method. The return value of this method is `Operator`, which contains multiple steps such as `AddPeer` and `RemovePeer`. For example, `MovePeer` may contain `AddPeer`,  `transferLeader` and `RemovePeer` which you have implemented in former part. Take the first RaftGroup in the diagram below as an example. The scheduler tries to move peers from the third store to the fourth. First, it should `AddPeer` for the fourth store. Then it checks whether the third is a leader, and find that no, it isn’t, so there is no need to `transferLeader`. Then it removes the peer in the third store.

`Schedule` 函数的返回值是 `Operator`，包含了 AddPeer 和 RemovePeer。MovePeer 可能包括 `AddPeer`、`transferLeader`、以及 `RemovePeer` 操作（移动一个 peer 的时候）

You can use the `CreateMovePeerOperator` function in `scheduler/server/schedule/operator` package to create a `MovePeer` operator.

![balance](imgs/balance1.png)

![balance](imgs/balance2.png)

In this part, the only function you need to implement is the `Schedule` method in `scheduler/server/schedulers/balance_region.go`. This scheduler avoids too many regions in one store. First, the Scheduler will select all suitable stores. Then sort them according to their region size. Then the Scheduler tries to find regions to move from the store with the biggest region size.

首先选择 store：balance-region scheduler 用来避免太多 region 堆积在一个 store。首先 scheduler 会找到所有合适的 sotres，然后根据 region 的大小对它们进行排序，然后 scheduler 会移动 region size 最大的 store 中的 region

The scheduler will try to find the region most suitable for moving in the store. First, it will try to select a pending region because pending may mean the disk is overloaded. If there isn’t a pending region, it will try to find a follower region. If it still cannot pick out one region, it will try to pick leader regions. Finally, it will select out the region to move, or the Scheduler will try the next store which has a smaller region size until all stores will have been tried.

然后选择 region：scheduler 首先尝试使用 pending region，因为 pending 可能意味着磁盘已经满了；然后会尝试 follower region；再然后会尝试 leader region。最后，会选择一个需要移动的 region，或者 scheduler 将尝试具有较小 region size 的下一个 store，直到所有的 store 都已经尝试过

After you pick up one region to move, the Scheduler will select a store as the target. Actually, the Scheduler will select the store with the smallest region size. Then the Scheduler will judge whether this movement is valuable, by checking the difference between region sizes of the original store and the target store. If the difference is big enough, the Scheduler should allocate a new peer on the target store and create a move peer operator.

scheduler 会选择一个 region size 最小的 store 移动 region，它会判断 original 和 target 两个 store 上的 region size，如果 size 差值足够大，就会分配一个新的 peer 在 target store 上并且创建一个 MovePeer operator

As you might have noticed, the routine above is just a rough process. A lot of problems are left:

* Which stores are suitable to move?

In short, a suitable store should be up and the down time cannot be longer than `MaxStoreDownTime` of the cluster, which you can get through `cluster.GetMaxStoreDownTime()`.（适合被移动的 store 的条件：停机时间不能超过集群的 MaxStoreDownTime）

* How to select regions?

The Scheduler framework provides three methods to get regions. `GetPendingRegionsWithLock`, `GetFollowersWithLock` and `GetLeadersWithLock`. The Scheduler can get related regions from them. And then you can select a random region.（从 PendingRegions、FollowerRegions、LeaderRegion 中随机选择一个 region）

* How to judge whether this operation is valuable?

If the difference between the original and target stores’ region sizes is too small, after we move the region from the original store to the target store, the Scheduler may want to move back again next time. So we have to make sure that the difference has to be bigger than two times the approximate size of the region, which ensures that after moving, the target store’s region size is still smaller than the original store.

region size 差值的定义：确保差值必须大于 region 近似大小的两倍，这样可以确保在移动之后 target sotre 的 region size 仍然小于 original store，避免移动之后又移回来
