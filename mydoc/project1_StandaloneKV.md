# Project1 StandaloneKV

实验一要求我们实现一个单机存储引擎 `StoragealoneStorage`，其底层基于 badger 并支持 Column Family，然后需要基于我们自己编写的 `StoragealoneStorage` 实现上层服务，提供 `RawGet, RawScan, RawPut, RawDelete` 四种操作

### 什么是 Column Family

key-value 可以认为是一个巨大的 map，key-value 都按照字节存储，其中 key 按照比特位顺序排列，而 Column Family 可以认为是一个 namespace 或者类似于关系数据库的 table 一样，将数据进行了分类。同一个 key 在不同的 Column Family 下就可以索引到不同的 value，类似关系数据库中对于一个主键的查找它的其他属性一样

| key      | name      | age  | addr     |
| -------- | --------- | ---- | -------- |
| temp_key | temp_name | 18   | Hangzhou |

（关系数据库中的一行）

| key      | value     |
| -------- | --------- |
| name_key | temp_name |
| age_key  | 18        |
| addr_key | Hangzhou  |

（我们可以通过添加前缀的方式来实现 CF）

### Badger 的 LSM-Tree

badger 在 LSM-Tree 中只存放 <key, value_addr>，value 单独存放在 value log 中，因此 MemTable 可以容纳大量的 key，而 value log 中的数据都是直接 append 的、不需要和 LSM 一样实现排序

### txn 和 engine_util

txn 是一个 badger 的事务，对 badger 的操作都可以借助 txn 来实现例如 Get、Set。而我们不需要自己去执行这些操作，官方给我们提供了 API 封装在 engine_util 中，只需要创建 txn 然后调用 engine_util 的接口就可以了

Txn：[badger package - github.com/dgraph-io/badger - pkg.go.dev](https://pkg.go.dev/github.com/dgraph-io/badger#Txn)

badger API 的使用：[Connor1996/badger: Fast key-value DB in Go. (github.com)](https://github.com/Connor1996/badger)

### Storage Engine

我们需要完成存储引擎的创建（`NewStandaloneStorage`）、结束（`Stop`）、以及读写操作（`Reader`、`Write`）。其中存储引擎的创建实际上就是创建一个 badger 实例，需要的创建参数从 conf 里面获取就可以了，Stop 操作只需要关闭 badger 即可

```go
type StandAloneStorage struct {
	// Your Data Here (1).
	engines *engine_util.Engines
	config  *config.Config
}
```

Reader 方法需要我们返回 `storage.StorageReader`，我们需要自己实现一个 StandAloneReader 并实现 `StorageReader` 的接口，包括 GetCF、IterCF 以及 Close。StandAloneReader 中只需要包含 txn 即可，上层服务执行只读操作的时候需要借助 txn 实现

- GetCF 直接调用 engine_util 的接口就可以
- IterCF 调用 engine_util 接口创建一个迭代器
- Close 关闭一个事务

Write 方法也很简单，只需要遍历 batch，然后获取其中的 kv 并通过 engine_util 写入到 badger 即可。batch 中的每个元素都是一个 Put 或者 Delete 类型的对象

### Service Handlers

这里需要我们实现 RawGet、RawScan、RawPut、RawDelete，前面两个需要借助 storage.StorageReader 来完成，因为前面一部分我们已经实现了 GetCF 和 IterCF 接口，刚好对应了 RawGet 和 RawScan 这两个操作。RawScan 需要注意 req.Limits 给出了返回 kv 数量的最大值

RawPut 和 RawDelete 需要我们跟去 Request 中的数据创建 Modify 对象，然后将 Modify 对象传递给存储引擎完成 Write 操作