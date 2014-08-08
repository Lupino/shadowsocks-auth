带有用户和流量统计的 shadowsocks
================================

当提供 shadowsocks 服务时，可以通过一个端口和用户的模式，服务于多个用户，而不必启用多个服务。
流量统计可以实时的查看用户的流量，并带有流量限制。

配套的 gui 客户端 <https://github.com/Lupino/shadowsocks-gui>


build
-----

    git clone https://github.com/Lupino/shadowsocks-auth.git
    cd shadowsocks-auth
    make


server
-----

    $(GOPATH)/bin/server -p 8388 -redis=127.0.0.1:6379

    redis-cli
    redis 127.0.0.1:6379> set ss:user:lupino "{\"name\": \"lupino\", \"password\": \"lup12345\", \"method\": \"aes-256-cfb\", \"limit\": 10737418240}"
    redis 127.0.0.1:6379> keys "*"
     1) "ss:flow:lupino:2014:7:31"
     2) "ss:flow:lupino:2014:7:29"
     3) "ss:user:lupino"
     4) "ss:flow:lupino"

    # redis keys 说明
    ss:flow:[username] sorted set 用户当前的总流量
    ss:flow:[username]:year:month:day sorted set 用户当天的流量
    ss:user:[username] json 用户的信息
    @name: 用户名
    @password: 密码
    @method: 加密方式
    @limit: 最大限制流量

server 端使用脚本 <https://gist.github.com/Lupino/d0609ab79d873b2c1015>

local
-----

修改 config.json

    {
        "server":"23.226.79.100",
        "server_port":8388,
        "local_port":1080,
        "password":"lupino:lup12345",  # 用户名:密码
        "method": "aes-256-cfb",
        "timeout": 600
    }


    $(GOPATH)/bin/local -c config.json
