// Package settings stores application settings structs as JSON in the settings table. A struct is registered
// with a key by Register, loaded from the table on registration and on connect, and changed by Store.Update.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sync"

	"github.com/ordaen/orgo/pg"
)

// BeforeUpdateHook is implemented by settings validating their new values. It is called by Store.Update on a copy
// of the settings with the new values, an error leaves the settings and the table unchanged.
type BeforeUpdateHook interface {
	BeforeUpdate() error
}

// Register registers the settings e, a pointer to a struct, with the key and loads them from the table.
// When the database is not connected yet, they are loaded on connect. The fields missing in the stored data
// keep the values of e.
func Register[T any](key string, e T) error {
	v := reflect.ValueOf(e)
	if k := v.Kind(); k != reflect.Pointer {
		return fmt.Errorf("SETTINGS REGISTER [%s]: expect pointer to data structure got '%v'", key, k)
	}
	if k := v.Elem().Kind(); k != reflect.Struct {
		return fmt.Errorf("SETTINGS REGISTER [%s]: expect pointer to data structure got pointer to '%v'", key, k)
	}
	Settings.Lock()
	if prev, ok := Settings.types[key]; ok {
		Settings.Unlock()
		return fmt.Errorf("SETTINGS REGISTER [%s]: duplicate key previously assigned to '%T'", key, prev)
	}
	Settings.types[key] = e
	Settings.Unlock()

	if err := Settings.load(key, e); err != nil && !errors.Is(err, pg.ErrNotConnected) {
		return err
	}
	return nil
}

// Get returns a copy of the settings registered with the key as *T, or false when there are none of that type.
// Unlike reading the registered struct, it is safe while the settings are updated.
func Get[T any](key string) (T, bool) {
	Settings.RLock()
	defer Settings.RUnlock()
	if v, ok := Settings.types[key].(*T); ok {
		// a shallow copy is safe, Update replaces the slices and maps of the settings instead of changing them
		return *v, true
	}
	var zero T
	return zero, false
}

// Settings is the store of the registered settings, loaded on every connect.
var Settings = pg.RegisterRepository(&Store{types: make(map[string]any)})

// Store holds the registered settings. Update changes the registered structs with the lock held,
// so the readers of a registered struct hold the read lock while they read it, or use Get.
type Store struct {
	sync.RWMutex
	// writeMu serializes the writes of the settings to the table
	writeMu sync.Mutex
	types   map[string]any
}

func (s *Store) get(key string) any {
	s.RLock()
	defer s.RUnlock()
	return s.types[key]
}

// Setup loads all registered settings from the table, it is called on connect.
func (s *Store) Setup() error {
	s.RLock()
	types := maps.Clone(s.types)
	s.RUnlock()
	var errs []error
	for key, e := range types {
		errs = append(errs, s.load(key, e))
	}
	return errors.Join(errs...)
}

// load sets the settings e of the key to their stored data, when they are stored.
func (s *Store) load(key string, e any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rec, err := findByKey(key)
	if errors.Is(err, pg.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("SETTINGS LOAD [%s]: %w", key, err)
	}
	next, err := s.decode(e, rec.Data)
	if err != nil {
		return fmt.Errorf("SETTINGS LOAD [%s]: %w", key, err)
	}
	s.assign(e, next)
	return nil
}

// Clear deletes all stored settings and the registrations.
func (s *Store) Clear() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := pg.Exec("DELETE FROM settings"); err != nil {
		return err
	}
	s.Lock()
	s.types = make(map[string]any)
	s.Unlock()
	return nil
}

// Update sets the settings of the key to the JSON data and stores them. The fields missing in data keep their values.
// The settings are changed only when BeforeUpdate and the write succeed.
func (s *Store) Update(key string, data json.RawMessage) error {
	e := s.get(key)
	if e == nil {
		return fmt.Errorf("SETTINGS UPDATE [%s]: key not registered", key)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	next, err := s.decode(e, data)
	if err != nil {
		return fmt.Errorf("SETTINGS UPDATE [%s]: %w", key, err)
	}
	if v, ok := next.(BeforeUpdateHook); ok {
		if err := v.BeforeUpdate(); err != nil {
			return fmt.Errorf("SETTINGS UPDATE [%s]: %w", key, err)
		}
	}
	if err := s.store(key, next); err != nil {
		return fmt.Errorf("SETTINGS UPDATE [%s]: %w", key, err)
	}
	s.assign(e, next)
	return nil
}

// store writes the settings of the key to the table, s.writeMu must be held.
func (s *Store) store(key string, v any) error {
	r, err := newRecord(key, v)
	if err != nil {
		return err
	}
	rec, err := findByKey(key)
	switch {
	case errors.Is(err, pg.ErrRecordNotFound):
		_, err = pg.CreateModel(r)
	case err == nil:
		rec.Data = r.Data
		_, err = pg.UpdateModel(rec, "data")
	}
	return err
}

// decode returns a new copy of the settings e with the JSON data. The JSON fields of the copy do not share memory
// with e, so changing them, like decoding into a slice does, does not change e.
func (s *Store) decode(e any, data []byte) (any, error) {
	s.RLock()
	current, err := json.Marshal(e)
	next := reflect.New(reflect.TypeOf(e).Elem())
	// the fields not in JSON are copied as they are
	next.Elem().Set(reflect.ValueOf(e).Elem())
	s.RUnlock()
	if err != nil {
		return nil, err
	}

	fresh := reflect.New(next.Type().Elem())
	if err := json.Unmarshal(current, fresh.Interface()); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, fresh.Interface()); err != nil {
		return nil, err
	}
	copyJSONFields(next.Elem(), fresh.Elem())
	return next.Interface(), nil
}

// assign sets the registered settings e to next.
func (s *Store) assign(e, next any) {
	s.Lock()
	defer s.Unlock()
	reflect.ValueOf(e).Elem().Set(reflect.ValueOf(next).Elem())
}

// copyJSONFields sets the fields of the struct dst encoded in JSON to the fields of src.
func copyJSONFields(dst, src reflect.Value) {
	t := dst.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Tag.Get("json") == "-" {
			continue
		}
		// the fields of an embedded struct without a JSON name are encoded as the fields of the outer struct
		if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("json") == "" {
			copyJSONFields(dst.Field(i), src.Field(i))
			continue
		}
		if f.IsExported() {
			dst.Field(i).Set(src.Field(i))
		}
	}
}

// GetData returns the settings registered with the key, or nil
func (s *Store) GetData(key string) any {
	return s.get(key)
}

// GetDatas returns a copy of the map of the registered settings by key
func (s *Store) GetDatas() map[string]any {
	s.RLock()
	defer s.RUnlock()
	return maps.Clone(s.types)
}
