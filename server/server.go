package main

import (
    "bytes"
    "encoding/binary"
    "errors"
    "flag"
    "fmt"
    "github.com/cyfdecyf/leakybuf"
    ss "github.com/shadowsocks/shadowsocks-go/shadowsocks"
    "io"
    "log"
    "net"
    "os"
    "os/signal"
    "runtime"
    "strconv"
    "syscall"
    "bufio"
    "net/http"
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

func handleConnection(user User, conn *ss.Conn) {
    var host string
    var size = 0
    var raw_req_header, raw_res_header []byte
    var is_http = false
    var res_size = 0
    var req_chan = make(chan []byte)

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
        if is_http{
            tmp_req_header := <-req_chan
            buffer := bytes.NewBuffer(raw_req_header)
            buffer.Write(tmp_req_header)
            raw_req_header = buffer.Bytes()
        }
        showConn(raw_req_header, raw_res_header, host, user, size, is_http)
        close(req_chan)
        if !closed {
            remote.Close()
        }
    }()
    // write extra bytes read from

    is_http, extra, _ = checkHttp(extra, conn)
    raw_req_header = extra
    res_size, err = remote.Write(extra)
    storage.IncrSize("flow:" + user.Name, res_size)
    size += res_size
    if err != nil {
        debug.Println("write request extra error:", err)
        return
    }

    if debug {
        debug.Printf("piping %s<->%s", conn.RemoteAddr(), host)
    }

    go func() {
        _, raw_header := PipeThenClose(conn, remote, ss.SET_TIMEOUT, is_http, false, user)
        if is_http {
            req_chan<-raw_header
        }
    }()

    res_size, raw_res_header = PipeThenClose(remote, conn, ss.NO_TIMEOUT, is_http, true, user)
    size += res_size
    closed = true
    return
}


func showConn(raw_req_header, raw_res_header []byte, host string, user User, size int, is_http bool) {
    if is_http {
        req, _ := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw_req_header)))
        if req == nil {
            lines := bytes.SplitN(raw_req_header, []byte(" "), 2)
            fmt.Printf("%s http://%s/ \"Unknow\" HTTP/1.1 unknow %s %d\n", lines[0], host, user.Name, size)
            return
        }
        res, _ := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw_res_header)), req)
        statusCode := 200
        if res != nil {
            statusCode = res.StatusCode
        }
        fmt.Printf("%s http://%s%s \"%s\" %s %d %s %d\n", req.Method, req.Host, req.URL.String(), req.Header.Get("user-agent"), req.Proto, statusCode, user.Name, size)
    } else {
        fmt.Printf("CONNECT %s \"Unknow\" HTTPS unknow %s %d\n", host, user.Name, size)
    }
}

func checkHttp(extra []byte, conn *ss.Conn) (is_http bool, data []byte, err error) {
    var buf []byte
    var methods = []string{"GET", "HEAD", "POST", "PUT", "TRACE", "OPTIONS", "DELETE"}
    is_http = false
    if extra == nil || len(extra) < 10 {
        buf = make([]byte, 10)
        if _, err = io.ReadFull(conn, buf); err != nil {
            return
        }
    }

    if buf == nil {
        data = extra
    } else if extra == nil {
        data = buf
    }else {
        buffer := bytes.NewBuffer(extra)
        buffer.Write(buf)
        data = buffer.Bytes()
    }

    for _, method := range methods {
        if bytes.HasPrefix(data, []byte(method)) {
            is_http = true
            break
        }
    }
    return
}

const bufSize = 4096
const nBuf = 2048

func PipeThenClose(src, dst net.Conn, timeoutOpt int, is_http bool, is_res bool, user User) (total int, raw_header []byte) {
    var pipeBuf = leakybuf.NewLeakyBuf(nBuf, bufSize)
    defer dst.Close()
    buf := pipeBuf.Get()
    // defer pipeBuf.Put(buf)
    var buffer = bytes.NewBuffer(nil)
    var is_end = false
    var size int

    for {
        if timeoutOpt == ss.SET_TIMEOUT {
            ss.SetReadTimeout(src)
        }
        n, err := src.Read(buf)
        // read may return EOF with n > 0
        // should always process n > 0 bytes before handling error
        if n > 0 {
            if is_http && !is_end {
                buffer.Write(buf)
                raw_header = buffer.Bytes()
                lines := bytes.SplitN(raw_header, []byte("\r\n\r\n"), 2)
                if len(lines) == 2 {
                    is_end = true
                }
            }

            size, err = dst.Write(buf[0:n])
            if is_res {
                total_size, _ := storage.IncrSize("flow:" + user.Name, size)
                if total_size > user.Limit {
                    return
                }
            }
            total += size
            if err != nil {
                ss.Debug.Println("write:", err)
                break
            }
        }
        if err != nil || n == 0 {
            // Always "use of closed network connection", but no easy way to
            // identify this specific error. So just leave the error along for now.
            // More info here: https://code.google.com/p/go/issues/detail?id=4373
            break
        }
    }
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
        size, err := storage.GetSize("flow:" + user.Name)
        if user.Limit < size {
            log.Printf("Error user runover: %s\n", size)
            conn.Close()
            continue
        }

        // log.Println("creating cipher for user:", user.Name)
        cipher, err = ss.NewCipher(user.Method, user.Password)
        if err != nil {
            log.Printf("Error generating cipher for user: %s %v\n", user.Name, err)
            conn.Close()
            continue
        }
        go handleConnection(user, ss.NewConn(conn, cipher.Copy()))
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
