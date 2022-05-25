# Project 4: Transactions

In the previous projects, you have built a key/value database which, by using Raft, is consistent across multiple nodes. To be truly scalable, the database must be able to handle multiple clients. With multiple clients, there is a problem: what happens if two clients try to write the same key at 'the same time'? If a client writes and then immediately reads that key, should they expect the read value to be the same as the written value? In project4, you will address such issues by building a transaction system into our database.

The transaction system will be a collaborative protocol between the client (TinySQL) and server (TinyKV). Both partners must be implemented correctly for the transactional properties to be ensured. We will have a complete API for transactional requests, independent of the raw request that you implemented in project1 (in fact, if a client uses both raw and transactional APIs, we cannot guarantee the transactional properties).

Transactions  promise [*snapshot isolation* (SI)](https://en.wikipedia.org/wiki/Snapshot_isolation). That means that within a transaction, a client will read from the database as if it were frozen in time at the start of the transaction (the transaction sees a *consistent* view of the database). Either all of a transaction is written to the database or none of it is (if it conflicts with another transaction).

To provide SI, you need to change the way that data is stored in the backing storage. Rather than storing a value for each key, you'll store a value for a key and a time (represented by a timestamp). This is called <u>multi-version concurrency control (MVCC)</u> because multiple different versions of a value are stored for every key.

You'll implement MVCC in part A. In parts B and C you’ll implement the transactional API.

这一部分实验主要解决两个问题：多个客户端同时写同一个 key 会发生什么？当一个客户端写后立即读，是否应该读到刚刚写入的值？project4 使用事务系统解决这两个问题

事务系统是客户端（TinySQL）和服务器（TinyKV）之间的协作协议，必须正确实现 TinySQL 和 TinyKV 才能确保事务属性。我们将为事务请求提供一个完整的 API，独立于在 project1 中实现的 raw API（事实上，如果客户端同时使用原始API和事务API，我们无法保证事务属性）

事务保证快照隔离性，这意味着在事务中，客户机将从数据库中读取数据，就好像它在事务开始时被及时冻结一样（事务会看到数据库的一致视图）。要么将所有事务写入数据库，要么不写入数据库（如果它与另一个事务冲突）

要提供快照隔离，您需要更改数据在后备存储中的存储方式。您将存储一个 key 的值和一个时间戳。这被称为多版本并发控制（MVCC），因为每个键都存储了一个值的多个不同版本

Part A 实现 MVCC，Part B 和 Part C 实现事务 API

## Transactions in TinyKV

TinyKV's transaction design follows [Percolator](https://storage.googleapis.com/pub-tools-public-publication-data/pdf/36726.pdf); it is a two-phase commit protocol (2PC).

A transaction is a list of reads and writes. A transaction has a start timestamp and, when a transaction is committed, it has a commit timestamp (which must be greater than the starting timestamp). The whole transaction reads from the version of a key that is valid at the start timestamp. After committing, all writes appear to have been written at the commit timestamp. Any key to be written must not be written by any other transaction between the start and commit timestamps, otherwise, the whole transaction is canceled (this is called a write conflict).

The protocol starts with the client getting a start timestamp from TinyScheduler. It then builds the transaction locally, reading from the database (using a `KvGet` or `KvScan` request which includes the start timestamp, in contrast to  `RawGet` or `RawScan` requests), but only recording writes locally in memory. Once the transaction is built, the client will select one key as the *primary key* (note that this has nothing to do with an SQL primary key). The client sends `KvPrewrite` messages to TinyKV. A `KvPrewrite` message contains all the writes in the transaction. A TinyKV server will attempt to lock all keys required by the transaction. If locking any key fails, then TinyKV responds to the client that the transaction has failed. The client can retry the transaction later (i.e., with a different start timestamp). If all keys are locked, the prewrite succeeds. Each lock stores the primary key of the transaction and a time to live (TTL).

In fact, since the keys in a transaction may be in multiple regions and thus be stored in different Raft groups, the client will send multiple `KvPrewrite` requests, one to each region leader. Each prewrite contains only the modifications for that region. If all prewrites succeed, then the client will send a commit request for the region containing the primary key. The commit request will contain a commit timestamp (which the client also gets from TinyScheduler) which is the time at which the transaction's writes are committed and thus become visible to other transactions.

If any prewrite fails, then the transaction is rolled back by the client by sending a `KvBatchRollback` request to all regions (to unlock all keys in the transaction and remove any prewritten values).

In TinyKV, TTL checking is not performed spontaneously. To initiate a timeout check, the client sends the current time to TinyKV in a `KvCheckTxnStatus` request. The request identifies the transaction by its primary key and start timestamp. The lock may be missing or already be committed; if not, TinyKV compares the TTL on the lock with the timestamp in the `KvCheckTxnStatus` request. If the lock has timed out, then TinyKV rolls back the lock. In any case, TinyKV responds with the status of the lock so that the client can take action by sending a `KvResolveLock` request. The client typically checks transaction status when it fails to prewrite a transaction due to another transaction's lock.

If the primary key commit succeeds, then the client will commit all other keys in the other regions. These requests should always succeed because by responding positively to a prewrite request, the server is promising that if it gets a commit request for that transaction then it will succeed. Once the client has all its prewrite responses, the only way for the transaction to fail is if it times out, and in that case, committing the primary key should fail. Once the primary key is committed, then the other keys can no longer time out.

If the primary commit fails, then the client will rollback the transaction by sending `KvBatchRollback` requests.

**翻译：**

事务是读和写的列表。事务有一个开始时间戳，当事务被提交时，它有一个提交时间戳(必须大于开始时间戳)。整个事务读取在起始时间戳有效的键的版本。提交之后，所有写操作似乎都在提交时间戳写入。在开始时间戳和提交时间戳之间，任何要写入的键都不能由任何其他事务写入，否则，整个事务将被取消(这称为写入冲突)。

首先客户端会从 TinyScheduler 获取开始时间戳，然后它在本地构建事务从数据库读取（使用包含开始时间戳的 `KvGet` 或者 `KvScan` 请求，而不是 `RawGet` 或者 `RawScan`），但仅记录内存中的本地写操作。一旦事务建立了，客户端将会选择一个 key 作为 primary key（这里不是指 SQL 的主键）。客户端会发送 `KvPrewrite` 消息给 TinyKV，`KvPreWrite` 消息包含事务中的所有写请求。TinyKV server 会尝试锁住所有这些 key，如果任意一个 key lock fail，TinyKV 会回复客户端事务失败了，客户端可以在之后再次尝试这个事务（使用不同的开始时间戳）。如果所有的 key 都成功锁住，prewrite 成功，每个锁存储事务的主键（primary key）和生存时间（TTL）

事实上，由于事务中的密钥可能位于多个 Region，因此存储在不同的 RaftGroup 中，因此客户端将发送多个 KvPrewrite 请求，每个 region leader 一个。每个 prewrite 只包含该 Region 的修改，如果所有的 prewrite 都成功，那么客户端将向包含主键的 region 发送提交请求。提交请求包含一个提交时间戳（也从 TinyScheduler）获取，这个时间戳是提交事务的写入时间，因此对其他事物可见

如果某个 prewrite 失败了，客户端会发送 `KvBatchRollback` 请求给所有的 regions 以此来对事务进行回滚（解锁事务中的所有 key 并删除所有预写的值）

在 TinyKV 中 TTL 检查不是自发执行的。要启动超时检查，客户端会在 `KvCheckTxnStatus` 请求中向 TinyKV 发送当前时间，该请求通过其主键和开始时间戳标识事务。锁可能丢失或者已经提交，如果没有的话 TinyKV 会将锁上的 TTL 与 `KvCheckTxnStatus` 请求中的时间戳进行比较，如果锁已经过期了，TinyKV 会回滚这个锁。在任何情况下，TinyKV 都会以锁的状态进行响应，以便客户端可以通过发送 `KvResolveLock` 请求来采取行动。客户端通常会在因为另一个事务的锁在城当前事务无法预写的时候，检查事务的状态

一旦主键提交成功，客户端会提交所有在其他 region 上的 keys。这些请求总是成功的，因为通过积极响应预写请求，服务器承诺如果它获得该事务的提交请求，那么它将成功。一旦客户端收到了所有预写响应，事务失败唯一的方法就是超时，在这种情况下，提交主键应该失败。一旦提交了主键，其他键就不能再超时

如果主键提交失败，客户端会通过发送 `KvBatchRollback` 请求对事务进行回滚

## Part A

The raw API you implemented in earlier projects maps user keys and values directly to keys and values stored in the underlying storage (Badger). Since Badger is not aware of the distributed transaction layer, you must handle transactions in TinyKV, and *encode* user keys and values into the underlying storage. This is implemented using multi-version concurrency control (MVCC). In this project, you will implement the MVCC layer in TinyKV.

Implementing MVCC means representing the transactional API using a simple key/value API. Rather than store one value per key, TinyKV stores every version of a value for each key. For example, if a key has value `10`, then gets set to `20`, TinyKV will store both values (`10` and `20`) along with timestamps for when they are valid.

TinyKV uses three column families (CFs): `default` to hold user values, `lock` to store locks, and `write` to record changes. The `lock` CF is accessed using the user key; it stores a serialized `Lock` data structure (defined in [lock.go](/kv/transaction/mvcc/lock.go)). The `default` CF is accessed using the user key and the *start* timestamp of the transaction in which it was written; it stores the user value only. The `write` CF is accessed using the user key and the *commit* timestamp of the transaction in which it was written; it stores a `Write` data structure (defined in [write.go](/kv/transaction/mvcc/write.go)).

A user key and timestamp are combined into an *encoded key*. Keys are encoded in such a way that the ascending order of encoded keys orders first by user key (ascending), then by timestamp (descending). Helper functions for encoding and decoding keys are defined in [transaction.go](/kv/transaction/mvcc/transaction.go).

This exercise requires implementing a single struct called `MvccTxn`. In parts B and C, you'll use the `MvccTxn` API to implement the transactional API. `MvccTxn` provides read and write operations based on the user key and logical representations of locks, writes, and values. Modifications are collected in `MvccTxn` and once all modifications for a command are collected, they will be written all at once to the underlying database. This ensures that commands succeed or fail atomically. Note that an MVCC transaction is not the same as a TinySQL transaction. An MVCC transaction contains the modifications for a single command, not a sequence of commands.

`MvccTxn` is defined in [transaction.go](/kv/transaction/mvcc/transaction.go). There is a stub implementation, and some helper functions for encoding and decoding keys. Tests are in [transaction_test.go](/kv/transaction/mvcc/transaction_test.go). For this exercise, you should implement each of the `MvccTxn` methods so that all tests pass. Each method is documented with its intended behavior.

> Hints:
>
> - An `MvccTxn` should know the start timestamp of the request it is representing.
> - The most challenging methods to implement are likely to be `GetValue` and the methods for retrieving writes. You will need to use `StorageReader` to iterate over a CF. Bear in mind the ordering of encoded keys, and remember that when deciding when a value is valid depends on the commit timestamp, not the start timestamp, of a transaction.

**翻译：**

前面实验中实现的 Raw API 将 user key 和 value 直接映射为存储到 badger 中的 key 和 value。因为Badger不知道分布式事务层，所以你必须在TinyKV中处理事务，并将user key和value编码到底层存储中。这是使用多版本并发控制(MVCC)实现的。在这个项目中，你将在TinyKV中实现MVCC层

实现MVCC意味着使用一个简单的 key/value API来表示事务API。TinyKV不是为每个键存储一个值，而是为每个键存储一个值的每个版本。例如，如果一个键的值为10，然后被设置为20,TinyKV将存储这两个值(10和20)以及它们有效的时间戳。

TinyKV 使用三个 CF：`default` 表示 user values，`lock` 保存 store locks，`write` 记录数据变更。`lock` CF 使用 user key 进行访问，他保存了序列化的 `Lock` 数据结构（定义在 lock.go 中）`defaut` 通过 user key 和开始时间戳进行访问，他保存了 user value。`write` 通过 user key 和提交时间戳进行访问，他保存了 `Write` 数据结构

user key 和 timestamp 被合并为 encoded key。编码方式为，已编码的键按升序排列，首先按用户键(升序)，然后按时间戳(降序)。这确保了查找已编码的 key 的时候，将首先给出最新版本

这部分需要实现 `MvccTxn` 数据结构，在 Part B 和 Part C 将会使用 `MvccTxn` API 实现事务 API。`MvccTxn` 提供基于 user key 以及 lock、write、value 逻辑表示的读写操作。修改被收集到MvccTxn中，一旦收集到一个命令的所有修改，它们将被一次性写入底层数据库。这确保命令以原子方式成功或失败。请注意，MVCC事务与TinySQL事务不同。MVCC事务包含单个命令的修改，而不是一系列命令

`MvccTxn` 定义在 transaction.go 中，又一个基本的实现代码以及一些编码和解码的辅助函数。测试程序位于 transaction_test.go。这一部分你需要实现每个 `MvccTxn` 的方法，以便通过所有的测试，每种方法都记录了其预期的行为

提示：

1. 一个 `MvccTxn` 需要知道该请求对应的开始时间戳
2. 最具挑战性的函数可能是 `GetValue` 以及检索 write 的函数。你需要使用 `StorageReader` 来遍历 CF，记住已编码键的顺序，记住判断一个值何时有效取决于事务的提交时间戳，而不是事务的开始时间戳

## Part B

In this part, you will use `MvccTxn` from part A to implement handling of `KvGet`, `KvPrewrite`, and `KvCommit` requests. As described above, `KvGet` reads a value from the database at a supplied timestamp. If the key to be read is locked by another transaction at the time of the `KvGet` request, then TinyKV should return an error. Otherwise, TinyKV must search the versions of the key to find the most recent, valid value.

`KvPrewrite` and `KvCommit` write values to the database in two stages. Both requests operate on multiple keys, but the implementation can handle each key independently.

`KvPrewrite` is where a value is actually written to the database. A key is locked and a value stored. We must check that another transaction has not locked or written to the same key.

`KvCommit` does not change the value in the database, but it does record that the value is committed. `KvCommit` will fail if the key is not locked or is locked by another transaction.

You'll need to implement the `KvGet`, `KvPrewrite`, and `KvCommit` methods defined in [server.go](/kv/server/server.go). Each method takes a request object and returns a response object, you can see the contents of these objects by looking at the protocol definitions in [kvrpcpb.proto](/proto/kvrpcpb.proto) (you shouldn't need to change the protocol definitions).

TinyKV can handle multiple requests concurrently, so there is the possibility of local race conditions. For example, TinyKV might receive two requests from different clients at the same time, one of which commits a key, and the other rolls back the same key. To avoid race conditions, you can *latch* any key in the database. This latch works much like a per-key mutex. One latch covers all CFs. [latches.go](/kv/transaction/latches/latches.go) defines a `Latches` object which provides API for this.

> Hints:
>
> - All commands will be a part of a transaction. Transactions are identified by a start timestamp (aka start version).
> - Any request might cause a region error, these should be handled in the same way as for the raw requests. Most responses have a way to indicate non-fatal errors for situations like a key being locked. By reporting these to the client, it can retry a transaction after some time.

**翻译：**

这部分需要使用 Part A 实现的 MvccTxn 实现 KvGet、KvPrewrite、KvCommit 请求。`KvGet` 根据提供的时间戳从数据库中读取值，如果 key 被另一个事务锁定了，则返回一个 error，否则的话 TinyKV 需要找到最近一个有效的 value

`KvPrewrite` 和 `KvCommit` 通过两个阶段向数据库中写入值，这两个请求都会操作多个键，但是实现可以独立处理每个键。`KvPrewrite` 用来真正的将值写入到数据库，锁定 key 然后写入 value，我们必须要检查没有别的事务锁定或写入相同的 key。`KvCommit` 不会改变在数据库中的值，但是它会记录 value 已经被提交，如果 key 没有被锁定或者被其他事务锁定了，`KvCommit` 会执行失败

你需要实现 `KvGet`、`KvPrewrite` 和 `KvCommit` 函数，这些函数定义在 server.go 中。每个函数接受一个请求对象并返回一个响应对象，你可以通过查看 kvrpcpb.proto 中的协议定义，查看请求和回复中的内容

TinyKV 可以同时并发处理多个请求，因此可能会存在 race condition。例如，TinyKV 收到了两个不同的客户端对同一个 key 的请求，其中一个 commits 这个 key，另一个 rollback 这个 key。为了避免竞态条件，你可以锁定数据库中的 key，latch 的工作原理很像每个 key 的 mutex。一个 latch 覆盖了所有的 CFs，latches.go 定义了 Latchs 对象以及它的 API

> 提示：
>
> - 所有的 command 都是事务的一部分，事务由开始时间戳标识（又叫开始版本）
> - 任何请求都可能导致 Region error，这个时候应该和 raw requests 相同的方式处理。对于 key 被锁定的情况，大多数 repsonse 都有一种指示非致命错误的方法，通过向客户端报告这些，它可以在一段时间后重试事务

## Part C

In this part, you will implement `KvScan`, `KvCheckTxnStatus`, `KvBatchRollback`, and `KvResolveLock`. At a high-level, this is similar to part B - implement the gRPC request handlers in [server.go](/kv/server/server.go) using `MvccTxn`.

`KvScan` is the transactional equivalent of `RawScan`, it reads many values from the database. But like `KvGet`, it does so at a single point in time. Because of MVCC, `KvScan` is significantly more complex than `RawScan` - you can't rely on the underlying storage to iterate over values because of multiple versions and key encoding.

`KvCheckTxnStatus`, `KvBatchRollback`, and `KvResolveLock` are used by a client when it encounters some kind of conflict when trying to write a transaction. Each one involves changing the state of existing locks.

`KvCheckTxnStatus` checks for timeouts, removes expired locks and returns the status of the lock.

`KvBatchRollback` checks that a key is locked by the current transaction, and if so removes the lock, deletes any value and leaves a rollback indicator as a write.

`KvResolveLock` inspects a batch of locked keys and either rolls them all back or commits them all.

> Hints:
>
> - For scanning, you might find it helpful to implement your own scanner (iterator) abstraction which iterates over logical values, rather than the raw values in underlying storage. `kv/transaction/mvcc/scanner.go` is a framework for you.
> - When scanning, some errors can be recorded for an individual key and should not cause the whole scan to stop. For other commands, any single key causing an error should cause the whole operation to stop.
> - Since `KvResolveLock` either commits or rolls back its keys, you should be able to share code with the `KvBatchRollback` and `KvCommit` implementations.
> - A timestamp consists of a physical and a logical component. The physical part is roughly a monotonic version of wall-clock time. Usually, we use the whole timestamp, for example when comparing timestamps for equality. However, when calculating timeouts, we must only use the physical part of the timestamp. To do this you may find the `PhysicalTime` function in [transaction.go](/kv/transaction/mvcc/transaction.go) useful.

**翻译：**

`KvScan` 类似于 `RawScan`，但是在 MVCC 中比 `RawScan` 更加复杂，这是因为存在多个版本的 key，所以不能依赖底层存储进行迭代

`KvCheckTxnStatus`, `KvBatchRollback`, `KvResolveLock` 在客户端编写事务遇到冲突的时候使用，每个函数都涉及更改现有锁的状态

`KvCheckTxnStatus` 检查超时时间，删除过期的锁并返回锁的状态

`KvBatchRollback` 检查 key 是否被当前事务锁定，如果是则移除锁定，删除 value 并且以 rollback 作为 write 写入

`KvResolveLock` 检查一批锁定的 key，要么全部回滚要么全部提交

> 提示：
>
> - 对于 Scan 操作，迭代逻辑值而不是底层存储中的原始值，框架提供了 `kv/transaction/mvcc/scanner.go`
> - 扫描的时候，某些 key 可能会记录一些错误，但是不应该停止整个扫描。对于其他命令，任何导致错误的单个 key 都会使整个操作停止
> - 因为 `KvResolveLock` 提交或者回滚 key，你应该能够与 `KvBatchRollback` 和 `KvCommit` 共享代码
> - 时间戳由一个物理组件和一个逻辑组件组成，物理部分大致是壁挂钟的单调版本。通常，我们使用整个时间戳，例如在比较时间戳是否相等的时候。但是，在计算超时时，必须只使用时间戳的物理部分

