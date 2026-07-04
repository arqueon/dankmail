package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client is the CLI/scripting side of the IPC socket.
type Client struct {
	conn   net.Conn
	enc    *json.Encoder
	sc     *bufio.Scanner
	nextID int
	Caps   Capabilities
}

// Dial connects and performs the capabilities handshake.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, enc: json.NewEncoder(conn), sc: bufio.NewScanner(conn)}
	c.sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !c.sc.Scan() {
		_ = conn.Close()
		return nil, fmt.Errorf("ipc: no handshake from daemon")
	}
	if err := json.Unmarshal(c.sc.Bytes(), &c.Caps); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ipc: bad handshake: %w", err)
	}
	return c, nil
}

func (c *Client) Close() error { return c.conn.Close() }

type rawResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// Call performs one request/response round trip.
func (c *Client) Call(method string, params map[string]any) (json.RawMessage, error) {
	c.nextID++
	if err := c.enc.Encode(Request{ID: c.nextID, Method: method, Params: params}); err != nil {
		return nil, err
	}
	if !c.sc.Scan() {
		return nil, fmt.Errorf("ipc: connection closed by daemon")
	}
	var resp rawResponse
	if err := json.Unmarshal(c.sc.Bytes(), &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Result, nil
}

// Subscribe switches this connection to the event stream. The returned
// channel closes when the daemon goes away. The client cannot Call
// afterwards; open a second connection for commands.
func (c *Client) Subscribe() (<-chan Event, error) {
	c.nextID++
	if err := c.enc.Encode(Request{ID: c.nextID, Method: "subscribe"}); err != nil {
		return nil, err
	}
	if !c.sc.Scan() { // ack line
		return nil, fmt.Errorf("ipc: connection closed by daemon")
	}
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		for c.sc.Scan() {
			var ev Event
			if err := json.Unmarshal(c.sc.Bytes(), &ev); err != nil {
				continue
			}
			ch <- ev
		}
	}()
	return ch, nil
}
