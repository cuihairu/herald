package builtin

import (
	"github.com/cuihaitao/herald/core/runtime"
	"github.com/cuihaitao/herald/providers/builtin/discord"
	"github.com/cuihaitao/herald/providers/builtin/dingtalk"
	"github.com/cuihaitao/herald/providers/builtin/email"
	"github.com/cuihaitao/herald/providers/builtin/feishu"
	"github.com/cuihaitao/herald/providers/builtin/log"
	"github.com/cuihaitao/herald/providers/builtin/slack"
	"github.com/cuihaitao/herald/providers/builtin/telegram"
	"github.com/cuihaitao/herald/providers/builtin/webhook"
	"github.com/cuihaitao/herald/providers/builtin/wecom"
)

// RegisterBuiltinProviders registers all builtin provider factories
func RegisterBuiltinProviders(manager *runtime.Manager) {
	manager.RegisterFactory(&log.Factory{})
	manager.RegisterFactory(&telegram.Factory{})
	manager.RegisterFactory(&feishu.Factory{})
	manager.RegisterFactory(&wecom.Factory{})
	manager.RegisterFactory(&email.Factory{})
	manager.RegisterFactory(&webhook.Factory{})
	manager.RegisterFactory(&discord.Factory{})
	manager.RegisterFactory(&slack.Factory{})
	manager.RegisterFactory(&dingtalk.Factory{})
}
