package handlers

import (
	"github.com/icaruszezen/AIProviderScheduling/v7/internal/runtime/executor/helps"
	"github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/usage"
)

func parsePluginExecutorResponseUsage(protocol string, payload []byte) usage.Detail {
	return helps.ParsePluginExecutorResponseUsage(protocol, payload)
}

func observePluginExecutorStreamUsage(protocol string, payload []byte, buffer *helps.StreamUsageBuffer) {
	helps.ObservePluginExecutorStreamUsage(protocol, payload, buffer)
}
