package main

import (
    "encoding/binary"
    "errors"
    "flag"
    "fmt"
    ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
    "io"
    "log"
    "net"
    "os"
    "os/signal"
    "runtime"
    "strconv"
    "syscall"
)

var debug ss.DebugLog

var storage *Storage

const dnsGoroutineNum = 64

func getUser(conn net.Conn) (user User, err error) {
    const (
        idVersion = 0 // \x01
        version   = 1
        idUser    = 1 // \x02
    )
    buf := make([]byte, 2)
    if _, err = io.ReadFull(conn, buf); err != nil {
        return
    }
    switch buf[idVersion] {
    case version:
        break
    default:
        err = errors.New(fmt.Sprintf("version %s not supported", buf[idVersion]))
        return
    }
    userLen := buf[idUser]
    username := make([]byte, userLen)
    if _, err = io.ReadFull(conn, username); err != nil {
        return
    }
    user, err = storage.Get(string(username))
    return
}

func getRequest(conn *ss.Conn) (host string, extra []byte, err error) {
    const (
        idType  = 0 // address type index
        idIP0   = 1 // ip addres start index
        idDmLen = 1 // domain address length index
        idDm0   = 2 // domain address start index

        typeIPv4 = 1 // type is ipv4 address
        typeDm   = 3 // type is domain address
        typeIPv6 = 4 // type is ipv6 address

        lenIPv4   = 1 + net.IPv4len + 2 // 1addrType + ipv4 + 2port
        lenIPv6   = 1 + net.IPv6len + 2 // 1addrType + ipv6 + 2port
        lenDmBase = 1 + 1 + 2           // 1addrType + 1addrLen + 2port, plus addrLen
    )

    // buf size should at least have the same size with the largest possible
    // request size (when addrType is 3, domain name has at most 256 bytes)
    // 1(addrType) + 1(lenByte) + 256(max length address) + 2(port)
    buf := make([]byte, 260)
    var n int
    // read till we get possible domain length field
    ss.SetReadTimeout(conn)
    if n, err = io.ReadAtLeast(conn, buf, idDmLen+1); err != nil {
        return
    }

    reqLen := -1
    switch buf[idType] {
    case typeIPv4:
        reqLen = lenIPv4
    case typeIPv6:
        reqLen = lenIPv6
    case typeDm:
        reqLen = int(buf[idDmLen]) + lenDmBase
    default:
        err = errors.New(fmt.Sprintf("addr type %d not supported", buf[idType]))
        return
    }

    if n < reqLen { // rare case
        ss.SetReadTimeout(conn)
        if _, err = io.ReadFull(conn, buf[n:reqLen]); err != nil {
            return
        }
    } else if n > reqLen {
        // it's possible to read more than just the request head
        extra = buf[reqLen:n]
    }

    // Return string for typeIP is not most efficient, but browsers (Chrome,
    // Safari, Firefox) all seems using typeDm exclusively. So this is not a
    // big problem.
    switch buf[idType] {
    case typeIPv4:
        host = net.IP(buf[idIP0 : idIP0+net.IPv4len]).String()
    case typeIPv6:
        host = net.IP(buf[idIP0 : idIP0+net.IPv6len]).String()
    case typeDm:
        host = string(buf[idDm0 : idDm0+buf[idDmLen]])
    }
    // parse port
    port := binary.BigEndian.Uint16(buf[reqLen-2 : reqLen])
    host = net.JoinHostPort(host, strconv.Itoa(int(port)))
    return
}

const logCntDelta = 100

var connCnt int
var nextLogConnCnt int = logCntDelta

func handleConnection(conn *ss.Conn) {
    var host string

    connCnt++ // this maybe not accurate, but should be enough
    if connCnt-nextLogConnCnt >= 0 {
        // XXX There's no xadd in the atomic package, so it's difficult to log
        // the message only once with low cost. Also note nextLogConnCnt maybe
        // added twice for current peak connection number level.
        log.Printf("Number of client connections reaches %d\n", nextLogConnCnt)
        nextLogConnCnt += logCntDelta
    }

    // function arguments are always evaluated, so surround debug statement
    // with if statement
    if debug {
        debug.Printf("new client %s->%s\n", conn.RemoteAddr().String(), conn.LocalAddr())
    }
    closed := false
    defer func() {
        if debug {
            debug.Printf("closed pipe %s<->%s\n", conn.RemoteAddr(), host)
        }
        connCnt--
        if !closed {
            conn.Close()
        }
    }()

    host, extra, err := getRequest(conn)
    if err != nil {
        log.Println("error getting request", conn.RemoteAddr(), conn.LocalAddr(), err)
        return
    }
    debug.Println("connecting", host)
    remote, err := net.Dial("tcp", host)
    if err != nil {
        if ne, ok := err.(*net.OpError); ok && (ne.Err == syscall.EMFILE || ne.Err == syscall.ENFILE) {
            // log too many open file error
            // EMFILE is process reaches open file limits, ENFILE is system limit
            log.Println("dial error:", err)
        } else {
            log.Println("error connecting to:", host, err)
        }
        return
    }
    defer func() {
        if !closed {
            remote.Close()
        }
    }()
    // write extra bytes read from
    if extra != nil {
        // debug.Println("getRequest read extra data, writing to remote, len", len(extra))
        if _, err = remote.Write(extra); err != nil {
            debug.Println("write request extra error:", err)
            return
        }
    }
    if debug {
        debug.Printf("piping %s<->%s", conn.RemoteAddr(), host)
    }
    go ss.PipeThenClose(conn, remote, ss.SET_TIMEOUT)
    ss.PipeThenClose(remote, conn, ss.NO_TIMEOUT)
    closed = true
    return
}

func waitSignal() {
    var sigChan = make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGHUP)
    for sig := range sigChan {
        if sig == syscall.SIGHUP {
        } else {
            // is this going to happen?
            log.Printf("caught signal %v, exit", sig)
            os.Exit(0)
        }
    }
}

func run(port string) {
    ln, err := net.Listen("tcp", ":"+port)
    if err != nil {
        log.Printf("error listening port %v: %v\n", port, err)
        return
    }
    var cipher *ss.Cipher
    log.Printf("server listening port %v ...\n", port)
    for {
        conn, err := ln.Accept()
        if err != nil {
            debug.Printf("accept error: %v\n", err)
            return
        }

        user, err := getUser(conn)
        if err != nil {
            log.Printf("Error get passeoed: %s %v\n", port, err)
            conn.Close()
            continue
        }
        log.Println("creating cipher for user:", user.Name)
        cipher, err = ss.NewCipher(user.Method, user.Password)
        if err != nil {
            log.Printf("Error generating cipher for user: %s %v\n", user.Name, err)
            conn.Close()
            continue
        }
        go handleConnection(ss.NewConn(conn, cipher.Copy()))
    }
}

func main() {
    log.SetOutput(os.Stdout)

    var printVer bool
    var core int
    var serverPort string
    var redisServer string

    flag.BoolVar(&printVer, "version", false, "print version")
    flag.StringVar(&serverPort, "p", "8388", "server port")
    flag.StringVar(&redisServer, "redis", ":6379", "redis server")
    flag.IntVar(&core, "core", 0, "maximum number of CPU cores to use, default is determinied by Go runtime")
    flag.BoolVar((*bool)(&debug), "d", false, "print debug message")

    flag.Parse()

    if printVer {
        ss.PrintVersion()
        os.Exit(0)
    }

    ss.SetDebug(debug)

    storage = NewStorage(redisServer)
    if core > 0 {
        runtime.GOMAXPROCS(core)
    }

    go run(serverPort)

    waitSignal()
}
