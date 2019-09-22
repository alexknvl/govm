package database

import (
	"bytes"
	"fmt"
	"github.com/boltdb/bolt"
	"log"
	"os"
	"path"
	"sync"
)

type table map[string][]byte

// LDB Local DB
type LDB struct {
	wmu     sync.Mutex
	cmu     sync.Mutex
	ldb     *bolt.DB
	cacheTB map[string]bool
	lru     *LRUCache
	rdisk   int
	wdisk   int
}

// NewLDB new local db
func NewLDB(name string, cacheCap int) *LDB {
	os.Mkdir(gDbRoot, 0600)
	out := LDB{}
	out.cacheTB = make(map[string]bool)
	out.lru = NewLRUCache(cacheCap)
	var err error
	fn := path.Join(gDbRoot, name)
	out.ldb, err = bolt.Open(fn, 0600, nil)
	if err != nil {
		log.Println("fail to open file(ldb):", fn, err)
		return nil
	}
	return &out
}

// Close close ldb
func (d *LDB) Close() {
	d.ldb.Close()
}

// SetCache set memory cache of the table
func (d *LDB) SetCache(tbName string) {
	d.cmu.Lock()
	defer d.cmu.Unlock()
	d.cacheTB[tbName] = true
}

// LGet Local DB Get
func (d *LDB) LGet(chain uint64, tbName string, key []byte) []byte {
	var out []byte
	var save bool
	tn := fmt.Sprintf("%d:%s", chain, tbName)
	d.cmu.Lock()
	if d.cacheTB[tbName] {
		save = true
		k := fmt.Sprintf("%s:%x", tn, key)
		v, ok := d.lru.Get(k)
		if ok {
			d.cmu.Unlock()
			return v.([]byte)
		}
	}
	d.cmu.Unlock()

	d.ldb.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(tn))
		if b == nil {
			return nil
		}
		v := b.Get(key)
		if v != nil {
			out = make([]byte, len(v))
			copy(out, v)
		}
		return nil
	})
	d.rdisk++
	if save {
		d.cmu.Lock()
		k := fmt.Sprintf("%s:%x", tn, key)
		d.lru.Set(k, out)
		d.cmu.Unlock()
	}
	return out
}

// LSet Local DB Set,if value = nil,will delete the key
func (d *LDB) LSet(chain uint64, tbName string, key, value []byte) error {
	tn := fmt.Sprintf("%d:%s", chain, tbName)
	var same bool
	d.cmu.Lock()
	if d.cacheTB[tbName] {
		var oldV []byte
		k := fmt.Sprintf("%s:%x", tn, key)
		v, ok := d.lru.Get(k)
		if ok {
			oldV = v.([]byte)
		}
		if ok && bytes.Compare(oldV, value) == 0 {
			same = true
		} else {
			d.lru.Set(k, value)
		}
	}
	d.cmu.Unlock()
	if same {
		return nil
	}
	d.wmu.Lock()
	defer d.wmu.Unlock()
	d.ldb.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(tn))
		if err != nil {
			log.Printf("fail to create bucket,%x\n", tbName)
			return err
		}
		if value == nil {
			b.Delete(key)
		} else {
			err = b.Put(key, value)
			if err != nil {
				log.Println("fail to put:", err)
				return err
			}
		}
		return nil
	})
	d.wdisk++
	return nil
}
