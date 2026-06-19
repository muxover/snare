package proxy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/dynamic"
)

// ProtoDecoder holds parsed proto file descriptors for runtime gRPC decoding.
type ProtoDecoder struct {
	methods map[string]*desc.MethodDescriptor
}

// NewProtoDecoder parses the given .proto files and indexes every RPC method by
// its gRPC path (/package.Service/Method) for later message decoding.
func NewProtoDecoder(protoFiles []string) (*ProtoDecoder, error) {
	if len(protoFiles) == 0 {
		return nil, nil
	}
	parser := protoparse.Parser{ImportPaths: importDirs(protoFiles)}
	fds, err := parser.ParseFiles(baseNames(protoFiles)...)
	if err != nil {
		return nil, fmt.Errorf("parse proto files: %w", err)
	}
	methods := make(map[string]*desc.MethodDescriptor)
	for _, fd := range fds {
		for _, svc := range fd.GetServices() {
			for _, m := range svc.GetMethods() {
				path := "/" + svc.GetFullyQualifiedName() + "/" + m.GetName()
				methods[path] = m
			}
		}
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no gRPC methods found in proto files")
	}
	return &ProtoDecoder{methods: methods}, nil
}

func (d *ProtoDecoder) DecodeRequest(serviceMethod string, data []byte) (json.RawMessage, error) {
	m := d.methods[serviceMethod]
	if m == nil {
		return nil, fmt.Errorf("unknown method: %s", serviceMethod)
	}
	return decodeMessage(m.GetInputType(), data)
}

func (d *ProtoDecoder) DecodeResponse(serviceMethod string, data []byte) (json.RawMessage, error) {
	m := d.methods[serviceMethod]
	if m == nil {
		return nil, fmt.Errorf("unknown method: %s", serviceMethod)
	}
	return decodeMessage(m.GetOutputType(), data)
}

func decodeMessage(msgType *desc.MessageDescriptor, data []byte) (json.RawMessage, error) {
	msg := dynamic.NewMessage(msgType)
	if err := msg.Unmarshal(data); err != nil {
		return nil, err
	}
	out, err := msg.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

func importDirs(files []string) []string {
	seen := make(map[string]struct{})
	var dirs []string
	for _, f := range files {
		dir := "."
		if idx := strings.LastIndexAny(f, "/\\"); idx != -1 {
			dir = f[:idx]
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs
}

func baseNames(files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		if idx := strings.LastIndexAny(f, "/\\"); idx != -1 {
			out[i] = f[idx+1:]
		} else {
			out[i] = f
		}
	}
	return out
}
