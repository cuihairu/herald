package builtin

import (
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/providers/builtin/aliyunsms"
	"github.com/cuihairu/herald/providers/builtin/discord"
	"github.com/cuihairu/herald/providers/builtin/dingtalk"
	"github.com/cuihairu/herald/providers/builtin/email"
	"github.com/cuihairu/herald/providers/builtin/feishu"
	"github.com/cuihairu/herald/providers/builtin/log"
	"github.com/cuihairu/herald/providers/builtin/neteasesms"
	"github.com/cuihairu/herald/providers/builtin/slack"
	"github.com/cuihairu/herald/providers/builtin/telegram"
	"github.com/cuihairu/herald/providers/builtin/tencentsms"
	"github.com/cuihairu/herald/providers/builtin/webhook"
	"github.com/cuihairu/herald/providers/builtin/wecom"
	"github.com/cuihairu/herald/providers/builtin/wechat"
	"github.com/cuihairu/herald/providers/builtin/wechatmp"
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
	manager.RegisterFactory(&aliyunsms.Factory{})
	manager.RegisterFactory(&tencentsms.Factory{})
	manager.RegisterFactory(&neteasesms.Factory{})
	manager.RegisterFactory(&wechat.Factory{})
	manager.RegisterFactory(&wechatmp.Factory{})
}
