package main

import (
    "encoding/json"
    "github.com/garyburd/redigo/redis"
)

const SS_PREFIX = "ss:"

type User struct {
    Name     string `json:"name"`
    Password string `json:"password"`
    Method   string `json:"method"`
    Limit    int64  `json:"limit"`
}

type Storage struct {
    pool *redis.Pool
}

func NewStorage(server string) *Storage {
    pool := redis.NewPool(func() (conn redis.Conn, err error) {
        conn, err = redis.Dial("tcp", server)
        return
    }, 3)
    return &Storage{pool}
}

func (s *Storage) Get(key string) (user User, err error) {
    var data []byte
    var conn = s.pool.Get()
    defer conn.Close()
    data, err = redis.Bytes(conn.Do("GET", SS_PREFIX + key))
    if err != nil {
        return
    }
    err = json.Unmarshal(data, &user)
    return
}

func (s *Storage) Set(key string, user User) (err error) {
    data, err := json.Marshal(user)
    if err != nil {
        return err
    }
    conn := s.pool.Get()
    defer conn.Close()
    _, err = conn.Do("SET", SS_PREFIX + key, data)
    return
}

func (s *Storage) IncrSize(key string, incr int) (score int64, err error) {
    var conn = s.pool.Get()
    defer conn.Close()
    score, err = redis.Int64(conn.Do("INCRBY", SS_PREFIX + key, incr))
    return
}

func (s *Storage) GetSize(key string) (score int64, err error) {
    var conn = s.pool.Get()
    defer conn.Close()
    score, err = redis.Int64(conn.Do("GET", SS_PREFIX + key))
    return
}
