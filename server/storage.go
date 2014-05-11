package main

import (
    "encoding/json"
    "github.com/garyburd/redigo/redis"
)

type User struct {
    Name     string `json:"name"`
    Password string `json:"password"`
    Method   string `json:"method"`
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
