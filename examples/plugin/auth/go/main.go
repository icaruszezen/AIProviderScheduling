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

static const aps_host_api* stored_host;

static void store_host_api(const aps_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, aps_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"encoding/json"
	"net/http"
	"time"
	"unsafe"
)

const abiVersion uint32 = 1

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {}

//export aps_plugin_init
func aps_plugin_init(host *C.aps_host_api, plugin *C.aps_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
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
	raw, errHandle := handleMethod(C.GoString(method))
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	_ = request
	_ = requestLen
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

func handleMethod(method string) ([]byte, error) {
	_ = http.StatusOK
	_ = time.Second
	switch method {
	case "plugin.register":
		return okEnvelopeJSON("{\"schema_version\":1,\"metadata\":{\"Name\":\"example-auth-go\",\"Version\":\"0.1.0\",\"Author\":\"router-for-me\",\"GitHubRepository\":\"https://github.com/icaruszezen/AIProviderScheduling\",\"Logo\":\"https://example.invalid/example-auth-go.png\",\"ConfigFields\":[]},\"capabilities\":{\"auth_provider\":true}}")
	case "plugin.reconfigure":
		return okEnvelopeJSON("{\"schema_version\":1,\"metadata\":{\"Name\":\"example-auth-go\",\"Version\":\"0.1.0\",\"Author\":\"router-for-me\",\"GitHubRepository\":\"https://github.com/icaruszezen/AIProviderScheduling\",\"Logo\":\"https://example.invalid/example-auth-go.png\",\"ConfigFields\":[]},\"capabilities\":{\"auth_provider\":true}}")
	case "auth.identifier":
		return okEnvelopeJSON("{\"identifier\":\"example-auth-go\"}")
	case "auth.parse":
		return okEnvelopeJSON("{\"Handled\":true,\"Auth\":{\"Provider\":\"example-auth-go\",\"ID\":\"example-auth-go\",\"FileName\":\"example-auth-go.json\",\"Label\":\"Auth Example\",\"StorageJSON\":\"eyJ0eXBlIjoiZXhhbXBsZS1hdXRoLWdvIiwidG9rZW4iOiJleGFtcGxlLXRva2VuIn0=\",\"Metadata\":{\"type\":\"example-auth-go\"}}}")
	case "auth.login.start":
		return okEnvelopeJSON("{\"Provider\":\"example-auth-go\",\"URL\":\"https://example.invalid/login\",\"State\":\"example-state\",\"ExpiresAt\":\"2030-01-01T00:00:00Z\"}")
	case "auth.login.poll":
		return okEnvelopeJSON("{\"Status\":\"success\",\"Message\":\"example login complete\",\"Auth\":{\"Provider\":\"example-auth-go\",\"ID\":\"example-auth-go\",\"FileName\":\"example-auth-go.json\",\"Label\":\"Auth Example\",\"StorageJSON\":\"eyJ0eXBlIjoiZXhhbXBsZS1hdXRoLWdvIiwidG9rZW4iOiJleGFtcGxlLXRva2VuIn0=\",\"Metadata\":{\"type\":\"example-auth-go\"}}}")
	case "auth.refresh":
		return okEnvelopeJSON("{\"Auth\":{\"Provider\":\"example-auth-go\",\"ID\":\"example-auth-go\",\"FileName\":\"example-auth-go.json\",\"Label\":\"Auth Example\",\"StorageJSON\":\"eyJ0eXBlIjoiZXhhbXBsZS1hdXRoLWdvIiwidG9rZW4iOiJleGFtcGxlLXRva2VuIn0=\",\"Metadata\":{\"type\":\"example-auth-go\"}},\"NextRefreshAfter\":\"2030-01-01T00:00:00Z\"}")
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func okEnvelopeJSON(result string) ([]byte, error) {
	return json.Marshal(envelope{OK: true, Result: json.RawMessage(result)})
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

func callHost(method string, payload []byte) {
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var response C.aps_buffer
	var req *C.uint8_t
	if len(payload) > 0 {
		req = (*C.uint8_t)(C.CBytes(payload))
		defer C.free(unsafe.Pointer(req))
	}
	if C.call_host_api(cMethod, req, C.size_t(len(payload)), &response) == 0 && response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
}
