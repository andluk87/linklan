package uibridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"linklan/internal/tunnel"
	"sync"
)

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type response struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Serve accepts only framed JSON commands on inherited stdio handles. No TCP
// listener or externally accessible named pipe exposes VPN credentials/actions.
func Serve(store *tunnel.Store, opts Options, input io.Reader, output io.Writer) error {
	c := New(store, opts)
	defer c.Close()
	var writer sync.Mutex
	encoder := json.NewEncoder(output)
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var r request
			if e := json.Unmarshal(scanner.Bytes(), &r); e != nil {
				done <- fmt.Errorf("invalid UI frame")
				return
			}
			if r.ID == 0 {
				done <- fmt.Errorf("invalid UI request ID")
				return
			}
			go func(r request) {
				value, e := c.Handle(r.Method, r.Params)
				reply := response{ID: r.ID, Result: value}
				if e != nil {
					reply.Result = nil
					reply.Error = e.Error()
				}
				writer.Lock()
				writeErr := encoder.Encode(reply)
				writer.Unlock()
				if writeErr != nil || r.Method == "shutdown" {
					c.cancel()
				}
			}(r)
		}
		done <- scanner.Err()
	}()
	select {
	case <-c.ctx.Done():
		return nil
	case e := <-done:
		return e
	}
}
