package wechatmp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cuihairu/herald/core/httpclient"
)

// userInfoURL is the MP 用户基本信息 endpoint — the subscribe flag on its
// answer is the platform's word on whether the openid still follows the
// account.
var userInfoURL = "https://api.weixin.qq.com/cgi-bin/user/info"

// Probe is the §8 公众号对账 probe: it answers whether the account's
// follower list still contains an openid (粉丝列表比对 half of external
// reconciliation). subscribe=false means the user unfollowed without the
// webhook arriving — the reconciler applies the 取关回流 correction.
type Probe struct {
	appID     string
	appSecret string
	cache     *TokenCache
	client    *httpclient.Client
}

// NewProbe builds the MP probe from the provider configuration map (same
// app_id/app_secret shape as the sender; template_id is irrelevant here).
func NewProbe(config map[string]interface{}) (*Probe, error) {
	// parseConfig type-asserts with ok and cannot fail today; the
	// credential check below is the real gate.
	cfg, _ := parseConfig(config)
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("wechatmp: probe needs app_id and app_secret")
	}
	return &Probe{appID: cfg.AppID, appSecret: cfg.AppSecret, cache: &TokenCache{}, client: httpclient.NewClient(nil)}, nil
}

// Channel names the surface vocabulary this probe verifies.
func (p *Probe) Channel() string { return "wechat_mp" }

// userInfoResponse is the subset of the user/info answer the probe
// reads. MP encodes subscribe as 1/0 numbers, not JSON booleans.
type userInfoResponse struct {
	ErrCode   int    `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	Subscribe int    `json:"subscribe"`
}

// Valid reports whether the MP still counts openid among its followers.
func (p *Probe) Valid(ctx context.Context, openid string) (bool, error) {
	if openid == "" {
		return false, fmt.Errorf("wechatmp: probe needs an openid")
	}
	token, err := p.cache.GetToken(p.appID, p.appSecret, p.client)
	if err != nil {
		return false, fmt.Errorf("wechatmp: probe token: %w", err)
	}
	resp, err := p.client.Get(ctx, fmt.Sprintf("%s?access_token=%s&openid=%s", userInfoURL, token, openid))
	if err != nil {
		// Transport failures and every non-2xx status (httpclient's
		// contract) land here alike — a probe that cannot ask is an
		// error, never an opinion.
		return false, fmt.Errorf("wechatmp: user/info: %w", err)
	}
	var parsed userInfoResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return false, fmt.Errorf("wechatmp: user/info response: %w", err)
	}
	// MP reports API-level failures (bad token, invalid openid) in-band
	// with errcode — that is a probe that cannot ask, not a "no".
	if parsed.ErrCode != 0 {
		return false, fmt.Errorf("wechatmp: user/info: errcode %d %s", parsed.ErrCode, parsed.ErrMsg)
	}
	return parsed.Subscribe == 1, nil
}
