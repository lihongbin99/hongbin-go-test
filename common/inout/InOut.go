package inout

import (
	"hongbin-p2p-test/common/convert"
	"hongbin-p2p-test/common/logger"
	"hongbin-p2p-test/common/transfer"
	"io"
	"sync"
)

var writeMut = sync.Mutex{}
var readMut = sync.Mutex{}

func Write(w io.Writer, transferType byte, message []byte) error {
	return WriteConnId(w, transferType, -1, message)
}

func WriteConnId(w io.Writer, transferType byte, connId int32, message []byte) error {
	writeMut.Lock()
	writeMut.Unlock()
	messageLength := int32(len(message))
	if connId != -1 {
		messageLength += 4
	}
	length := convert.I2b(messageLength)

	if err := write(w, length); err != nil {
		return nil
	}
	if err := write(w, []byte{transferType}); err != nil {
		return nil
	}
	if connId != -1 {
		if err := write(w, convert.I2b(connId)); err != nil {
			return nil
		}
	}
	if messageLength > 0 {
		if err := write(w, message); err != nil {
			return nil
		}
	}
	return nil
}

func write(w io.Writer, message []byte) error {
	var sendLength int32 = 0
	for sendLength < int32(len(message)) {
		writeLength, err := w.Write(message)
		if err != nil {
			return err
		}
		sendLength += int32(writeLength)
	}
	return nil
}

func Read(r io.Reader, buf []byte) (int32, byte, error) {
	readMut.Lock()
	readMut.Unlock()
	length := make([]byte, 4)
	if err := read(r, length, 4); err != nil {
		return 0, transfer.ErrorCode, err
	}
	messageLength := convert.B2i(length)
	if err := read(r, length[:1], 1); err != nil {
		return 0, transfer.ErrorCode, err
	}
	if messageLength > 0 {
		if err := read(r, buf, messageLength); err != nil {
			return 0, transfer.ErrorCode, err
		}
	}
	return messageLength, length[0], nil
}

func read(r io.Reader, buf []byte, length int32) error {
	var receiverLength int32 = 0
	for receiverLength < length {
		readLength, err := r.Read(buf[receiverLength:])
		if err != nil {
			return err
		}
		receiverLength += int32(readLength)
	}
	return nil
}

func Close(closers ...io.Closer) {
	for _, err := range closers {
		if em := err.Close(); em != nil {
			logger.Error("close", em)
		}
	}
}
