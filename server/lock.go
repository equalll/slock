package server

import (
    "sync"
    "github.com/snower/slock/protocol"
)

type LockManager struct {
    lock_db        *LockDB
    lock_key       [2]uint64
    current_lock   *Lock
    locks          *LockQueue
    lock_maps      map[[2]uint64]*Lock
    wait_locks     *LockQueue
    glock          *sync.Mutex
    free_locks     *LockQueue
    locked         uint16
    db_id          uint8
    waited         bool
    freed          bool
    glock_index    int8
}

func NewLockManager(lock_db *LockDB, command *protocol.LockCommand, glock *sync.Mutex, glock_index int8, free_locks *LockQueue) *LockManager {
    return &LockManager{lock_db, command.LockKey,
    nil, nil, nil, nil, glock, free_locks, 0, command.DbId, false, true, glock_index}
}

func (self *LockManager) GetDB() *LockDB{
    return self.lock_db
}

func (self *LockManager) AddLock(lock *Lock) *Lock {
    lock.expried_time = self.lock_db.current_time + int64(lock.command.Expried)
    lock.locked = true
    lock.ref_count++

    if self.current_lock == nil {
        self.current_lock = lock
        return lock
    }

    self.locks.Push(lock)
    self.lock_maps[lock.command.LockId] = lock
    return lock
}

func (self *LockManager) RemoveLock(lock *Lock) *Lock {
    lock.locked = false

    if self.current_lock == lock {
        self.current_lock = nil
        lock.ref_count--
        if lock.ref_count == 0 {
            self.FreeLock(lock)
        }

        locked_lock := self.locks.Pop()
        for ; locked_lock != nil; {
            if locked_lock.locked {
                _, ok := self.lock_maps[locked_lock.command.LockId]
                if ok {
                    delete(self.lock_maps, locked_lock.command.LockId)
                }
                self.current_lock = locked_lock
                return lock
            }

            locked_lock.ref_count--
            if locked_lock.ref_count == 0 {
                self.FreeLock(locked_lock)
            }

            locked_lock = self.locks.Pop()
        }

        return lock
    }

    _, ok := self.lock_maps[lock.command.LockId]
    if ok {
        delete(self.lock_maps, lock.command.LockId)
    }

    locked_lock := self.locks.Head()
    for ; locked_lock != nil; {
        if locked_lock.locked {
            break
        }

        self.locks.Pop()
        locked_lock.ref_count--
        if locked_lock.ref_count == 0 {
            self.FreeLock(locked_lock)
        }

        locked_lock = self.locks.Head()
    }
    return lock
}

func (self *LockManager) GetLockedLock(command *protocol.LockCommand) *Lock {
    if self.current_lock.command.LockId == command.LockId {
        return self.current_lock
    }

    locked_lock, ok := self.lock_maps[command.LockId]
    if ok {
        return locked_lock
    }
    return nil
}

func (self *LockManager) UpdateLockedLock(lock *Lock, timeout uint32, expried uint32, count uint16) error {
    lock.command.Timeout = timeout
    lock.command.Expried = expried
    lock.command.Count = count
    lock.timeout_time = self.lock_db.current_time + int64(timeout)
    lock.expried_time = self.lock_db.current_time + int64(expried)
    return nil;
}

func (self *LockManager) AddWaitLock(lock *Lock) *Lock {
    self.wait_locks.Push(lock)
    lock.ref_count++
    self.waited = true
    return lock
}

func (self *LockManager) GetWaitLock() *Lock {
    lock := self.wait_locks.Head()
    for ; lock != nil; {
        if lock.timeouted {
            self.wait_locks.Pop()
            lock.ref_count--
            if lock.ref_count == 0{
                self.FreeLock(lock)
            }

            lock = self.wait_locks.Head()
        }
        return lock
    }
    return nil
}

func (self *LockManager) FreeLock(lock *Lock) *Lock{
    lock.manager = nil
    lock.protocol = nil
    lock.command = nil
    self.free_locks.Push(lock)
    return lock
}

func (self *LockManager) GetOrNewLock(protocol *ServerProtocol, command *protocol.LockCommand) *Lock {
    lock := self.free_locks.PopRight()
    if lock == nil {
        locks := make([]Lock, 4096)
        lock = &locks[0]

        for i := 1; i < 4096; i++ {
            self.free_locks.Push(&locks[i])
        }
    }

    now := self.lock_db.current_time

    lock.manager = self
    lock.command = command
    lock.protocol = protocol
    lock.start_time = now
    lock.expried_time = 0
    lock.timeout_time = now + int64(command.Timeout)
    lock.timeout_checked_count = 2
    lock.expried_checked_count = 2
    lock.ref_count = 0
    return lock
}

type Lock struct {
    manager             *LockManager
    command             *protocol.LockCommand
    protocol            *ServerProtocol
    start_time          int64
    expried_time        int64
    timeout_time        int64
    timeout_checked_count int64
    expried_checked_count int64
    locked              bool
    timeouted           bool
    expried             bool
    ref_count           uint8
}

func NewLock(manager *LockManager, protocol *ServerProtocol, command *protocol.LockCommand) *Lock {
    now := manager.lock_db.current_time
    return &Lock{manager, command, protocol,now, 0, now + int64(command.Timeout), 0, 0,false, false, false, 0}
}

func (self *Lock) GetDB() *LockDB{
    if self.manager == nil {
        return nil
    }
    return self.manager.GetDB()
}