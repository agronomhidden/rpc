// Copyright 2009 The Go Authors. All rights reserved.
// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
)

// ----------------------------------------------------------------------------
// Codec
// ----------------------------------------------------------------------------

// Codec creates a CodecRequest to process each request.
type Codec interface {
	NewRequest(*http.Request) ([]CodecRequest, error)
	WriteBatchedReply(r *http.Request, w http.ResponseWriter, replyArray []interface{})
}

// CodecRequest decodes a request and encodes a response using a specific
// serialization scheme.
type CodecRequest interface {
	// Reads the request and returns the RPC method name.
	Method() (string, error)
	// Reads the request filling the RPC method args.
	ReadRequest(interface{}) error
	// Writes the response using the RPC method reply.
	//WriteResponse(http.ResponseWriter, interface{})
	// Writes an error produced by the server.
	//WriteError(w http.ResponseWriter, status int, err error)

	ErrorReply(err error) interface{}
	ResponseReply(reply interface{}) interface{}

	//Jason: extended for auth check
	Body() []byte
	Error() error
}

// ----------------------------------------------------------------------------
// Server
// ----------------------------------------------------------------------------

// NewServer returns a new RPC server.
func NewServer() *Server {
	return &Server{
		codecs:   make(map[string]Codec),
		services: new(serviceMap),
	}
}

// Server serves registered RPC services using registered codecs.
type Server struct {
	codecs   map[string]Codec
	services *serviceMap
}

// RegisterCodec adds a new codec to the server.
//
// Codecs are defined to process a given serialization scheme, e.g., JSON or
// XML. A codec is chosen based on the "Content-Type" header from the request,
// excluding the charset definition.
func (s *Server) RegisterCodec(codec Codec, contentType string) {
	s.codecs[strings.ToLower(contentType)] = codec
}

// RegisterService adds a new service to the server.
//
// The name parameter is optional: if empty it will be inferred from
// the receiver type name.
//
// Methods from the receiver will be extracted if these rules are satisfied:
//
//    - The receiver is exported (begins with an upper case letter) or local
//      (defined in the package registering the service).
//    - The method name is exported.
//    - The method has three arguments: *http.Request, *args, *reply.
//    - All three arguments are pointers.
//    - The second and third arguments are exported or local.
//    - The method has return type error.
//
// All other methods are ignored.
func (s *Server) RegisterService(receiver interface{}, name string) error {
	return s.services.register(receiver, name)
}

// HasMethod returns true if the given method is registered.
//
// The method uses a dotted notation as in "Service.Method".
func (s *Server) HasMethod(method string) bool {
	if _, _, err := s.services.get(method); err == nil {
		return true
	}
	return false
}

//Jason: helper struct for converting ByteBuffer to ReadCloser
type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

// ServeHTTP
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, 405, "rpc: POST method required, received "+r.Method)
		return
	}
	contentType := r.Header.Get("Content-Type")
	idx := strings.Index(contentType, ";")
	if idx != -1 {
		contentType = contentType[:idx]
	}
	codec := s.codecs[strings.ToLower(contentType)]
	if codec == nil {
		WriteError(w, 415, "rpc: unrecognized Content-Type: "+contentType)
		return
	}
	// Create a new codec request.
	codecReqArray, err := codec.NewRequest(r)

	if err != nil {
		WriteError(w, 400, "Failed to parse the body as valid JSONRPC 2.0 request")
		return
	}

	queryCount := len(codecReqArray)

	// Prevents Internet Explorer from MIME-sniffing a response away
	// from the declared content-type
	w.Header().Set("x-content-type-options", "nosniff")

	codecRepArray := make([]interface{}, queryCount)

	for i, codecReq := range codecReqArray {

		// if queryCount > 1 && i == 0 {
		// 	w.Write([]byte("["))
		// }
		errParse := codecReq.Error()
		if errParse != nil {
			codecRepArray[i] = codecReq.ErrorReply(errParse)
			continue
		}

		// Get service method to be called.
		method, errMethod := codecReq.Method()
		if errMethod != nil {
			//codecReq.WriteError(w, 400, errMethod)
			codecRepArray[i] = codecReq.ErrorReply(errMethod)
			return
		}
		serviceSpec, methodSpec, errGet := s.services.get(method)
		if errGet != nil {
			//codecReq.WriteError(w, 400, errGet)
			codecRepArray[i] = codecReq.ErrorReply(errGet)
			return
		}
		// Decode the args.
		args := reflect.New(methodSpec.argsType)
		if errRead := codecReq.ReadRequest(args.Interface()); errRead != nil {
			//codecReq.WriteError(w, 400, errRead)
			codecRepArray[i] = codecReq.ErrorReply(errRead)
			return
		}

		//Jason: restore body for further auth check
		r.Body = nopCloser{bytes.NewBuffer(codecReq.Body())}

		// Call the service method.
		reply := reflect.New(methodSpec.replyType)
		errValue := methodSpec.method.Func.Call([]reflect.Value{
			serviceSpec.rcvr,
			reflect.ValueOf(r),
			args,
			reply,
		})
		// Cast the result to error if needed.
		var errResult error
		errInter := errValue[0].Interface()
		if errInter != nil {
			errResult = errInter.(error)
		}

		// Encode the response.
		if errResult == nil {
			codecRepArray[i] = codecReq.ResponseReply(reply.Interface())
		} else {
			codecRepArray[i] = codecReq.ErrorReply(errResult)
		}
	}

	codec.WriteBatchedReply(r, w, codecRepArray)
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, msg)
}
