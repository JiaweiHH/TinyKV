package standalone_storage

import (
	"github.com/Connor1996/badger"
	"os"
	"path/filepath"

	"github.com/pingcap-incubator/tinykv/kv/config"
	"github.com/pingcap-incubator/tinykv/kv/storage"
	"github.com/pingcap-incubator/tinykv/kv/util/engine_util"
	"github.com/pingcap-incubator/tinykv/proto/pkg/kvrpcpb"
)

// StandAloneStorage is an implementation of `Storage` for a single-node TinyKV instance. It does not
// communicate with other nodes and all data is stored locally.
type StandAloneStorage struct {
	// Your Data Here (1).
	engines *engine_util.Engines
	config  *config.Config
}

type StandAloneReader struct {
	txn *badger.Txn
}

func NewStandAloneReader(txn *badger.Txn) *StandAloneReader {
	return &StandAloneReader{txn: txn}
}

func (s *StandAloneReader) GetCF(cf string, key []byte) ([]byte, error) {
	val, _ := engine_util.GetCFFromTxn(s.txn, cf, key)
	return val, nil
}

func (s *StandAloneReader) IterCF(cf string) engine_util.DBIterator {
	iter := engine_util.NewCFIterator(cf, s.txn)
	return iter
}

func (s *StandAloneReader) Close() {
	s.txn.Discard()
}

// NewStandAloneStorage 创建 badger 数据库
func NewStandAloneStorage(conf *config.Config) *StandAloneStorage {
	// Your Code Here (1).
	dbPath := conf.DBPath
	kvPath := filepath.Join(dbPath, "kv")
	os.MkdirAll(kvPath, os.ModePerm)
	kvDB := engine_util.CreateDB(kvPath, false)
	engines := engine_util.NewEngines(kvDB, nil, kvPath, "")
	return &StandAloneStorage{engines: engines, config: conf}
}

func (s *StandAloneStorage) Start() error {
	// Your Code Here (1).
	// 不需要修改，NewStandAloneStorage 已经 open badger 了
	return nil
}

// Stop 关闭数据库
func (s *StandAloneStorage) Stop() error {
	// Your Code Here (1).
	s.engines.Kv.Close()
	return nil
}

func (s *StandAloneStorage) Reader(ctx *kvrpcpb.Context) (storage.StorageReader, error) {
	// Your Code Here (1).
	// 创建一个事务，封装到 StandAloneReader 中
	db := s.engines.Kv
	txn := db.NewTransaction(false)
	return NewStandAloneReader(txn), nil
}

func (s *StandAloneStorage) Write(ctx *kvrpcpb.Context, batch []storage.Modify) error {
	// Your Code Here (1).
	db := s.engines.Kv
	for _, m := range batch {
		switch m.Data.(type) {
		case storage.Put:
			engine_util.PutCF(db, m.Cf(), m.Key(), m.Value())
		case storage.Delete:
			engine_util.DeleteCF(db, m.Cf(), m.Key())
		}
	}
	return nil
}
