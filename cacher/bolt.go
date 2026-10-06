package cacher

import (
	"errors"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// ErrNotFound is returned when a key is not in the storage, or its cache entry expired.
var ErrNotFound = errors.New("cacher: not found")

// DB is a bbolt storage of values in buckets. It is safe for concurrent use.
type DB struct {
	db *bbolt.DB
}

// OpenDB opens the bbolt database file, creating it when it does not exist.
func OpenDB(file string) (*DB, error) {
	db, err := bbolt.Open(file, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("cacher: open %s: %w", file, err)
	}
	return &DB{db: db}, nil
}

// Close closes the database.
func (db *DB) Close() error {
	return db.db.Close()
}

// Get returns a copy of the value of the key in the bucket, or ErrNotFound.
func (db *DB) Get(bucket, key string) (out []byte, err error) {
	err = db.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return ErrNotFound
		}
		v := b.Get([]byte(key))
		if v == nil {
			return ErrNotFound
		}
		// the value memory is owned by bbolt and valid only during the transaction
		out = append([]byte{}, v...)
		return nil
	})
	return
}

// Set writes the value of the key in the bucket, creating the bucket when it does not exist.
func (db *DB) Set(bucket, key string, data []byte) error {
	return db.db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return fmt.Errorf("cacher: create bucket: %w", err)
		}
		return b.Put([]byte(key), data)
	})
}

// DeleteBucket deletes the bucket with all its keys. A missing bucket is not an error.
func (db *DB) DeleteBucket(bucket string) error {
	return db.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.DeleteBucket([]byte(bucket)); err != nil && !errors.Is(err, bolterrors.ErrBucketNotFound) {
			return err
		}
		return nil
	})
}

// DeleteKey deletes the key in the bucket. A missing bucket or key is not an error.
func (db *DB) DeleteKey(bucket, key string) error {
	return db.db.Update(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte(bucket)); b != nil {
			return b.Delete([]byte(key))
		}
		return nil
	})
}

// DeleteFunc deletes the keys of all buckets, except the skipped one, for which del returns true.
// The value passed to del is valid only during the call.
func (db *DB) DeleteFunc(skip string, del func(value []byte) bool) error {
	return db.db.Update(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			if string(name) == skip {
				return nil
			}
			// the keys are collected first, a cursor skips the key after a deleted one
			var keys [][]byte
			err := b.ForEach(func(k, v []byte) error {
				if v != nil && del(v) {
					keys = append(keys, append([]byte{}, k...))
				}
				return nil
			})
			if err != nil {
				return err
			}
			for _, k := range keys {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// deleteBucketsExcept deletes all buckets except the kept one.
func (db *DB) deleteBucketsExcept(keep string) error {
	return db.db.Update(func(tx *bbolt.Tx) error {
		var names [][]byte
		err := tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
			if string(name) != keep {
				names = append(names, append([]byte{}, name...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := tx.DeleteBucket(name); err != nil {
				return err
			}
		}
		return nil
	})
}
