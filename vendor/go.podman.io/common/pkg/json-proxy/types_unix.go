//go:build unix

package jsonproxy

import (
	"encoding/json"
	"io"
	"net"
	"syscall"

	"github.com/sirupsen/logrus"
)

// send writes a reply buffer to the socket.
func (buf replyBuf) send(conn *net.UnixConn, err error) error {
	logrus.Debugf("Sending reply: err=%v value=%v pipeid=%v datafd=%v errfd=%v", err, buf.value, buf.pipeid, buf.fd, buf.errfd)
	// We took ownership of these FDs, so close when we're done sending them or on error
	defer func() {
		if buf.fd != nil {
			buf.fd.Close()
		}
		if buf.errfd != nil {
			buf.errfd.Close()
		}
	}()
	replyToSerialize := reply{
		Success: err == nil,
		Value:   buf.value,
		PipeID:  buf.pipeid,
	}
	if err != nil {
		replyToSerialize.ErrorCode = mapProxyErrorCode(err)
		replyToSerialize.Error = err.Error()
	}
	serializedReply, err := json.Marshal(&replyToSerialize)
	if err != nil {
		return err
	}
	// Copy the FD number(s) to the socket ancillary buffer
	fds := make([]int, 0)
	if buf.fd != nil {
		fds = append(fds, int(buf.fd.Fd()))
	}
	if buf.errfd != nil {
		fds = append(fds, int(buf.errfd.Fd()))
	}
	oob := syscall.UnixRights(fds...)
	n, oobn, err := conn.WriteMsgUnix(serializedReply, oob, nil)
	if err != nil {
		return err
	}
	// Validate that we sent the full packet
	if n != len(serializedReply) || oobn != len(oob) {
		return io.ErrShortWrite
	}
	return nil
}
