// Copyright 2009 The Go Authors. All rights reserved.
// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package json2

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"bytes"

	"github.com/agronomhidden/rpc/v2_batch"
)

var null = json.RawMessage([]byte("null"))
var Version = "2.0"

// ----------------------------------------------------------------------------
// Request and Response
// ----------------------------------------------------------------------------

// serverRequest represents a JSON-RPC request received by the server.
type serverRequest struct {
	// JSON-RPC protocol.
	Version string `json:"jsonrpc"`

	// A String containing the name of the method to be invoked.
	Method string `json:"method"`

	// A Structured value to pass as arguments to the method.
	Params *json.RawMessage `json:"params"`

	// The request id. MUST be a string, number or null.
	// Our implementation will not do type checking for id.
	// It will be copied as it is.
	Id *json.RawMessage `json:"id"`
}

// serverResponse represents a JSON-RPC response returned by the server.
type serverResponse struct {
	// JSON-RPC protocol.
	Version string `json:"jsonrpc"`

	// The Object that was returned by the invoked method. This must be null
	// in case there was an error invoking the method.
	// As per spec the member will be omitted if there was an error.
	Result interface{} `json:"result,omitempty"`

	// An Error object if there was an error invoking the method. It must be
	// null if there was no error.
	// As per spec the member will be omitted if there was no error.
	Error *Error `json:"error,omitempty"`

	// This must be the same id as the request it is responding to.
	Id *json.RawMessage `json:"id"`
}

// ----------------------------------------------------------------------------
// Codec
// ----------------------------------------------------------------------------

// NewcustomCodec returns a new JSON Codec based on passed encoder selector.
func NewCustomCodec(encSel rpc.EncoderSelector) *Codec {
	return &Codec{encSel: encSel}
}

// NewCodec returns a new JSON Codec.
func NewCodec() *Codec {
	return NewCustomCodec(rpc.DefaultEncoderSelector)
}

// Codec creates a CodecRequest to process each request.
type Codec struct {
	encSel rpc.EncoderSelector
}

// NewRequest returns a CodecRequest.
func (c *Codec) NewRequest(r *http.Request) ([]rpc.CodecRequest, error) {
	return newCodecRequest(r, c.encSel.Select(r))
}

func (c *Codec) WriteBatchedReply(r *http.Request, w http.ResponseWriter, replyArray []interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder_ := c.encSel.Select(r)
	encoder := json.NewEncoder(encoder_.Encode(w))

	var temp interface{}
	if len(replyArray) == 1 {
		temp = replyArray[0]
	} else {
		temp = replyArray
	}

	err := encoder.Encode(temp)
	if err != nil {
		rpc.WriteError(w, 400, err.Error())
	}
}

// ----------------------------------------------------------------------------
// CodecRequest
// ----------------------------------------------------------------------------

// newCodecRequest returns a new CodecRequest.
func newCodecRequest(r *http.Request, encoder rpc.Encoder) ([]rpc.CodecRequest, error) {

	//jason:
	body_, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil || body_ == nil || len(body_) < 2 { // think of "[]" or "{}"
		return []rpc.CodecRequest{}, nil
	}

	// Decode the request body and check if RPC method is valid.
	var reqArray []serverRequest
	var req_ serverRequest
	var isMultiQuery bool

	for _, char := range body_ {
		if char != 32 {
			isMultiQuery = char == 91
			break
		}
	}

	if !isMultiQuery {
		err = json.NewDecoder(bytes.NewBuffer(body_)).Decode(&req_)
		reqArray = []serverRequest{req_}
	} else {
		err = json.NewDecoder(bytes.NewBuffer(body_)).Decode(&reqArray)
	}

	if err != nil {
		err = &Error{
			Code:    E_PARSE,
			Message: err.Error(),
			Data:    body_,
		}
		return nil, err
	}

	codecRequestArray := make([]rpc.CodecRequest, len(reqArray))

	for i, req := range reqArray {
		if req.Version != Version {
			err := &Error{
				Code:    E_INVALID_REQ,
				Message: "jsonrpc must be " + Version,
				Data:    req,
			}
			codecRequestArray[i] = &CodecRequest{request: &reqArray[i], err: err, encoder: encoder, body: body_}

		} else {
			codecRequestArray[i] = &CodecRequest{request: &reqArray[i], err: nil, encoder: encoder, body: body_}

		}
	}

	return codecRequestArray, nil

}

// CodecRequest decodes and encodes a single request.
type CodecRequest struct {
	request *serverRequest
	err     error
	encoder rpc.Encoder
	//Jason
	body []byte
}

// Method returns the RPC method for the current request.
//
// The method uses a dotted notation as in "Service.Method".
func (c *CodecRequest) Method() (string, error) {
	if c.err == nil {
		return c.request.Method, nil
	}
	return "", c.err
}

//Jason
func (c *CodecRequest) Body() []byte {
	return c.body
}

func (c *CodecRequest) Error() error {
	return c.err
}

// ReadRequest fills the request object for the RPC method.
func (c *CodecRequest) ReadRequest(args interface{}) error {
	if c.err == nil {
		if c.request.Params != nil {
			// JSON params structured object. Unmarshal to the args object.
			err := json.Unmarshal(*c.request.Params, args)
			if err != nil {
				c.err = &Error{
					Code:    E_INVALID_REQ,
					Message: err.Error(),
					Data:    c.request.Params,
				}
			}
		} else {
			c.err = &Error{
				Code:    E_INVALID_REQ,
				Message: "rpc: method request ill-formed: missing params field",
			}
		}
	}
	return c.err
}

// WriteResponse encodes the response and writes it to the ResponseWriter.
func (c *CodecRequest) WriteResponse(w http.ResponseWriter, reply interface{}) {
	res := &serverResponse{
		Version: Version,
		Result:  reply,
		Id:      c.request.Id,
	}
	c.writeServerResponse(w, res)
}

func (c *CodecRequest) WriteError(w http.ResponseWriter, status int, err error) {
	jsonErr, ok := err.(*Error)
	if !ok {
		jsonErr = &Error{
			Code:    E_SERVER,
			Message: err.Error(),
		}
	}
	res := &serverResponse{
		Version: Version,
		Error:   jsonErr,
		Id:      c.request.Id,
	}
	c.writeServerResponse(w, res)
}

func (c *CodecRequest) ResponseReply(reply interface{}) interface{} {
	res := &serverResponse{
		Version: Version,
		Result:  reply,
		Id:      c.request.Id,
	}
	return res
}

func (c *CodecRequest) ErrorReply(err error) interface{} {
	jsonErr, ok := err.(*Error)
	if !ok {
		jsonErr = &Error{
			Code:    E_SERVER,
			Message: err.Error(),
		}
	}
	res := &serverResponse{
		Version: Version,
		Error:   jsonErr,
		Id:      c.request.Id,
	}
	return res
}

func (c *CodecRequest) writeServerResponse(w http.ResponseWriter, res *serverResponse) {
	// Id is null for notifications and they don't have a response.
	if c.request.Id != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		encoder := json.NewEncoder(c.encoder.Encode(w))
		err := encoder.Encode(res)

		// Not sure in which case will this happen. But seems harmless.
		if err != nil {
			rpc.WriteError(w, 400, err.Error())
		}
	}
}

type EmptyResponse struct {
}
