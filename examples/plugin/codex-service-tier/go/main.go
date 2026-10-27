package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} aps_buffer;

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	void* call;
	void* free_buffer;
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
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/icaruszezen/AIProviderScheduling/v7/sdk/pluginabi"
	"github.com/icaruszezen/AIProviderScheduling/v7/sdk/pluginapi"
	"github.com/tidwall/sjson"
	"gopkg.in/yaml.v3"
)

var fastEnabled atomic.Bool

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type pluginConfig struct {
	Fast bool `yaml:"fast"`
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	RequestNormalizer bool `json:"request_normalizer"`
}

func main() {}

//export aps_plugin_init
func aps_plugin_init(_ *C.aps_host_api, plugin *C.aps_plugin_api) C.int {
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
}

//export apsPluginShutdown
func apsPluginShutdown() {}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodRequestNormalize:
		return normalizeRequest(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func configure(raw []byte) error {
	var req lifecycleRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return errUnmarshal
		}
	}

	cfg := pluginConfig{}
	if len(req.ConfigYAML) > 0 {
		fast, errDecodeFast := decodeFastConfig(req.ConfigYAML)
		if errDecodeFast != nil {
			return errDecodeFast
		}
		cfg.Fast = fast
	}
	fastEnabled.Store(cfg.Fast)
	return nil
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "codex-service-tier",
			Version:          "0.1.0",
			Author:           "router-for-me",
			GitHubRepository: "https://github.com/icaruszezen/AIProviderScheduling",
			Logo:             "https://raw.githubusercontent.com/icaruszezen/AIProviderScheduling/main/docs/logo.png",
			ConfigFields: []pluginapi.ConfigField{{
				Name:        "fast",
				Type:        pluginapi.ConfigFieldTypeBoolean,
				Description: "Sets Codex gpt-5.5 Responses requests to the priority service tier.",
			}},
		},
		Capabilities: registrationCapability{
			RequestNormalizer: true,
		},
	}
}

func normalizeRequest(raw []byte) ([]byte, error) {
	var req pluginapi.RequestTransformRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	body := req.Body
	if !shouldSetPriorityServiceTier(req) {
		return okEnvelope(pluginapi.PayloadResponse{Body: body})
	}
	updated, okSet := setPriorityServiceTier(body)
	if !okSet {
		return okEnvelope(pluginapi.PayloadResponse{Body: body})
	}
	return okEnvelope(pluginapi.PayloadResponse{Body: updated})
}

func shouldSetPriorityServiceTier(req pluginapi.RequestTransformRequest) bool {
	if !fastEnabled.Load() {
		return false
	}
	if !strings.EqualFold(req.ToFormat, "codex") {
		return false
	}
	return req.Model == "gpt-5.5"
}

func decodeFastConfig(configYAML []byte) (bool, error) {
	var cfg pluginConfig
	if errUnmarshal := yaml.Unmarshal(configYAML, &cfg); errUnmarshal != nil {
		return false, errUnmarshal
	}
	return cfg.Fast, nil
}

func setPriorityServiceTier(body []byte) ([]byte, bool) {
	updated, errSet := sjson.SetBytes(body, "service_tier", "priority")
	if errSet != nil {
		return nil, false
	}
	return updated, true
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
