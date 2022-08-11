package main

import (
	"hongbin-p2p-test/common/convert"
	"hongbin-p2p-test/common/inout"
	"hongbin-p2p-test/common/logger"
	"hongbin-p2p-test/common/transfer"
	"io"
	"log"
	"net"
	"time"
)

var (
	name      []byte
	target    []byte
	localAddr string

	clientType string

	//serverAddr       = "0.0.0.0:13520"
	serverAddr = "43.128.70.137:13520"
	capLength  = 1024 * 64
)

type NatError struct {
	err   error
	reset bool
}

type ReadContext struct {
	readLength   int32
	transferType byte
	buf          []byte
	socket       net.Conn
}

type TransferToMessage struct {
	connId     int32
	conn       net.Conn
	readLength int
	buf        []byte
	err        error
}

func init() {
	clientType = "client"
	//clientType = "server"

	if clientType == "client" {
		name = []byte("client2")
		target = []byte("client1")
		localAddr = "0.0.0.0:9090"
	} else if clientType == "server" {
		name = []byte("client1")
		target = []byte("client2")
		localAddr = "0.0.0.0:8080"
	}
}

func main() {
	readChan := make(chan ReadContext)
	natConn := HandleNat()

	go func() {
		for {
			buf := make([]byte, capLength)
			setReadDeadlineErr := natConn.SetReadDeadline(time.Now().Add(30 * time.Second))
			if setReadDeadlineErr != nil {
				logger.Error("set read deadline reset", setReadDeadlineErr)
				natConn = HandleNat()
				continue
			}
			readLength, transferType, err := inout.Read(natConn, buf)
			if err != nil {
				logger.Error("nat read error start reset", err)
				natConn = HandleNat()
				continue
			}
			logger.Info("read success, readLength: %d, transferType: %d", readLength, transferType)
			// 处理函数
			readChan <- ReadContext{
				readLength:   readLength,
				transferType: transferType,
				buf:          buf,
				socket:       natConn,
			}
			<-readChan
		}
	}()

	if clientType == "client" {
		client(natConn, readChan)
	} else if clientType == "server" {
		server(natConn, readChan)
	}
}

func TransferTo(connId int32, conn net.Conn, transferToMessageChan chan TransferToMessage) {
	logger.Info("start wait client message, connId: %d", connId)
	buf := make([]byte, capLength-1024)
	for {
		readLength, err := conn.Read(buf)
		logger.Info("read client message success, connId: %d, readLength: %d", connId, readLength)
		transferToMessageChan <- TransferToMessage{
			connId:     connId,
			conn:       conn,
			readLength: readLength,
			buf:        buf,
			err:        err,
		}
		if err != nil {
			inout.Close(conn)
			return
		}
		<-transferToMessageChan
	}
}

func HandleTransferToMessage(natConn net.Conn, transferToMessageChanMap map[int32]chan TransferToMessage) {
	for connId, transferToMessageChan := range transferToMessageChanMap {
		select {
		case transferToMessage := <-transferToMessageChan:
			if transferToMessage.err != nil {
				if transferToMessage.err == io.EOF {
					logger.Info("client read over, connId: %d", connId)
				} else {
					logger.Error("client read error, connId: %d", connId, transferToMessage.err)
				}
				logger.Info("delete transferToMessageChanMap, connId: %d", connId)
				delete(transferToMessageChanMap, connId)
				if err := inout.WriteConnId(natConn, transfer.Close, connId, []byte(transferToMessage.err.Error())); err != nil {
					logger.Error("send close error to nat", err)
					inout.Close(natConn)
					break
				}
				logger.Info("send close to nat success, connId: %d", connId)
				break
			}
			if err := inout.WriteConnId(natConn, transfer.Transfer, transferToMessage.connId, transferToMessage.buf[:transferToMessage.readLength]); err != nil {
				logger.Error("send transfer error", err)
				inout.Close(natConn)
			} else {
				logger.Info("send transfer success, connId: %d, sendLength: %d", transferToMessage.connId, transferToMessage.readLength)
			}
			transferToMessageChan <- TransferToMessage{}
		default:
			// no operation
		}
	}
}

func client(natConn net.Conn, readChan chan ReadContext) {
	connChan := make(chan net.Conn, 3)
	transferToMessageChanMap := make(map[int32]chan TransferToMessage)
	serverAddr, err := net.ResolveTCPAddr("tcp", localAddr)
	if err != nil {
		log.Fatal(err)
	}

	listen, err := net.ListenTCP("tcp", serverAddr)
	if err != nil {
		log.Fatal(err)
	}

	logger.Info("bind server success: %s", listen.Addr().String())

	go func() {
		for {
			socket, err := listen.Accept()
			if err != nil {
				logger.Error("accept error, %v", err)
				continue
			}
			logger.Info("new connect, addr: %s", socket.RemoteAddr().String())
			// 处理函数
			connChan <- socket
		}
	}()

	var connId int32 = 1
	connMap := make(map[int32]net.Conn)

	// Ping=
	pingTicker := time.NewTicker(3 * time.Second)
	lastPingTime := time.Now()
	pingTimeOutTicker := time.NewTicker(30 * time.Second)

	for {
		select {
		case <-pingTicker.C:
			if err := inout.Write(natConn, transfer.Ping, []byte{}); err != nil {
				logger.Info("ping error")
				inout.Close(natConn)
				break
			}
		case <-pingTimeOutTicker.C:
			if time.Now().Sub(lastPingTime).Seconds() > 30 {
				logger.Info("ping time out")
				inout.Close(natConn)
				break
			}
		case newSocket := <-connChan:
			connMap[connId] = newSocket
			if err := inout.Write(natConn, transfer.NewConnect, convert.I2b(connId)); err != nil {
				logger.Error("send new connect error", err)
				inout.Close(newSocket)
				break
			}
			logger.Info("send new connect success, coinId: %d", connId)
			connId++
		case readContext := <-readChan:
			switch readContext.transferType {
			case transfer.Ping:
				logger.Info("ping success")
				lastPingTime = time.Now()
			case transfer.NewConnectSuccess:
				connId := convert.B2i(readContext.buf[0:4])
				logger.Info("new connect success, connId: %d", connId)
				// 接收数据并且发送
				transferToMessageChanMap[connId] = make(chan TransferToMessage)
				if conn, ok := connMap[connId]; ok {
					if transferToMessageChan, ok := transferToMessageChanMap[connId]; ok {
						go TransferTo(connId, conn, transferToMessageChan)
						break
					}
				}
				logger.Info("error!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
			case transfer.NewConnectError:
				connId := convert.B2i(readContext.buf[0:4])
				logger.Info("new connect error, connId: %d, error: %s", connId, readContext.buf[4:readContext.transferType])
			case transfer.Transfer:
				connId := convert.B2i(readContext.buf[0:4])
				if localSocket, ok := connMap[connId]; ok {
					// TODO go
					if err := HandleTransfer(connId, natConn, readContext, localSocket); err != nil {
						delete(connMap, connId)
						logger.Info("close client success, connId: %d", connId)
					}
				}
			case transfer.Close:
				connId := convert.B2i(readContext.buf[0:4])
				logger.Info("from close, connId: %d, error: %s", connId, string(readContext.buf[4:readContext.readLength]))
				if _, ok := connMap[connId]; ok {
					delete(connMap, connId)
				}
				logger.Info("from close success, connId: %d", connId)
			default:
				logger.Info("read type error, transferType: %d", readContext.transferType)
				inout.Close(natConn)
			}
			readChan <- ReadContext{}
		default:
			HandleTransferToMessage(natConn, transferToMessageChanMap)
		}
	}
}

func server(natConn net.Conn, readChan chan ReadContext) {
	connMap := make(map[int32]net.Conn)
	transferToMessageChanMap := make(map[int32]chan TransferToMessage)
	lastPingTime := time.Now()
	pingTimeOutTicker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-pingTimeOutTicker.C:
			if time.Now().Sub(lastPingTime).Seconds() > 30 {
				logger.Info("ping time out")
				inout.Close(natConn)
				break
			}
		case readContext := <-readChan:
			switch readContext.transferType {
			case transfer.Ping:
				logger.Info("ping success")
				if err := inout.Write(natConn, transfer.Ping, []byte{}); err != nil {
					logger.Error("ping send error", err)
					inout.Close(natConn)
					break
				}
				lastPingTime = time.Now()
			case transfer.NewConnect:
				connId := convert.B2i(readContext.buf[:readContext.readLength])
				conn, err := net.Dial("tcp", localAddr)
				if err != nil {
					if err := inout.WriteConnId(natConn, transfer.NewConnectError, connId, []byte(err.Error())); err != nil {
						logger.Error("new connect error send error", err)
						inout.Close(natConn)
						break
					}
				} else {
					connMap[connId] = conn
					if err := inout.WriteConnId(readContext.socket, transfer.NewConnectSuccess, connId, []byte{}); err != nil {
						logger.Error("new connect success send error", err)
					}
					logger.Info("new connect success, connId: %d", connId)
					// 接收数据并且发送
					transferToMessageChanMap[connId] = make(chan TransferToMessage)
					if conn, ok := connMap[connId]; ok {
						if transferToMessageChan, ok := transferToMessageChanMap[connId]; ok {
							go TransferTo(connId, conn, transferToMessageChan) // TODO 锁异常
							break
						}
					}
					logger.Info("error!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
				}
			case transfer.Transfer:
				connId := convert.B2i(readContext.buf[0:4])
				if localSocket, ok := connMap[connId]; ok {
					// TODO go
					if err := HandleTransfer(connId, natConn, readContext, localSocket); err != nil {
						delete(connMap, connId)
						logger.Info("close client success, connId: %d", connId)
					}
				}
			case transfer.Close:
				connId := convert.B2i(readContext.buf[0:4])
				logger.Info("from close, connId: %d, error: %s", connId, string(readContext.buf[4:readContext.readLength]))
				if _, ok := connMap[connId]; ok {
					delete(connMap, connId)
				}
				logger.Info("from close success, connId: %d", connId)
			default:
				logger.Info("read transferType: %d", readContext.transferType)
			}
			readChan <- ReadContext{}
		default:
			HandleTransferToMessage(natConn, transferToMessageChanMap)
		}
	}
}

func HandleTransfer(connId int32, natConn net.Conn, readContext ReadContext, localSocket net.Conn) error {
	logger.Info("start transfer local, connId: %d, len: %d", connId, readContext.readLength-4)
	writeLength, err := localSocket.Write(readContext.buf[4:readContext.readLength])
	if err != nil {
		logger.Error("transfer local error, send close", err)
		if err := inout.WriteConnId(natConn, transfer.Close, connId, []byte(err.Error())); err != nil {
			logger.Error("transfer send close error", err)
			inout.Close(localSocket)
			return err
		}
	}
	logger.Info("start transfer local success, connId: %d, writeLength: %d", connId, writeLength)
	return nil
}

func HandleNat() net.Conn {
	for {
		conn := handleNat()
		if conn == nil {
			time.Sleep(3 * time.Second)
			continue
		}
		return conn
	}

}

func handleNat() net.Conn {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		logger.Error("dial server error", err)
		return nil
	}

	buf := make([]byte, capLength)

	sendMessage := make([]byte, 0)
	sendMessage = append(sendMessage[:0], name...)
	if err := inout.Write(conn, transfer.Register, sendMessage); err != nil {
		logger.Error("register send error", err)
		return nil
	}

	_, transferType, err := inout.Read(conn, buf)
	if err != nil {
		logger.Error("read register result error", err)
		return nil
	}

	if transferType != transfer.RegisterSuccess {
		logger.Info("register error, code: %d", transferType)
		return nil
	}

	logger.Info("register success")

	sendMessage = append(sendMessage[:0], target...)
	if err := inout.Write(conn, transfer.Nat, sendMessage); err != nil {
		logger.Error("send nat to server error", err)
		return nil
	}

	readLength, transferType, err := inout.Read(conn, buf)
	if err != nil {
		logger.Error("read nat result from server error", err)
		return nil
	}

	if transferType != transfer.NatSuccess {
		logger.Info("nat error, code: %d", transferType)
		return nil
	}

	targetAddr := string(buf[:readLength])
	logger.Info("connect success, target addr: %s", targetAddr)

	if err := conn.Close(); err != nil {
		logger.Error("close server connect error", err)
		return nil
	}

	selfAddr, err := net.ResolveTCPAddr("tcp", conn.LocalAddr().String())
	remoteAddr, err := net.ResolveTCPAddr("tcp", targetAddr)
	logger.Info("self[%s] -> target[%s]", conn.LocalAddr().String(), targetAddr)
	conn, err = net.DialTCP("tcp", selfAddr, remoteAddr)
	if err != nil {
		logger.Error("dial target error", err)
		return nil
	}

	if err := inout.Write(conn, transfer.Ping, []byte{}); err != nil {
		logger.Error("write ping target error", err)
		return nil
	}

	for {
		readLength, transferType, err := inout.Read(conn, buf)
		context := string(buf[:readLength])
		logger.Info("read success readLength: %d, transferType: %d, context: %s", readLength, transferType, context)
		if err != nil {
			logger.Error("read ping target error", err)
			return nil
		}
		switch transferType {
		case transfer.Ping:
			logger.Info("nat success")
			if true { // 测试
				ticker := time.NewTicker(180 * time.Second)
				go func() {
					for {
						readLength, transferType, err := inout.Read(conn, buf)
						context := string(buf[:readLength])
						logger.Info("test read success readLength: %d, transferType: %d, context: %s", readLength, transferType, context)
						if err != nil {
							logger.Error("test read ping target error", err)
						}
					}
				}()
				var flag = true
				for flag {
					t := <-ticker.C
					logger.Info("test start send ping")
					if err := inout.Write(conn, transfer.Ping, []byte(t.String())); err != nil {
						flag = false
						logger.Error("test write ping target error", err)
						return nil
					}
				}
			}
			return conn
		default:
			logger.Info("read transferType: %d", transferType)
		}
	}
}
