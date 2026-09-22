package access

import (
	configaccess "github.com/router-for-me/CLIProxyAPI/v7/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// RegisterConfigAccess installs the inline API key provider, including group keys.
func RegisterConfigAccess(cfg *config.Config) {
	if cfg == nil {
		configaccess.Register(nil, nil)
		return
	}
	credentials := cfg.ChannelGroupCredentials()
	groups := make(map[string]configaccess.GroupCredential, len(credentials))
	for key, credential := range credentials {
		groups[key] = configaccess.GroupCredential{
			Panel:      credential.Panel,
			Group:      credential.Group,
			PolicyJSON: credential.PolicyJSON,
		}
	}
	configaccess.Register(cfg.APIKeys, groups)
}
