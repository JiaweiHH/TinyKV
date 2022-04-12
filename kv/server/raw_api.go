package server

import (
	"context"
	"github.com/pingcap-incubator/tinykv/kv/storage"
	"github.com/pingcap-incubator/tinykv/proto/pkg/kvrpcpb"
)

// The functions below are Server's Raw API. (implements TinyKvServer).
// Some helper methods can be found in sever.go in the current directory

// RawGet return the corresponding Get response based on RawGetRequest's CF and Key fields
func (server *Server) RawGet(_ context.Context, req *kvrpcpb.RawGetRequest) (*kvrpcpb.RawGetResponse, error) {
	// Your Code Here (1).
	reader, _ := server.storage.Reader(req.Context)
	defer reader.Close()
	val, _ := reader.GetCF(req.Cf, req.Key)
	resp := &kvrpcpb.RawGetResponse{}
	resp.Value = val

	if len(val) == 0 {
		resp.NotFound = true
	}

	return resp, nil
}

// RawPut puts the target data into storage and returns the corresponding response
func (server *Server) RawPut(_ context.Context, req *kvrpcpb.RawPutRequest) (*kvrpcpb.RawPutResponse, error) {
	// Your Code Here (1).
	// Hint: Consider using Storage.Modify to store data to be modified
	batch := make([]storage.Modify, 0)
	batch = append(batch, storage.Modify{
		Data: storage.Put{
			Key: req.Key,
			Value: req.Value,
			Cf: req.Cf,
		},
	})
	_ = server.storage.Write(req.Context, batch)
	resp := &kvrpcpb.RawPutResponse{}
	return resp, nil
}

// RawDelete delete the target data from storage and returns the corresponding response
func (server *Server) RawDelete(_ context.Context, req *kvrpcpb.RawDeleteRequest) (*kvrpcpb.RawDeleteResponse, error) {
	// Your Code Here (1).
	// Hint: Consider using Storage.Modify to store data to be deleted
	batch := make([]storage.Modify, 0)
	batch = append(batch, storage.Modify{
		Data: storage.Delete{
			Key: req.Key,
			Cf: req.Cf,
		},
	})
	_ = server.storage.Write(req.Context, batch)
	resp := &kvrpcpb.RawDeleteResponse{}
	return resp, nil
}

// RawScan scan the data starting from the start key up to limit. and return the corresponding result
func (server *Server) RawScan(_ context.Context, req *kvrpcpb.RawScanRequest) (*kvrpcpb.RawScanResponse, error) {
	// Your Code Here (1).
	// Hint: Consider using reader.IterCF
	reader, _ := server.storage.Reader(req.Context)
	defer reader.Close()
	iter := reader.IterCF(req.Cf)
	defer iter.Close()
	response := &kvrpcpb.RawScanResponse{
		Kvs: make([]*kvrpcpb.KvPair, 0),
	}
	var cnt uint32
	for iter.Seek(req.StartKey); iter.Valid(); iter.Next() {
		item := iter.Item()
		k := item.Key()
		v, _ := item.Value()
		response.Kvs = append(response.Kvs, &kvrpcpb.KvPair{Key: k, Value: v})
		cnt++
		if cnt >= req.GetLimit() {
			break
		}
	}
	return response, nil
}
