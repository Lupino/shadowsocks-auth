package main

import (
    "github.com/garyburd/redigo/redis"
    "encoding/json"
)

type User struct {
    Name string `json:"name"`
    Password string `json:"password"`
    Method  string `json:"method"`
}

type Storage struct {
    conn redis.Conn
}

func NewStorage(server string) (*Storage, error){
    conn, err := redis.Dial("tcp", server)
    if err != nil {
        return nil, err
    }
    return &Storage{conn}, nil
}

func (s *Storage) Get(key string) (user User, err error) {
    var data []byte
    data, err = redis.Bytes(s.conn.Do("GET", key))
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
    _, err = s.conn.Do("SET", key, data)
    return
}
