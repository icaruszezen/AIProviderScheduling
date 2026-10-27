package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} aps_buffer;

typedef int (*aps_host_call_fn)(void*, const char*, const uint8_t*, size_t, aps_buffer*);
typedef void (*aps_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	aps_host_call_fn call;
	aps_host_free_fn free_buffer;
} aps_host_api;

typedef int (*aps_plugin_call_fn)(char*, uint8_t*, size_t, aps_buffer*);
typedef void (*aps_plugin_free_fn)(void*, size_t);
typedef void (*aps_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	aps_plugin_call_fn call;
	aps_plugin_free_fn free_buffer;
	aps_plugin_shutdown_fn shutdown;
} aps_plugin_api;

extern int apsPluginCall(char*, uint8_t*, size_t, aps_buffer*);
extern void apsPluginFree(void*, size_t);
extern void apsPluginShutdown(void);
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/icaruszezen/AIProviderScheduling/v7/sdk/pluginabi"
	"github.com/icaruszezen/AIProviderScheduling/v7/sdk/pluginapi"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  capabilities       `json:"capabilities"`
}

type capabilities struct {
	FrontendAuthProvider          bool `json:"frontend_auth_provider"`
	FrontendAuthProviderExclusive bool `json:"frontend_auth_provider_exclusive"`
}

type identifierResponse struct {
	Identifier string `json:"identifier"`
}

func main() {}

//export aps_plugin_init
func aps_plugin_init(host *C.aps_host_api, plugin *C.aps_plugin_api) C.int {
	_ = host
	if plugin == nil {
		return 1
	}
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.aps_plugin_call_fn(C.apsPluginCall)
	plugin.free_buffer = C.aps_plugin_free_fn(C.apsPluginFree)
	plugin.shutdown = C.aps_plugin_shutdown_fn(C.apsPluginShutdown)
	return 0
}

//export apsPluginCall
func apsPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.aps_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export apsPluginFree
func apsPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export apsPluginShutdown
func apsPluginShutdown() {}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return okEnvelope(exampleRegistration())
	case pluginabi.MethodFrontendAuthIdentifier:
		return okEnvelope(identifierResponse{Identifier: "example-frontend-auth-exclusive-go"})
	case pluginabi.MethodFrontendAuthAuthenticate:
		return authenticate(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func exampleRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "example-frontend-auth-exclusive-go",
			Version:          "0.1.0",
			Author:           "router-for-me",
			GitHubRepository: "https://github.com/icaruszezen/AIProviderScheduling",
			Logo:             "https://example.invalid/example-frontend-auth-exclusive-go.png",
			ConfigFields:     []pluginapi.ConfigField{},
		},
		Capabilities: capabilities{
			FrontendAuthProvider:          true,
			FrontendAuthProviderExclusive: true,
		},
	}
}

func authenticate(request []byte) ([]byte, error) {
	var req pluginapi.FrontendAuthRequest
	if errUnmarshal := json.Unmarshal(request, &req); errUnmarshal != nil {
		return okEnvelope(pluginapi.FrontendAuthResponse{Authenticated: false})
	}
	if req.Headers.Get("X-Example-Frontend-Auth") != "exclusive" {
		return okEnvelope(pluginapi.FrontendAuthResponse{Authenticated: false})
	}
	return okEnvelope(pluginapi.FrontendAuthResponse{
		Authenticated: true,
		Principal:     "example-frontend-auth-exclusive-go",
		Metadata: map[string]string{
			"mode":     "exclusive",
			"provider": "example-frontend-auth-exclusive-go",
		},
	})
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.aps_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}
