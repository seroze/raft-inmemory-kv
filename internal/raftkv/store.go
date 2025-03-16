package raftkv

import "fmt"

type Store struct {
	//stores map of []byte to []byte
	myMap map[string][]byte
}

func NewStore() *Store {
	return &Store{
		myMap: make(map[string][]byte),
	}
}

func (store *Store) Get(key string) ([]byte, error) {
	// check if key exists in map
	val, exists := store.myMap[key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}
	return val, nil
}

func (store *Store) Set(key string, val []byte) {
	store.myMap[key] = val
}

func (store *Store) Delete(key string) {
	delete(store.myMap, key)
}
