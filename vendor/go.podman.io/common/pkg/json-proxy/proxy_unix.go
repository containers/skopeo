//go:build !windows

package jsonproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/sirupsen/logrus"
)

// manager is the proxy server for managing JSON-RPC proxy operations.
// Use NewManager to create one.
type manager struct {
	handler *handler
	logger  logrus.FieldLogger
}

// NewManager creates a new proxy manager with the given options.
// WithSystemContext and WithPolicyContext are required.
//
// EXPERIMENTAL: This function is experimental and subject to breaking changes.
func NewManager(opts ...Option) (*manager, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	if o.getSystemContext == nil {
		return nil, errors.New("WithSystemContext is required")
	}
	if o.getPolicyContext == nil {
		return nil, errors.New("WithPolicyContext is required")
	}
	if o.logger == nil {
		o.logger = logrus.StandardLogger()
	}

	handler := newHandler(o.getSystemContext, o.getPolicyContext, o.logger)

	return &manager{
		handler: handler,
		logger:  o.logger,
	}, nil
}

// Serve runs the proxy server, reading requests from the given socket file descriptor.
func (m *manager) Serve(ctx context.Context, sockfd int) error {
	defer m.handler.close()

	// Convert the socket FD passed by client into a net.FileConn
	fd := os.NewFile(uintptr(sockfd), "sock")
	fconn, err := net.FileConn(fd)
	if err != nil {
		return err
	}
	conn, ok := fconn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("expected unix connection, got %T", fconn)
	}

	// Allocate a buffer to copy the packet into
	buf := make([]byte, maxMsgSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading socket: %v", err)
		}
		readbuf := buf[0:n]

		rb, terminate, err := m.handler.processRequest(ctx, readbuf)
		if terminate {
			m.logger.Debug("terminating")
			return nil
		}

		if err := rb.send(conn, err); err != nil {
			return fmt.Errorf("writing to socket: %w", err)
		}
	}
}

// Close releases all resources associated with this manager.
func (m *manager) Close() error {
	m.handler.close()
	return nil
}
