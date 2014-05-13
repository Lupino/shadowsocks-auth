package main

import (
    "encoding/json"
    "github.com/garyburd/redigo/redis"
)

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
    data, err = redis.Bytes(conn.Do("GET", key))
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
    _, err = conn.Do("SET", key, data)
    return
}

func (s *Storage) IncrSize(key string, incr int) (score int64, err error) {
    conn := s.pool.Get()
    score, err = redis.Int64(conn.Do("INCRBY", key, incr))
    return
}

func (s *Storage) GetSize(key string) (score int64, err error) {
    conn := s.pool.Get()
    score, err = redis.Int64(conn.Do("GET", key))
    return
}
