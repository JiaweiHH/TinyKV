# Project4 Transactions

### 事务 ACID

什么是事务？首先简单介绍一下事务的概念。事务具有 ACID 特性：

A：原子性，注意这里的原子性的概念有别于多线程编程中的原子性概念。ACID 中的原子性不关心多个操作的并发性，也没有描述多个线程试图访问相同的数据时会发生什么，这部分由隔离性定义。ACID 的原子性指的是，当多个操作中间发生了错误，例如系统故障、进程崩溃、网络中断、磁盘满了等，这个时候事务会中止，并且丢弃或者撤销那些局部完成的更改

假如没有原子性，当多个更新操作中间发生了错误，就需要知道哪些更改已经发生了，这个寻找的过程会很麻烦。原子性简化了这个过程：如果事务已经中止，应用程序可以确定没有实质发生任何更改。<u>原子性的特征是：在出错时中止事务，并将部分完成的写入全部丢弃</u>

C：一致性，一致性其实并不源自数据库本身，不同场景的一致性有多种不同的含义，例如：最终一致性、一致性哈希、CAP 理论中的一致性等。在 ACID 中，一致性指的是数据库处于应用程序所期待的“预期状态”，例如账户贷款余额和借款余额保持平等

一致性本质上需要由应用层来维护这种状态一致性，应用层有责任正确的定义事务来保持一致性。ACID 中一致性更多的是应用层的属性，应用程序可以借助原子性和隔离性达到一致性

I：隔离性，也被称为可串行化，即假装某个事务是在数据库上唯一运行的事务，这里的概念等价于多线程中的原子性。隔离性会降低数据库的性能，因此实际实现中一般使用的是「弱隔离性」，例如快照隔离

D：持久性，它保证一旦事务提交成功，即使硬件故障或者数据库崩溃，事务所写入的任何数据都不会丢失。为了实现持久性，数据库必须等到这些写入完成之后才能报告事务成功提交

### 弱隔离级别

前面说到隔离性（可串性化隔离）会严重影响性能，因此实际中经常用到弱隔离级别（非串性化）。「如果你正在处理财务数据，请上 ACID 系统」

#### 读-提交

读-提交提供一下保证：

1. 读数据库的时候，只能读到已经成功提交的数据（防止“脏读”）
2. 写数据库的时候，只能覆盖已经成功提交的数据（防止“脏写”）

脏读指的是，某个事务只完成部分写入，但是还没有提交或者中止，此时另一个事务看到了中间结果。脏写指的是，某个事务只执行了部分写入，另一个事务覆盖了这部分写入

脏读的需求：

1. 如果事务需要更新多个对象，脏读意味着另一个事务可以看到部分更新，这是非法的。典型的例子是，电子邮件，如果收到了电子邮件，但是计数值没有更新
2. 事务中止，操作需要回滚的时候，如果发生了脏读，这意味着可以看到稍后会被回滚的数据

脏写的需求：

1. 事务 A 写了对象 X，事务 B 写了对象 X 和 Y 并提交事务，然后事务 A 接着写 Y 并提交首事务。如果 X 和 Y 是两个有关联的数据，那么这对数据中既包含了 A 的数据又包含了 B 的数据

【注意】读-提交不能防止计数器增量的竞争情况，事务 A 读取 X，事务 B 读取 X，事务 B 写入 X 并提交事务，事务 A 写入 X 并提交事务，导致事务 B 的写入丢失被覆盖了

**读-提交的实现**

防止脏写：使用行级锁，当事务想要修改某个对象的时候，必须先获得对象的锁，然后一直持有锁直到事务提交或者中止

防止脏读：对于每个正在更新的对象，数据库保存一份旧值和一份将要设置的值，两个版本的值。在事务提交之前，所有读操作都读取旧值，事务提交之后才可以读到新值

#### 快照隔离

读-提交 不能防止的场景：A 有两个银行账户 1 和 2，需要从账户 2 转账 100 到 账户 1

1. A 在初始的时候读取账户 1 的余额，此时是 500
2. 然后事务开始，先增加账户 1 的余额，账户 1 变为了 600（还没有提交）；然后减少账户 2 的余额，账户 2 变为了 400
3. 事务提交。A 读取账户 2 的余额，显示为 400，导致 A 以为丢了 100 元

相当于某人在提交转账请求之后，并在银行数据库执行转账过程中间，查看两个账户余额，会看到短暂的不一致

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220523205437047.png" alt="image-20220523205437047" style="zoom: 33%;" />

快照隔离的思想是：每个事务都从数据库的一致性快照中读取，事务开始时所看到的是最近提交的数据，即使数据库随后可能会被另一个事务更改，保证每个事务都只看到特定时间点的旧数据

**快照隔离的实现**

防止脏写：和读-提交一样，使用写锁来保证

防止脏读：由于多个正在执行的事务可能会在不同的时间点查看数据库的状态，所以数据库保留了对象多个不同的版本，这种技术也被称为多版本并发控制（MVCC）。对于读-提交来说，支持快照隔离的存储引擎往往直接采用 MVCC 来实现，只需要保存两个版本的数据：已提交的旧版本和未提交的新版本

对每个事务赋予一个唯一的事务 ID，当写入新的内容时所写的数据都会被标记为写入者的事务 ID。当事务需要删除某行的数据时，并没有直接删除，而是标记这个对象的请求删除事务 ID，等到没有其他事务引用这个对象的时候，数据库的垃圾回收进程会去真正删除

数据对象什么时候对事务可见？满足以下两个条件的对象对事务是可见的

1. 事务开始时刻，创建该对象的事务已经完成了提交
2. 对象没有被标记为删除，或者即使标记了，但删除事务在当前事务开始的时候还没有提交

## Part A

- CFDefaule：用于暂时存储 key 对应的 value，MVCC 机制会决定这个值是否被 commit 或者 delete（通过 user key + 时间戳 访问）
- CFLock：存储锁，如果某个 key 存在对应的 lock，说明它正在被某个事务修改（通过 user key 访问）
- CFWrite：存储 key 的每个版本的 value 的提交时间，每个 write 都有一个 WriteKind，表示这个版本的 value 执行了什么操作，例如 Put、Delete、Rollback（通过 user key + 时间戳访问）

TinyKV 将 key 按照对应的时间戳封装成新的 key 存储在 storage 中，新的 key 在 storage 中的存储顺序为：首先按照 user key 排序，然后相同 user key 的按照时间戳降序排序，这样查询的时候先查到的总是某个 user key 的最新版本

[TinyKV-Project4: Percolator分布式事务 - 知乎 (zhihu.com)](https://zhuanlan.zhihu.com/p/438064192)

### 实验过程

**GetValue 用来获取给定 user key 的 value**

1. 查找 user key 的最新 write 版本，获取 Write 数据结构
2. 从 Write 中解析 user key 是否相等，如果相等的话判断 Write 的类型是不是 WriteKindPut
3. 如果是 WriteKindPut 的话，使用 user key 和 Write 中保存的时间戳读取 default 中的 value

操作 1，Put {k1, v1}

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220523212059206.png" alt="image-20220523212059206" style="zoom:50%;" />

在时间戳为 3 的时候提交 Put {k1, v1}，这个时候 storage 中的数据如下

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220523212206590.png" alt="image-20220523212206590" style="zoom:50%;" />

操作 2，Put {k1, v2}

<img src="/Users/li/Library/Application Support/typora-user-images/image-20220523212310493.png" alt="image-20220523212310493" style="zoom:50%;" />

然后这个时候事务 A 尝试 GetValue，此时我们不能直接读取 Default，因为这个时候 v2 是不可见的，因为还没有提交。所以想要查找这个时候的 value，需要通过事务 A 的开始时间戳根据 Write 的提交时间戳找到最近一次的 Write，然后再根据 Write 的开始时间戳找到 Default

如果找到的第一个 write 的 Kind 是 delete 或者 key 不等于事务 A 想要查找的 user key，则说明数据库中没有关于这个 key 的 value

CurrentWrite：找到 user key 和当前事务开始时间相同的 Write；MostRecentWrite：找到 user key 的最近一次 Write

### 其他

1. 关于为什么要用 Write 的开始时间戳来获取 Default

寻找 Default 的时候一定要根据 Write 的开始时间戳，因为某一次事务对于 Default 的修改，Default 中 key 的时间戳是和 Write 的开始时间戳相等的（可以参考上面的例子表格），并且在 [Write.start_ts, Write.commit_ts] 之间是不可能有别的事务修改 Default 的，所以也只会找到唯一的 Default 列数据

2. 上面表格中的数据存储

事务在修改的时候，会添加一些新的键值对，其中包括了 {key = user_key + txn.StartTS, value = value}, {key = user_key, value = Lock}, {key = user_key + txn.CommitTS, value = Write}。Write 内部包含了 txn.StartTS，Lock 内部包含了 primary key

## Part B

### 两阶段提交

这里先简单介绍一下两阶段提交。对于单个数据库节点上执行的事务，原子性由存储引擎负责，数据库会首先将事务的写入持久化，然后把提交记录追加到磁盘日志文件中。如果在这个过程中数据库崩溃，那么当节点重启之后，事务可以从日志中恢复，恢复的规则为：如果在崩溃之前提交记录已经写入磁盘，则任务事务已经安全提交，否则的话会回滚该事务的所有写入（即在完成提交记录写之前如果发生崩溃，则事务需要中止；如果在提交记录写入完成之后发生崩溃，事务被认为安全提交）

上述过程是针对但节点的，但是对于一个分布式系统来说，有些节点提交事务成功，有些节点提交事务失败。由于事务的提交是不能撤销的，这就会导致节点上数据的不一致

**两阶段提交（2PC）**

2PC 是一种在多节点之间实现事务原子提交的算法，用来确保所有节点要么全部提交，要么全部终止。2PC 中有一个单节点事务没有的组件，协调者。当准备提交事务的时候，协调者开始阶段 1，发送一个准备请求到所有节点，询问它们是否可以提交，然后协调者会根据参与者的回应在阶段 2 发送提交或者不提交的请求

通常一个 2PC 的过程如下：

1. 应用程序启动一个分布式事务，向协调者请求事务 ID
2. 应用程序在每个参与节点上执行单节点事务，如果在这个阶段出现问题，例如节点崩溃或者请求超时，协调者和其他的参与者都可以安全中止
3. 应用程序准备提交，协调者向所有的参与者发送准备请求。如果准备请求中有任何一个失败或者超时，协调者会通知所有参与者放弃事务
4. 参与者收到准备请求之后，确保在任何情况下都可以提交事务。一旦向协调者回答“是”，节点就承诺会提交事务
5. 协调者收到所有参与者的答复，就是否提交事务作出决定。协调者首先将决定写入到磁盘事务日志中，防止稍后系统崩溃，并可以恢复之前的决定。写入磁盘的这个时刻称为**提交点**
6. 协调者将决定写入磁盘之后，接下来向素有参与者发送决定，如果这个时候出现失败或者超时，协调者必须一直重试，直到成功为止

**参与者发生故障**

如果在第一阶段发生故障，协调者会决定中止事务；如果在第二阶段发生故障，协调者会不断重试

**协调者发生故障**

一旦参与者回答了“是”，则表明参与者不能单方面的中止事务，它必须等待协调者的决定。在决定到来之前即便协调者出现故障，参与者也只能等待

参与者不能单方面的中止：例如协调者发送了提交决定给节点 2，但是在发送给节点 1 之前崩溃了，这个时候如果节点 1 中止事务，会导致数据不一致

参与者不能单方面提交事务：因为可能有些参与者投了否决票导致协调者最终中止事务

协调者恢复之后可以通过读取事务日志来确定所有未决事务的状态，如果在协调者日志中没有完成提交记录的事务就会中止

### Percolator

Percolator 分布式写事务是由 2PC 实现的，但是在传统 2PC 的基础上做了一些修改。对于一个写事务，分为 Prewrite 阶段和 Commit 阶段。在事务开始的时候 Client 会从 TSO 获取一个 Timestamp 作为事务的开始时间，在事务提交之前所有的写操作都是换存在内存中

Percolator 实现的是快照隔离级别，再回顾一下快照隔离中，数据对象的可见性条件：

1. 事务开始的时刻，创建该对象的事务已经完成了提交
2. 对象没有被标记为删除，或者即使标记了，但是删除事务在当前事务开始的时候还没有完成提交

**Prewrite**

1. 随机取事务中的某个 key 作为 primary key，其他的 key 变为 secondary
2. 对每个 key 进行冲突检测，冲突检测的依据可以参考快照隔离可见性的要求，具体步骤包括
   1. 在 start_ts 之后，检查 write 列是否有数据，如果有数据则说明有其他事务在当前事务开始之后提交了，当前事务的可见性发生变化，因此需要 abort
   2. 检查 lock 列是否有数据，不需要关心 lock 的时间戳，因为有 lock 就说明出现写冲突，当前事务需要等待 lock 释放
3. 锁定每个 key 所在的行，写入 {key = user_key, value = Lock} 的方式添加键值对，Lock 包含了 primary key、Timestamp、WriteKind 等信息；然后写入数据到 default 列，写入 {key = user_key + start_ts, value = value}

在 Prewrite 阶段还没有写入 Write，因此其他事务准备读取的时候是看不到当前还没有提交的修改的。因为读取的时候总是先找到最近一次的 Write，然后根据 Write.start_ts 找到 value

**Commit**

1. 从 TSO 获取 Timestamp 作为事务的提交时间
2. 检查 primary key 上面的 lock 的时间戳是否是当前事务的 start_ts，假如 lock 不存在则说明别的事务判断超时清除了当前事务的 lock。同理由于别的事务清除之后可能会加新的 lock，因此需要判断 lock 的时间戳是不是当前事务的 start_ts
3. 写入 Write 数据，写入 {key = user_key + commit_ts, value = Write}，Write 包含了 start_ts 和 WriteKind。清除 lock 列

primary key 的 Write 写入的时间点是 Percolator 的事务提交点，区别于传统 2PC Coordinator 写入本地磁盘提交决定是事务的提交点。一旦 primary key 提交成功，则整个事务就提交成功了。当需要回滚的时候，会根据 primary key 进行 rollback，如果 primary key 写入了则说明事务已经被提交，否则的话可以进行回滚

单节点上 Write 的写入和 Lock 的清除，由单行事务保证 ACID（TinyKV 的 Latches 加锁）

**其他区别**

读取数据的时候，如果发现数据被锁定了，需要等待解锁。可能会觉得可以通过 Write 读取到旧版本的数据，但是下面的例子说明在 Percolator 中这种读取可能是错误的

| T1            | T2           |
| ------------- | ------------ |
| Prewrite(key) |              |
| Get commit_ts |              |
|               | Get start_ts |
|               | Read(key)    |
| Commit        |              |

这个例子中 T2 正确读取的数据应该是最靠近 T2_start_ts 的 Write，然后对应的 value。由于 T1 的 T1_commit_ts < T2_start_ts 因此需要正确读取到 T1 最新的数据，但是由于在 T2 准备 Read 的时候 T1 还没有提交，所以 T2 需要等待 T1 提交。如果 lock 超时了可以尝试 rollback

但是如果锁的时间是位于 T2 之后的话，T2 就不需要等待锁匙放了，因为这个时候 T1 写入的数据无论什么时候提交必定是对 T2 不可见的

[Percolator - 分布式事务的理解与分析 - 知乎 (zhihu.com)](https://zhuanlan.zhihu.com/p/261115166)

### 实验思路

大致按照上述实现就可以了。测试代码会检测冲突提交，所以需要实现检测重复提交的逻辑，获取 CurrentWrite 判断时间戳以及 WriteKind

lock 为空的时候不需要返回 error，需要设置 resp.Error

[Fix TestCommitConflictRollback4B #373 - Github Lab](https://githublab.com/repository/issues/tidb-incubator/tinykv/373)

## Part C

### 实验思路

#### KvScan

Scanner.Next 的实现逻辑：

1. 读取 CfWrite
2. 跳过所有和当前 key 相等的 CfWrite
3. 读取 CfDefault

扫描 key，检查 Lock 是否为空，如果不为空并且时间戳在 request 时间戳之间，则记录 Error，否则记录 Value

#### KvCheckTxnStatus

`KvCheckTxnStatus` 检查超时时间，删除过期的锁并返回锁的状态

1. 获取 primary key 的 CurrentWrite，如果不是 WriteKindRollback 说明已经被 commit，返回 commitTs
2. 获取 Lock 检查 Lock 是否为空，如果为空的话则说明 primary key 被回滚了，这个时候创建一个 WriteKindRollback 的 Write
3. 如果 Lock 不为空，检查 Lock 是否 超时了，如果超时了则移除 Lock 和 Value，创建一个 WriteKindRollback 的 Write

#### KvBatchRollback

批量回滚 request 中的 key

1. 获取 key 的 CurrentWrite，如果已经是 WriteKindRollback，则说明这个 key 回滚完毕，跳过这个 key
2. 获取 Lock，如果 Lock 为空或者 Lock 的时间戳不是当前事务的时间戳，则中止操作，这个时候说明 key 被其他事务占用了
3. 否的的话移除 Lock 和 Value，创建 WriteKindRollback 的 Write

#### KvResolveLock

根据 request.CommitVersion 选择提交事务或者回滚事务

## 参考

[2021 Talent Plan KV 学习营结营总结 - 谭新宇的博客 (tanxinyu.work)](https://tanxinyu.work/tinykv/)

[TinyKV | lab3+lab4 - 知乎 (zhihu.com)](https://zhuanlan.zhihu.com/p/466627300)

[Talent plan Tinykv | project3 | Multi-raft KV (charstal.com)](https://www.charstal.com/talent-plan-tinykv-project3/)

[TinyKV 实验小结 - 知乎 (zhihu.com)](https://zhuanlan.zhihu.com/p/399720065)

[TinyKV 实现笔记 - リン屋 (rinchannow.top)](http://blog.rinchannow.top/tinykv-notes/#关于-Region-分裂)

[TinyKV-White-Paper/Project4-Transaction.md at main · Smith-Cruise/TinyKV-White-Paper (github.com)](https://github.com/Smith-Cruise/TinyKV-White-Paper/blob/main/Project4-Transaction.md)

