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
	"gopkg.in/yaml.v3"
)

var currentConfig atomic.Value

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
	AuthID   string `yaml:"auth_id"`
	Delegate string `yaml:"delegate"`
	Deny     bool   `yaml:"deny"`
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	Scheduler bool `json:"scheduler"`
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
	case pluginabi.MethodSchedulerPick:
		return pickAuth(request)
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
		decoded, errDecode := decodeConfig(req.ConfigYAML)
		if errDecode != nil {
			return errDecode
		}
		cfg = decoded
	}
	cfg.AuthID = strings.TrimSpace(cfg.AuthID)
	cfg.Delegate = strings.TrimSpace(cfg.Delegate)
	currentConfig.Store(cfg)
	return nil
}

func decodeConfig(raw []byte) (pluginConfig, error) {
	var cfg pluginConfig
	if errUnmarshal := yaml.Unmarshal(raw, &cfg); errUnmarshal != nil {
		return pluginConfig{}, errUnmarshal
	}
	return cfg, nil
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "scheduler",
			Version:          "0.1.0",
			Author:           "router-for-me",
			GitHubRepository: "https://github.com/icaruszezen/AIProviderScheduling",
			Logo:             "https://raw.githubusercontent.com/icaruszezen/AIProviderScheduling/main/docs/logo.png",
			ConfigFields: []pluginapi.ConfigField{
				{
					Name:        "auth_id",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "Selects this auth ID when it is present in the scheduler candidates.",
				},
				{
					Name:        "delegate",
					Type:        pluginapi.ConfigFieldTypeEnum,
					EnumValues:  []string{"", pluginapi.SchedulerBuiltinFillFirst, pluginapi.SchedulerBuiltinRoundRobin},
					Description: "Delegates selection to a built-in scheduler when set to fill-first or round-robin.",
				},
				{
					Name:        "deny",
					Type:        pluginapi.ConfigFieldTypeBoolean,
					Description: "Rejects scheduler picks with an explicit error when enabled.",
				},
			},
		},
		Capabilities: registrationCapability{
			Scheduler: true,
		},
	}
}

func pickAuth(raw []byte) ([]byte, error) {
	var req pluginapi.SchedulerPickRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}

	cfg := loadedConfig()
	if cfg.Deny {
		return errorEnvelope("scheduler_denied", "scheduler pick denied by plugin configuration"), nil
	}
	switch cfg.Delegate {
	case pluginapi.SchedulerBuiltinFillFirst, pluginapi.SchedulerBuiltinRoundRobin:
		return okEnvelope(pluginapi.SchedulerPickResponse{
			DelegateBuiltin: cfg.Delegate,
			Handled:         true,
		})
	case "":
	default:
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}
	if cfg.AuthID == "" {
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}
	for _, candidate := range req.Candidates {
		if candidate.ID == cfg.AuthID {
			return okEnvelope(pluginapi.SchedulerPickResponse{
				AuthID:  cfg.AuthID,
				Handled: true,
			})
		}
	}
	return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
}

func loadedConfig() pluginConfig {
	raw := currentConfig.Load()
	if cfg, ok := raw.(pluginConfig); ok {
		return cfg
	}
	return pluginConfig{}
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
