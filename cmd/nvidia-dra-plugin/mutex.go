package main

import (
	"sync"
)

type PerGPUMutex struct {
	sync.Mutex
	submutex map[string]*sync.Mutex
}

var perGpuLock *PerGPUMutex

func init() {
	perGpuLock = &PerGPUMutex{
		submutex: make(map[string]*sync.Mutex),
	}
}

func (pgm *PerGPUMutex) Get(gpu string) *sync.Mutex {
	pgm.Mutex.Lock()
	defer pgm.Mutex.Unlock()
	if pgm.submutex[gpu] == nil {
		pgm.submutex[gpu] = &sync.Mutex{}
	}
	return pgm.submutex[gpu]
}
