package main

import (
	"hongbin-p2p-test/common/inout"
	"hongbin-p2p-test/common/logger"
	"hongbin-p2p-test/common/transfer"
	"io"
	"log"
	"net"
	"sync"
)

var (
	serverAddr = "0.0.0.0:13520"
	capLength  = 1024 * 64
)

type Clients struct {
	sync.Mutex
	addr2name map[string]string
	name2addr map[string]net.Conn
}

var clientsPool = Clients{
	addr2name: make(map[string]string),
	name2addr: make(map[string]net.Conn),
}

func main() {
	serverAddr, err := net.ResolveTCPAddr("tcp", serverAddr)
	if err != nil {
		log.Fatal(err)
	}

	listen, err := net.ListenTCP("tcp", serverAddr)
	if err != nil {
		log.Fatal(err)
	}

	//if err := listen.SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
	//	log.Fatal(err)
	//}

	logger.Info("start server success: %s", listen.Addr().String())
	for {
		socket, err := listen.Accept()
		if err != nil {
			logger.Error("accept error, %v", err)
			continue
		}
		logger.Info("new connect, addr: %s", socket.RemoteAddr().String())
		// 处理函数
		go handleAccept(socket)
	}
}

func handleAccept(socket net.Conn) {
	buf := make([]byte, capLength)
	var err error
	var deleteClient = false
	var clientName string
	for {
		var readLength int32
		var transferType byte
		readLength, transferType, err = inout.Read(socket, buf)
		if err == io.EOF {
			logger.Info("read eof addr: %s", socket.RemoteAddr().String())
			break
		}
		if err != nil {
			logger.Error("read error addr: %s", err, socket.RemoteAddr().String())
			break
		}
		context := string(buf[:readLength])
		logger.Info("read success addr: %s, readLength: %d, transferType: %d, context: %s", socket.RemoteAddr().String(), readLength, transferType, context)
		switch transferType {
		case transfer.Register:
			clientName = context
			var flag bool
			flag, err = handleRegister(socket, clientName)
			if flag {
				deleteClient = true
			}
		case transfer.Nat:
			targetName := context
			err = handleConnect(socket, targetName)
		}

		if err != nil {
			logger.Error("handleAccept error addr: %s", err, socket.RemoteAddr().String())
			break
		}
	}

	// 删除连接
	if deleteClient {
		clientsPool.Lock()
		defer clientsPool.Unlock()
		logger.Info("client pool delete client name: %s, addr: %s", clientName, socket.RemoteAddr().String())
		delete(clientsPool.name2addr, clientName)
		delete(clientsPool.addr2name, socket.RemoteAddr().String())
	}

	// 关闭连接
	inout.Close(socket)
}

/**********************************************************************************************************************/

func handleRegister(socket net.Conn, clientName string) (bool, error) {
	if len(clientName) > 0 {
		clientsPool.Lock()
		defer clientsPool.Unlock()
		if _, ok := clientsPool.name2addr[clientName]; !ok { // 已存在, 拒绝
			logger.Info("client pool add client name: %s, addr: %s", clientName, socket.RemoteAddr().String())
			clientsPool.name2addr[clientName] = socket
			clientsPool.addr2name[socket.RemoteAddr().String()] = clientName
			// 返回成功
			if err := inout.Write(socket, transfer.RegisterSuccess, []byte{}); err != nil {
				logger.Error("register recode send error", err)
				return false, err
			}
			return true, nil // 不存在 添加成功后返回
		}
		logger.Info("client name exist: %s", clientName)
	}
	logger.Info("client name error: %s", clientName)
	// 返回异常
	if err := inout.Write(socket, transfer.RegisterError, []byte{}); err != nil {
		logger.Error("register recode send error", err)
		return false, err
	}
	return false, nil
}

func handleConnect(socket net.Conn, targetName string) error {
	if len(targetName) > 0 {
		clientsPool.Lock()
		defer clientsPool.Unlock()
		if targetSocket, ok := clientsPool.name2addr[targetName]; ok { // 存在
			logger.Info("connect success %s[%s] -> %s[%s]", clientsPool.addr2name[socket.RemoteAddr().String()], socket.RemoteAddr().String(), targetName, targetSocket.RemoteAddr().String())
			// 返回成功
			if err := inout.Write(socket, transfer.NatSuccess, []byte(targetSocket.RemoteAddr().String())); err != nil {
				return err
			}
			if err := inout.Write(targetSocket, transfer.NatSuccess, []byte(socket.RemoteAddr().String())); err != nil {
				return err
			}
		}
		logger.Info("connect addr: %s, no find [%s]", socket.RemoteAddr().String(), targetName)
		return nil // 不存在直接返回
	}
	// 返回异常
	if err := inout.Write(socket, transfer.NatError, []byte{}); err != nil {
		return err
	}
	return nil
}
