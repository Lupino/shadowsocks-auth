package main

import (
    "encoding/json"
    "github.com/garyburd/redigo/redis"
    "time"
    "fmt"
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

func (s *Storage) ZincrbySize(key, member string, incr int) (err error) {
    var conn = s.pool.Get()
    defer conn.Close()
    var score int64
    var year, month, day int
    var real_key string

    now := time.Now()
    year = now.Year()
    month = int(now.Month())
    day = now.Day()

    // store year
    real_key = fmt.Sprintf("%s%s:%d", SS_PREFIX, key, year)
    score, err = redis.Int64(conn.Do("ZINCRBY", real_key, member, incr))
    if score < 0 || err != nil {
        conn.Do("ZADD", real_key, incr, member)
    }
    // store year:month
    real_key = fmt.Sprintf("%s%s:%d:%d", SS_PREFIX, key, year, month)
    score, err = redis.Int64(conn.Do("ZINCRBY", real_key, member, incr))
    if score < 0 || err != nil {
        conn.Do("ZADD", real_key, incr, member)
    }
    // store year:month:day
    real_key = fmt.Sprintf("%s%s:%d:%d:%d", SS_PREFIX, key, year, month, day)
    score, err = redis.Int64(conn.Do("ZINCRBY", real_key, member, incr))
    if score < 0 || err != nil {
        conn.Do("ZADD", real_key, incr, member)
    }
    return
}
