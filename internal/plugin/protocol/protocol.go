// Package protocol is the contract between Shibu and a plugin: the
// manifest a plugin declares, the JSON-RPC messages of one line each,
// and the parts a plugin draws.
//
// It holds types and their checks only: no UI and no process.
package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// Version is the protocol version the app speaks. The host sends it in
// hello, the plugin answers with its own, and the host refuses any other.
const Version = 1

// MaxLine is the longest message line, in bytes: 4 MB.
const MaxLine = 4 << 20

// ErrLineTooLong is what a decoder reads when a line is longer than
// MaxLine. The rest of the line is dropped, so that the next line
// decodes.
var ErrLineTooLong = errors.New("plugin protocol: message longer than 4 MB")

// ErrVersion is what a host reads for a protocol version it does not
// know.
var ErrVersion = errors.New("plugin protocol: unknown version")

// CheckVersion refuses a protocol version this app does not speak.
func CheckVersion(v int) error {
	if v != Version {
		return fmt.Errorf("%w: %d", ErrVersion, v)
	}
	return nil
}

// The methods the host calls on a plugin.
const (
	MethodHello    = "hello"
	MethodOpen     = "open"
	MethodClose    = "close"
	MethodResize   = "resize"
	MethodVisible  = "visible"
	MethodTheme    = "theme"
	MethodSettings = "settings"
	MethodEvent    = "event"
	MethodCommand  = "command"
)

// The methods a plugin calls on its host.
const (
	MethodRender    = "render"
	MethodStateSave = "state.save"
	MethodSecretGet = "secret.get"
	MethodSecretSet = "secret.set"
	MethodNotify    = "notify"
	MethodOpenURL   = "openURL"
	MethodWebOpen   = "web.open"
	MethodWebClose  = "web.close"
)

// The error codes of JSON-RPC 2.0.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeNoMethod       = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
)

// Request is one JSON-RPC 2.0 request, or a notification when it has no
// id: one line of a plugin's stream.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification tells a request that wants no answer.
func (r *Request) IsNotification() bool { return len(r.ID) == 0 }

// Response is one JSON-RPC 2.0 answer: one line of a plugin's stream.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is what a response carries instead of a result.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error reads as its message.
func (e *Error) Error() string { return e.Message }

// NewRequest builds a request that asks for an answer.
func NewRequest(id int64, method string, params any) (*Request, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	idJSON, err := json.Marshal(id)
	if err != nil {
		return nil, err
	}
	return &Request{JSONRPC: "2.0", ID: idJSON, Method: method, Params: raw}, nil
}

// NewNotification builds a message that wants no answer.
func NewNotification(method string, params any) (*Request, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	return &Request{JSONRPC: "2.0", Method: method, Params: raw}, nil
}

// NewResult answers a request.
func NewResult(id json.RawMessage, result any) (*Response, error) {
	raw, err := marshalParams(result)
	if err != nil {
		return nil, err
	}
	return &Response{JSONRPC: "2.0", ID: id, Result: raw}, nil
}

// NewFailure answers a request with an error.
func NewFailure(id json.RawMessage, code int, message string) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
}

func marshalParams(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Encoder writes one message per line. It is safe for concurrent use,
// so that a plugin's own goroutines can render while its loop reads.
type Encoder struct {
	mu sync.Mutex
	w  io.Writer
}

// NewEncoder writes to w.
func NewEncoder(w io.Writer) *Encoder { return &Encoder{w: w} }

// Encode writes v as one line, and refuses a line that is too long.
func (e *Encoder) Encode(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b)+1 > MaxLine {
		return ErrLineTooLong
	}
	b = append(b, '\n')
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err = e.w.Write(b)
	return err
}

// Decoder reads one message per line, and refuses a line longer than
// MaxLine.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReaderSize(r, 64<<10)}
}

// Decode reads one message into v.
func (d *Decoder) Decode(v any) error {
	line, err := d.ReadLine()
	if err != nil {
		return err
	}
	return json.Unmarshal(line, v)
}

// ReadLine returns the next line, without its newline, skipping empty
// lines as a stream that has not yet warmed up.
func (d *Decoder) ReadLine() ([]byte, error) {
	for {
		line, err := d.line()
		if err != nil {
			return nil, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			continue
		}
		return line, nil
	}
}

func (d *Decoder) line() ([]byte, error) {
	var buf []byte
	for {
		chunk, err := d.r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > MaxLine {
			// Drop the rest of the line, so that the next one decodes.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = d.r.ReadSlice('\n')
			}
			return nil, ErrLineTooLong
		}
		switch {
		case err == nil:
			return buf, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case len(buf) > 0 && errors.Is(err, io.EOF):
			// The last line, without a newline.
			return buf, nil
		default:
			return nil, err
		}
	}
}

// MethodPermission is the permission each method a plugin calls needs,
// "" when it needs none.
var MethodPermission = map[string]string{
	MethodRender:    "",
	MethodStateSave: "",
	MethodSecretGet: PermSecrets,
	MethodSecretSet: PermSecrets,
	MethodNotify:    PermNotify,
	MethodOpenURL:   PermOpenURL,
	MethodWebOpen:   PermWeb,
	MethodWebClose:  PermWeb,
}

// Allows tells whether a plugin that asked for these permissions may
// call the method. A method the protocol does not know is refused.
func (p Permissions) Allows(method string) bool {
	need, ok := MethodPermission[method]
	if !ok {
		return false
	}
	return need == "" || slices.Contains(p, need)
}
